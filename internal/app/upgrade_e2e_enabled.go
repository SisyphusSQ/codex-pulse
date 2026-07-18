//go:build darwin && cgo && upgradee2e

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/singleinstance"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

const upgradeE2EControlFilename = "upgrade-e2e-control.json"

type upgradeE2EControl struct {
	Version        string `json:"version"`
	Root           string `json:"root"`
	DataDirectory  string `json:"dataDirectory"`
	RuntimeDir     string `json:"runtimeDirectory"`
	Preferences    string `json:"preferences"`
	Evidence       string `json:"evidence"`
	Result         string `json:"result"`
	Scenario       string `json:"scenario"`
	Action         string `json:"action"`
	SourceVersion  string `json:"sourceVersion"`
	TargetVersion  string `json:"targetVersion"`
	MarkerSession  string `json:"markerSession"`
	ExpectedSchema int    `json:"expectedSchema"`
}

type upgradeE2EEvidence struct {
	Version       string `json:"version"`
	AtMS          int64  `json:"atMs"`
	Scenario      string `json:"scenario"`
	Application   string `json:"applicationVersion"`
	Stage         string `json:"stage"`
	Phase         string `json:"phase,omitempty"`
	FaultCode     string `json:"faultCode,omitempty"`
	SchemaVersion int    `json:"schemaVersion,omitempty"`
	MarkerPresent bool   `json:"markerPresent,omitempty"`
	BackupPresent bool   `json:"backupPresent,omitempty"`
	PID           int    `json:"pid"`
}

func loadUpgradeE2EOverrides() (upgradeE2EOverrides, bool, error) {
	control, found, err := loadUpgradeE2EControl()
	if err != nil || !found {
		return upgradeE2EOverrides{}, found, err
	}
	return upgradeE2EOverrides{
		Store: storesqlite.Config{Path: filepath.Join(control.DataDirectory, "codex-pulse.db")},
		Instance: singleinstance.Config{
			Directory: control.RuntimeDir, Name: "com.sisyphussq.codexpulse-upgrade-e2e",
		},
		PreferencesPath: control.Preferences,
	}, true, nil
}

func prepareUpgradeE2EPreferences(ctx context.Context, store *preferences.FileStore) error {
	control, found, err := loadUpgradeE2EControl()
	if err != nil || !found {
		return err
	}
	if store == nil {
		return errors.New("upgrade E2E preference store is unavailable")
	}
	home := filepath.Join(control.Root, "home", "codex")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(home)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		return errors.New("upgrade E2E Codex Home inode is unavailable")
	}
	snapshot := preferences.OnboardingSnapshot{
		SchemaVersion: preferences.CurrentSchemaVersion, OnboardingVersion: preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: home, DeviceID: "upgrade-e2e-device", Inode: int64(stat.Ino),
			ConfirmedAtMS: 1,
		},
	}
	if err := store.Confirm(ctx, snapshot); err != nil && !errors.Is(err, preferences.ErrAlreadyConfirmed) {
		return err
	}
	current, err := store.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	if !current.Updates.AutoCheckEnabled {
		return nil
	}
	next := current
	next.Revision++
	next.Updates.AutoCheckEnabled = false
	return store.CompareAndSwap(ctx, current.Revision, next)
}

func loadUpgradeE2EControl() (upgradeE2EControl, bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return upgradeE2EControl{}, false, err
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "..", "..", upgradeE2EControlFilename))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return upgradeE2EControl{}, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return upgradeE2EControl{}, true, fmt.Errorf("invalid upgrade E2E control file")
	}
	file, err := os.Open(path)
	if err != nil {
		return upgradeE2EControl{}, true, err
	}
	defer file.Close()
	var control upgradeE2EControl
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(file, 16<<10)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&control); err != nil {
		return upgradeE2EControl{}, true, fmt.Errorf("decode upgrade E2E control: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return upgradeE2EControl{}, true, errors.New("decode upgrade E2E control: trailing content")
	}
	if err := validateUpgradeE2EControl(control, filepath.Dir(filepath.Dir(path))); err != nil {
		return upgradeE2EControl{}, true, err
	}
	return control, true, nil
}

