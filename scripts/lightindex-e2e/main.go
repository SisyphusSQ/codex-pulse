package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const confirmationPhrase = "READ_ONLY_CONFIRMED"

type summary struct {
	Version                 string `json:"version"`
	Result                  string `json:"result"`
	MigrationMS             int64  `json:"migrationMs"`
	MetadataFirstScreenMS   int64  `json:"metadataFirstScreenMs"`
	BackgroundTokenScanMS   int64  `json:"backgroundTokenScanMs"`
	InitialTotalMS          int64  `json:"initialTotalMs"`
	SessionCount            int    `json:"sessionCount"`
	RolloutCount            int    `json:"rolloutCount"`
	CompletedRolloutCount   int    `json:"completedRolloutCount"`
	LogicalSourceBytes      int64  `json:"logicalSourceBytes"`
	PhysicalBytesRead       int64  `json:"physicalBytesRead"`
	CandidateLines          int64  `json:"candidateLines"`
	JSONDecodedLines        int64  `json:"jsonDecodedLines"`
	DatabaseBytes           int64  `json:"databaseBytes"`
	WALBytes                int64  `json:"walBytes"`
	NoChangeMetadataMS      int64  `json:"noChangeMetadataMs"`
	NoChangeBackgroundMS    int64  `json:"noChangeBackgroundMs"`
	UnchangedRolloutCount   int    `json:"unchangedRolloutCount"`
	ChangedRolloutCount     int    `json:"changedRolloutCount"`
	NoChangePhysicalReadAdd int64  `json:"noChangePhysicalReadAdd"`
	ChangedPhysicalReadAdd  int64  `json:"changedPhysicalReadAdd"`
	IsolatedStoreRemoved    bool   `json:"isolatedStoreRemoved"`
}

type scanMetrics struct {
	rollouts, complete                       int
	logical, physical, candidates, jsonLines int64
	bySession                                map[string]scanSnapshot
}

type scanSnapshot struct {
	sizeBytes, mtimeNS, physicalBytes int64
}

func main() {
	home := flag.String("home", "", "explicit confirmed Codex Home")
	confirm := flag.String("confirm", "", "read-only confirmation phrase")
	tempParent := flag.String("temp-parent", "", "optional private temporary parent")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := execute(ctx, *home, *confirm, *tempParent)
	if err != nil {
		fmt.Fprintln(os.Stderr, stableError(err))
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "LIGHT-E2E-009: encode sanitized result")
		os.Exit(1)
	}
}

