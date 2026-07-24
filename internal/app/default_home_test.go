package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
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
	didConfigure, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            home,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logs.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if !didConfigure {
		t.Fatal("ensureDefaultCodexHomeConfigured() did not configure the safe default Home")
	}
	snapshot, err := store.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, home)
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
	existingMetadata, err := logs.NewHomeProbe().Probe(ctx, existingHome)
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
	didConfigure, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            defaultHome,
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logs.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if didConfigure {
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

func TestEnsureDefaultCodexHomeConfiguredLeavesMissingCandidateUnconfigured(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	didConfigure, err := ensureDefaultCodexHomeConfigured(ctx, defaultCodexHomeConfiguration{
		HomePath:            filepath.Join(t.TempDir(), "missing"),
		TrackerDatabasePath: filepath.Join(t.TempDir(), "data", "codex-pulse.db"),
		Store:               store,
		Probe:               logs.NewHomeProbe(),
	})
	if err != nil {
		t.Fatalf("ensureDefaultCodexHomeConfigured() error = %v", err)
	}
	if didConfigure {
		t.Fatal("ensureDefaultCodexHomeConfigured() configured a missing candidate")
	}
	if _, err := store.LoadPreferences(ctx); !errors.Is(err, preferences.ErrNotConfigured) {
		t.Fatalf("LoadPreferences() error = %v, want ErrNotConfigured", err)
	}
}