func validateUpgradeE2EControl(control upgradeE2EControl, expectedRoot string) error {
	if control.Version != "upgrade-e2e-v1" || control.SourceVersion == "" || control.TargetVersion == "" ||
		control.SourceVersion == control.TargetVersion || control.MarkerSession == "" || len(control.MarkerSession) > 128 ||
		control.ExpectedSchema < 1 || (control.Action != "update" && control.Action != "verify_rollback") {
		return errors.New("invalid upgrade E2E control contract")
	}
	switch control.Scenario {
	case "success", "bad_signature", "offline", "information_only", "migration_failure":
	default:
		return errors.New("invalid upgrade E2E scenario")
	}
	if !filepath.IsAbs(control.Root) || filepath.Clean(control.Root) != control.Root || !sameResolvedPath(control.Root, expectedRoot) {
		return errors.New("invalid upgrade E2E root")
	}
	for _, path := range []string{
		control.DataDirectory, control.RuntimeDir, control.Preferences, control.Evidence, control.Result,
	} {
		if !pathWithinRoot(control.Root, path) {
			return errors.New("upgrade E2E path escapes isolated root")
		}
	}
	rootInfo, err := os.Lstat(control.Root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || rootInfo.Mode().Perm() != 0o700 {
		return errors.New("upgrade E2E root is not a private directory")
	}
	for _, directory := range []string{control.DataDirectory, control.RuntimeDir} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			return errors.New("upgrade E2E directory is not private")
		}
	}
	for _, path := range []string{control.Preferences, control.Evidence, control.Result} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("upgrade E2E output is not a private regular file")
		}
	}
	return nil
}

func sameResolvedPath(left, right string) bool {
	resolvedLeft, leftErr := filepath.EvalSymlinks(left)
	resolvedRight, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && resolvedLeft == resolvedRight
}