func execute(ctx context.Context, home, confirm, tempParent string) (summary, error) {
	if home == "" || confirm != confirmationPhrase {
		return summary{}, errors.New("LIGHT-E2E-001: explicit Home and read-only confirmation are required")
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, home)
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-002: confirm Home identity")
	}
	root, err := os.MkdirTemp(tempParent, "codex-pulse-light-e2e-")
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-003: create isolated store")
	}
	removed := false
	defer func() {
		if !removed {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return summary{}, errors.New("LIGHT-E2E-003: secure isolated store")
	}
	databasePath := filepath.Join(root, "light-index.db")
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: databasePath})
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-004: open isolated store")
	}
	closed := false
	defer func() {
		if !closed {
			_ = database.Close(context.Background())
		}
	}()
	repository := store.NewRepository(database)
	migrationStarted := time.Now()
	if _, err := repository.MigrateApplicationSchema(ctx); err != nil {
		return summary{}, errors.New("LIGHT-E2E-004: migrate isolated store")
	}
	result := summary{Version: "light-index-e2e-v1", Result: "passed", MigrationMS: time.Since(migrationStarted).Milliseconds()}
	runtime, err := lightindex.NewRuntime(lightindex.RuntimeConfig{
		Repository: repository, Metadata: lightindex.LocalMetadataProvider{},
	})
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-005: create runtime")
	}
	homeIdentity := store.LightHomeIdentity{Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode}
	initialStarted := time.Now()
	run, err := runtime.Start(ctx, homeIdentity)
	result.MetadataFirstScreenMS = time.Since(initialStarted).Milliseconds()
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-006: publish App Server metadata")
	}
	sessions, err := repository.ListLightSessions(ctx)
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-006: read metadata projection")
	}
	result.SessionCount = len(sessions)
	backgroundStarted := time.Now()
	if err := run.Wait(ctx); err != nil {
		return summary{}, errors.New("LIGHT-E2E-007: scan Token events")
	}
	result.BackgroundTokenScanMS = time.Since(backgroundStarted).Milliseconds()
	result.InitialTotalMS = time.Since(initialStarted).Milliseconds()
	first, err := collectMetrics(ctx, repository, sessions)
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-007: collect scan metrics")
	}
	result.RolloutCount, result.CompletedRolloutCount = first.rollouts, first.complete
	result.LogicalSourceBytes, result.PhysicalBytesRead = first.logical, first.physical
	result.CandidateLines, result.JSONDecodedLines = first.candidates, first.jsonLines
	result.DatabaseBytes = fileSize(databasePath)
	result.WALBytes = fileSize(databasePath + "-wal")

	refreshStarted := time.Now()
	refresh, err := runtime.Start(ctx, homeIdentity)
	result.NoChangeMetadataMS = time.Since(refreshStarted).Milliseconds()
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-008: publish unchanged metadata")
	}
	refreshBackgroundStarted := time.Now()
	if err := refresh.Wait(ctx); err != nil {
		return summary{}, errors.New("LIGHT-E2E-008: reuse unchanged scans")
	}
	result.NoChangeBackgroundMS = time.Since(refreshBackgroundStarted).Milliseconds()
	secondSessions, err := repository.ListLightSessions(ctx)
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-008: read unchanged metadata")
	}
	second, err := collectMetrics(ctx, repository, secondSessions)
	if err != nil {
		return summary{}, errors.New("LIGHT-E2E-008: collect unchanged metrics")
	}
	for sessionID, after := range second.bySession {
		before, found := first.bySession[sessionID]
		if !found {
			result.ChangedRolloutCount++
			result.ChangedPhysicalReadAdd += after.physicalBytes
			continue
		}
		delta := after.physicalBytes - before.physicalBytes
		if before.sizeBytes == after.sizeBytes && before.mtimeNS == after.mtimeNS {
			result.UnchangedRolloutCount++
			result.NoChangePhysicalReadAdd += delta
		} else {
			result.ChangedRolloutCount++
			result.ChangedPhysicalReadAdd += delta
		}
	}
	if err := database.Close(context.Background()); err != nil {
		return summary{}, errors.New("LIGHT-E2E-004: close isolated store")
	}
	closed = true
	if err := os.RemoveAll(root); err != nil {
		return summary{}, errors.New("LIGHT-E2E-003: remove isolated store")
	}
	removed = true
	result.IsolatedStoreRemoved = true
	return result, nil
}

func collectMetrics(ctx context.Context, repository *store.Repository, sessions []store.LightSessionMetadata) (scanMetrics, error) {
	result := scanMetrics{bySession: make(map[string]scanSnapshot)}
	for _, session := range sessions {
		if session.RolloutPath == nil {
			continue
		}
		result.rollouts++
		scan, err := repository.ActiveLightTokenScan(ctx, session.SessionID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return scanMetrics{}, err
		}
		if scan.Checkpoint.Complete {
			result.complete++
		}
		result.logical += scan.Identity.SizeBytes
		result.physical += scan.Checkpoint.PhysicalBytesRead
		result.candidates += scan.Checkpoint.CandidateLines
		result.jsonLines += scan.Checkpoint.JSONDecoded
		result.bySession[session.SessionID] = scanSnapshot{
			sizeBytes: scan.Identity.SizeBytes, mtimeNS: scan.Identity.MTimeNS,
			physicalBytes: scan.Checkpoint.PhysicalBytesRead,
		}
	}
	return result, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func stableError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
