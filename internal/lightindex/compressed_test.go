package lightindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestRuntimeCompressedRebuildRestartFailureAndRecovery(t *testing.T) {
	homePath := t.TempDir()
	path := filepath.Join(homePath, "sessions", "one.jsonl.zst")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, encoder.EncodeAll([]byte(content), nil), 0600); err != nil {
			t.Fatal(err)
		}
	}
	content := strings.Repeat(" ", 6000) + tokenLine("2026-07-19T01:00:00Z", 100, 20, 10, 2) + "\n"
	write(content)
	h, err := logsource.NewHomeProbe().Probe(t.Context(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	home := storelight.LightHomeIdentity{Path: h.Path, DeviceID: h.DeviceID, Inode: h.Inode}
	_, deep, repository := openLightRuntimeRepository(t)
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{SessionID: "one", RolloutPath: &path, CreatedAtMS: 100, UpdatedAtMS: 200}}}, nil
	})
	makeRuntime := func() *Runtime {
		t.Helper()
		r, err := NewRuntime(RuntimeConfig{Repository: repository, DeepRepository: deep, Metadata: provider, ScanBatchBytes: 2048, Clock: func() time.Time { return time.UnixMilli(1000) }})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := makeRuntime()
	refresh := func() {
		t.Helper()
		run, err := r.Start(t.Context(), home)
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	a, err := repository.ActiveLightTokenScan(t.Context(), "one")
	if err != nil || a.Checkpoint.InputTokens != 100 || a.Checkpoint.DurableOffset != int64(len(content)) || !a.Checkpoint.Complete {
		t.Fatal("logical compressed scan failed", a, err)
	}
	refresh()
	b, err := repository.ActiveLightTokenScan(t.Context(), "one")
	if err != nil || b.Generation != a.Generation || b.Checkpoint.PhysicalBytesRead != a.Checkpoint.PhysicalBytesRead {
		t.Fatal("unchanged archive rescanned", err)
	}
	r = makeRuntime()
	refresh()
	b, err = repository.ActiveLightTokenScan(t.Context(), "one")
	if err != nil || b.Checkpoint.InputTokens != 100 {
		t.Fatal("restart duplicated usage", err)
	}
	beforeFailure := b.Generation
	if err := os.WriteFile(path, []byte("not a zstd frame"), 0600); err != nil {
		t.Fatal(err)
	}
	refresh()
	b, err = repository.ActiveLightTokenScan(t.Context(), "one")
	sessions, _ := repository.ListLightSessions(t.Context())
	state, _ := repository.LightIndexState(t.Context())
	if err != nil || b.Generation != beforeFailure || sessions[0].ScanState != "failed" || state.TokenScanState != "partial" {
		t.Fatal("bad archive replaced valid facts or stayed pending", err, sessions, state)
	}
	write(tokenLine("2026-07-19T01:00:00Z", 150, 30, 12, 3) + "\n")
	refresh()
	b, err = repository.ActiveLightTokenScan(t.Context(), "one")
	state, _ = repository.LightIndexState(t.Context())
	if err != nil || b.Checkpoint.InputTokens != 150 || state.TokenScanState != "complete" {
		t.Fatal("recovery did not replace facts", err, b, state)
	}
}

func TestRuntimeMissingPathIsDeferredAndEmptyUsageCompletes(t *testing.T) {
	homePath := t.TempDir()
	h, err := logsource.NewHomeProbe().Probe(t.Context(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	home := storelight.LightHomeIdentity{Path: h.Path, DeviceID: h.DeviceID, Inode: h.Inode}
	_, deep, repository := openLightRuntimeRepository(t)
	var rollout *string
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{SessionID: "empty", RolloutPath: rollout, CreatedAtMS: 100, UpdatedAtMS: 200}}}, nil
	})
	r, err := NewRuntime(RuntimeConfig{Repository: repository, DeepRepository: deep, Metadata: provider})
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.Start(t.Context(), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, _ := repository.ListLightSessions(t.Context())
	if sessions[0].ScanState != "deferred" {
		t.Fatal("missing source still pending", sessions)
	}
	p := filepath.Join(h.Path, "sessions", "empty.jsonl.zst")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	encoder, _ := zstd.NewWriter(nil)
	data := encoder.EncodeAll([]byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"turn_aborted\"}}\n"), nil)
	encoder.Close()
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	rollout = &p
	run, err = r.Start(t.Context(), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	sessions, _ = repository.ListLightSessions(t.Context())
	usage, err := repository.LightSessionTokenUsage(t.Context(), "empty")
	if err != nil || sessions[0].ScanState != "complete" || !usage.Complete || usage.InputTokens != 0 {
		t.Fatal("empty compressed source did not finish", err, sessions, usage)
	}
}

func TestRuntimeRestoresCompleteWhenUnchangedPlainSourceReturns(t *testing.T) {
	homePath := t.TempDir()
	h, err := logsource.NewHomeProbe().Probe(t.Context(), homePath)
	if err != nil {
		t.Fatal(err)
	}
	home := storelight.LightHomeIdentity{Path: h.Path, DeviceID: h.DeviceID, Inode: h.Inode}
	p := filepath.Join(h.Path, "sessions", "restored.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(tokenLine("2026-07-19T01:00:00Z", 100, 20, 10, 2)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, deep, repo := openLightRuntimeRepository(t)
	selected := &p
	provider := metadataProviderFunc(func(context.Context, string) (appserver.ThreadList, error) {
		return appserver.ThreadList{Threads: []appserver.ThreadMetadata{{SessionID: "returning", RolloutPath: selected, CreatedAtMS: 100, UpdatedAtMS: 200}}}, nil
	})
	r, err := NewRuntime(RuntimeConfig{Repository: repo, DeepRepository: deep, Metadata: provider})
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		run, err := r.Start(t.Context(), home)
		if err != nil {
			t.Fatal(err)
		}
		if err := run.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	a, err := repo.ActiveLightTokenScan(t.Context(), "returning")
	if err != nil {
		t.Fatal(err)
	}
	selected = nil
	refresh()
	sessions, _ := repo.ListLightSessions(t.Context())
	if sessions[0].ScanState != "deferred" {
		t.Fatal(sessions)
	}
	selected = &p
	refresh()
	sessions, _ = repo.ListLightSessions(t.Context())
	b, err := repo.ActiveLightTokenScan(t.Context(), "returning")
	if err != nil || sessions[0].ScanState != "complete" || b.Generation != a.Generation || b.Checkpoint.PhysicalBytesRead != a.Checkpoint.PhysicalBytesRead {
		t.Fatal("unchanged source recovery lost facts/state", err, sessions)
	}
}