func pathWithinRoot(root, path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func startUpgradeE2EAutomation(
	ctx context.Context,
	database *storesqlite.Store,
	runtime *applicationUpdaterRuntime,
	quit func(),
) error {
	control, found, err := loadUpgradeE2EControl()
	if err != nil || !found {
		return err
	}
	if database == nil || runtime == nil || quit == nil {
		return errors.New("upgrade E2E runtime is incomplete")
	}
	go runUpgradeE2EAutomation(context.WithoutCancel(ctx), control, database, runtime, quit)
	return nil
}

func runUpgradeE2EAutomation(
	ctx context.Context,
	control upgradeE2EControl,
	database *storesqlite.Store,
	runtime *applicationUpdaterRuntime,
	quit func(),
) {
	fail := func(stage string, err error) {
		fmt.Fprintf(os.Stderr, "upgrade e2e %s: %v\n", stage, err)
		_ = appendUpgradeE2EEvidence(control, upgradeE2EEvidence{Stage: stage, Phase: "failed"})
		_ = writeUpgradeE2EResult(control, upgradeE2EEvidence{Stage: stage, Phase: "failed", FaultCode: stableUpgradeE2EFault(err)})
		scheduleUpgradeE2EQuit(quit)
	}

	repository := factstore.NewRepository(database)
	schemaVersion, err := repository.UpgradeE2ESchemaVersion(ctx)
	if err != nil {
		fail("schema_readback", err)
		return
	}
	if applicationVersion == control.TargetVersion {
		marker, markerErr := repository.Session(ctx, control.MarkerSession)
		backup := hasUpgradeE2EBackup(control.DataDirectory)
		evidence := upgradeE2EEvidence{
			Stage: "target_verified", Phase: "succeeded", SchemaVersion: schemaVersion,
			MarkerPresent: markerErr == nil && marker.SessionID == control.MarkerSession, BackupPresent: backup,
		}
		if schemaVersion != control.ExpectedSchema || !evidence.MarkerPresent || !backup {
			fail("target_verify", errors.New("target migration evidence mismatch"))
			return
		}
		_ = appendUpgradeE2EEvidence(control, evidence)
		_ = writeUpgradeE2EResult(control, evidence)
		scheduleUpgradeE2EQuit(quit)
		return
	}
	if applicationVersion != control.SourceVersion || schemaVersion != control.ExpectedSchema-1 {
		fail("source_version", errors.New("source version or schema mismatch"))
		return
	}
	marker := factstore.Session{
		SessionID: control.MarkerSession, Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 1,
	}
	if err := repository.UpsertFacts(ctx, factstore.FactBatch{Session: &marker}); err != nil {
		fail("source_seed", err)
		return
	}
	if control.Action == "verify_rollback" {
		stored, err := repository.Session(ctx, control.MarkerSession)
		evidence := upgradeE2EEvidence{
			Stage: "rollback_verified", Phase: "succeeded", SchemaVersion: schemaVersion,
			MarkerPresent: err == nil && stored.SessionID == control.MarkerSession,
		}
		if !evidence.MarkerPresent {
			fail("rollback_verify", err)
			return
		}
		_ = appendUpgradeE2EEvidence(control, evidence)
		_ = writeUpgradeE2EResult(control, evidence)
		scheduleUpgradeE2EQuit(quit)
		return
	}
	_ = appendUpgradeE2EEvidence(control, upgradeE2EEvidence{
		Stage: "source_ready", Phase: "succeeded", SchemaVersion: schemaVersion, MarkerPresent: true,
	})
	time.Sleep(250 * time.Millisecond)
	if err := runtime.StartupError(); err != nil {
		fail("updater_start", err)
		return
	}
	receipt, err := runtime.Trigger(ctx, updater.TriggerManual)
	if err != nil || !receipt.Accepted {
		fail("check", errors.Join(err, fmt.Errorf("trigger reason: %s", receipt.Reason)))
		return
	}
	_ = appendUpgradeE2EEvidence(control, upgradeE2EEvidence{Stage: "check_accepted", Phase: "checking"})

	deadline := time.Now().Add(90 * time.Second)
	downloadRequested := false
	for time.Now().Before(deadline) {
		snapshot := runtime.Snapshot()
		if snapshot.Phase == updater.PhaseError {
			code := ""
			if snapshot.Fault != nil {
				code = string(snapshot.Fault.Code)
			}
			expected := control.Scenario == "offline" && code == string(updater.FaultCheck) ||
				control.Scenario == "bad_signature" && code == string(updater.FaultInvalidSignature)
			evidence := upgradeE2EEvidence{Stage: "expected_failure", Phase: "error", FaultCode: code, SchemaVersion: schemaVersion, MarkerPresent: true}
			_ = appendUpgradeE2EEvidence(control, evidence)
			if expected {
				_ = writeUpgradeE2EResult(control, evidence)
				scheduleUpgradeE2EQuit(quit)
				return
			}
			fail("unexpected_update_failure", errors.New(code))
			return
		}
		if snapshot.Phase == updater.PhaseAvailable && !snapshot.ReadyToInstall && !downloadRequested {
			if snapshot.Update != nil && snapshot.Update.InformationOnly {
				if err := runtime.Download(ctx); !errors.Is(err, updater.ErrCannotDownload) {
					fail("information_only_download", err)
					return
				}
				evidence := upgradeE2EEvidence{Stage: "information_only_verified", Phase: "available", SchemaVersion: schemaVersion, MarkerPresent: true}
				_ = appendUpgradeE2EEvidence(control, evidence)
				_ = writeUpgradeE2EResult(control, evidence)
				scheduleUpgradeE2EQuit(quit)
				return
			}
			if err := runtime.Download(ctx); err != nil {
				fail("download", err)
				return
			}
			downloadRequested = true
			_ = appendUpgradeE2EEvidence(control, upgradeE2EEvidence{Stage: "download_confirmed", Phase: "downloading"})
		}
		if snapshot.Phase == updater.PhaseAvailable && snapshot.ReadyToInstall {
			_ = appendUpgradeE2EEvidence(control, upgradeE2EEvidence{Stage: "install_confirmed", Phase: "draining"})
			if err := runtime.Install(ctx); err != nil {
				fail("install", err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fail("timeout", errors.New("upgrade E2E timed out"))
}

func finishUpgradeE2ERecovery(recovery *migrationRecoveryController, quit func()) error {
	control, found, err := loadUpgradeE2EControl()
	if err != nil || !found {
		return err
	}
	if applicationVersion != control.TargetVersion || control.Scenario != "migration_failure" || recovery == nil || quit == nil {
		return errors.New("unexpected upgrade E2E recovery graph")
	}
	snapshot := recovery.Snapshot()
	evidence := upgradeE2EEvidence{
		Stage: "migration_safe_mode", Phase: string(snapshot.Phase), FaultCode: snapshot.Code,
		SchemaVersion: snapshot.CurrentVersion, BackupPresent: hasUpgradeE2EBackup(control.DataDirectory),
	}
	if snapshot.Phase != MigrationRecoveryFailed || snapshot.Stage != factstore.MigrationStageApply || snapshot.Code != factstore.MigrationCodeApplyFailed {
		return errors.New("upgrade E2E recovery did not preserve typed migration failure")
	}
	if err := appendUpgradeE2EEvidence(control, evidence); err != nil {
		return err
	}
	if err := writeUpgradeE2EResult(control, evidence); err != nil {
		return err
	}
	scheduleUpgradeE2EQuit(quit)
	return nil
}

func scheduleUpgradeE2EQuit(quit func()) {
	go func() {
		time.Sleep(250 * time.Millisecond)
		quit()
	}()
}

func hasUpgradeE2EBackup(dataDirectory string) bool {
	matches, err := filepath.Glob(filepath.Join(dataDirectory, "backups", "codex-pulse-before-v*-*.db"))
	return err == nil && len(matches) > 0
}

func appendUpgradeE2EEvidence(control upgradeE2EControl, evidence upgradeE2EEvidence) error {
	evidence.Version = "upgrade-e2e-evidence-v1"
	evidence.AtMS = time.Now().UnixMilli()
	evidence.Scenario = control.Scenario
	evidence.Application = applicationVersion
	evidence.PID = os.Getpid()
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(control.Evidence, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func writeUpgradeE2EResult(control upgradeE2EControl, evidence upgradeE2EEvidence) error {
	evidence.Version = "upgrade-e2e-result-v1"
	evidence.AtMS = time.Now().UnixMilli()
	evidence.Scenario = control.Scenario
	evidence.Application = applicationVersion
	evidence.PID = os.Getpid()
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	temporary := control.Result + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, control.Result)
}

func stableUpgradeE2EFault(err error) string {
	if err == nil {
		return "e2e_failure"
	}
	return "e2e_failure"
}
