package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestEnsureDefaultCodexHomeConfiguredConfirmsSafeCandidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	home := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	result, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            home,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logsource.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if !result.Configured || result.IdentityMigrated {
		t.Fatal("ensureDefaultCodexHomeConfigured() did not configure the safe default Home")
	}
	snapshot, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	metadata, err := logsource.NewHomeProbe().Probe(ctx, home)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !snapshot.Onboarding.Completed ||
		snapshot.CodexHome.Source.Path != metadata.Path ||
		snapshot.CodexHome.Source.DeviceID != metadata.DeviceID ||
		snapshot.CodexHome.Source.Inode != metadata.Inode {
		t.Fatalf("persisted Home = %#v, want physical identity %#v", snapshot.CodexHome, metadata)
	}
	if !snapshot.Online.QuotaEnabled || !snapshot.Online.ResetCreditsEnabled {
		t.Fatalf("online defaults = %#v, want both enabled", snapshot.Online)
	}
}

func TestEnsureDefaultCodexHomeConfiguredPreservesExistingChoice(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	existingHome := t.TempDir()
	defaultHome := t.TempDir()
	existingMetadata, err := logsource.NewHomeProbe().Probe(ctx, existingHome)
	if err != nil {
		t.Fatalf("Probe(existing) error = %v", err)
	}
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: existingMetadata.Path, DeviceID: existingMetadata.DeviceID,
			Inode: existingMetadata.Inode, ConfirmedAtMS: 1,
		},
	}); err != nil {
		t.Fatalf("Confirm(existing) error = %v", err)
	}
	result, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            defaultHome,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logsource.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if result != (defaultCodexHomeResult{}) {
		t.Fatal("ensureDefaultCodexHomeConfigured() replaced an existing choice")
	}
	snapshot, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	if snapshot.CodexHome.Source.Path != existingMetadata.Path {
		t.Fatalf("persisted path = %q, want existing %q", snapshot.CodexHome.Source.Path, existingMetadata.Path)
	}
}

func TestEnsureDefaultCodexHomeConfiguredMigratesLegacyDefaultIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	home := t.TempDir()
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	const (
		legacyDeviceID = "16777231"
		stableDeviceID = "volume:00112233445566778899aabbccddeeff"
		homeInode      = int64(42)
	)
	if err := store.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: home, DeviceID: legacyDeviceID, Inode: homeInode, ConfirmedAtMS: 123,
		},
		OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
	}); err != nil {
		t.Fatalf("Confirm(legacy) error = %v", err)
	}
	before, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(before) error = %v", err)
	}
	result, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            home,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe: fixedDefaultHomeProbe{metadata: logsource.HomeMetadata{
			Path: home, DeviceID: stableDeviceID, Inode: homeInode,
		}},
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if result.Configured || !result.IdentityMigrated {
		t.Fatalf("result = %#v, want identity-only migration", result)
	}
	after, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(after) error = %v", err)
	}
	if after.Revision != before.Revision+1 || after.CodexHome.Generation != before.CodexHome.Generation ||
		after.CodexHome.Source.DeviceID != stableDeviceID ||
		after.CodexHome.Source.Path != before.CodexHome.Source.Path ||
		after.CodexHome.Source.Inode != before.CodexHome.Source.Inode ||
		after.CodexHome.Source.ConfirmedAtMS != before.CodexHome.Source.ConfirmedAtMS ||
		after.CodexHome.DataStoreKey != before.CodexHome.DataStoreKey ||
		after.Online != before.Online || after.Refresh != before.Refresh ||
		after.Updates != before.Updates || after.UI != before.UI {
		t.Fatalf("migrated preferences = %#v, want identity-only update from %#v", after, before)
	}
}

func TestEnsureDefaultCodexHomeConfiguredRejectsUnprovenLegacyIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	home := t.TempDir()
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: home, DeviceID: "16777231", Inode: 42, ConfirmedAtMS: 123,
		},
	}); err != nil {
		t.Fatalf("Confirm(legacy) error = %v", err)
	}
	before, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(before) error = %v", err)
	}
	result, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            home,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe: fixedDefaultHomeProbe{metadata: logsource.HomeMetadata{
			Path: home, DeviceID: "volume:00112233445566778899aabbccddeeff", Inode: 43,
		}},
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if result != (defaultCodexHomeResult{}) {
		t.Fatalf("result = %#v, want no migration", result)
	}
	after, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(after) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("preferences changed without identity proof: before=%#v after=%#v", before, after)
	}
}

func TestEnsureDefaultCodexHomeConfiguredLeavesMissingCandidateUnconfigured(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	result, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            filepath.Join(t.TempDir(), "missing"),
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logsource.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if result != (defaultCodexHomeResult{}) {
		t.Fatal("ensureDefaultCodexHomeConfigured() configured a missing candidate")
	}
	if _, err := store.LoadPreferences(ctx); !errors.Is(err, preferences.ErrNotConfigured) {
		t.Fatalf("LoadPreferences() error = %v, want ErrNotConfigured", err)
	}
}

type fixedDefaultHomeProbe struct {
	metadata logsource.HomeMetadata
	err      error
}

func (probe fixedDefaultHomeProbe) Probe(context.Context, string) (logsource.HomeMetadata, error) {
	return probe.metadata, probe.err
}
