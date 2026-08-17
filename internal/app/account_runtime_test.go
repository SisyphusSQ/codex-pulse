package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestAccountSnapshotDoesNotStartReaderAfterConfirmedHomeSwitch(t *testing.T) {
	t.Parallel()

	homeA := t.TempDir()
	homeB := t.TempDir()
	snapshotA := accountRuntimePreferences(t, homeA, 11)
	snapshotB := accountRuntimePreferences(t, homeB, 12)
	loader := &accountSequencePreferencesLoader{snapshots: []preferences.Snapshot{
		snapshotA,
		snapshotB,
	}}
	readerCalls := 0
	runtime := &applicationLifecycleRuntime{
		settingsLoader: loader,
		accountReader: func(
			context.Context,
			appserver.ConfirmedHome,
			appserver.ProcessOptions,
		) (*appserver.AccountSnapshot, error) {
			readerCalls++
			return &appserver.AccountSnapshot{Type: "chatgpt"}, nil
		},
	}

	account, err := runtime.AccountSnapshot(context.Background(), agentprovider.Scope{Provider: agentprovider.Codex})
	if !errors.Is(err, ErrApplicationLifecycleRuntime) || account.Account != nil {
		t.Fatalf("AccountSnapshot(switched before launch) = %#v, %v", account, err)
	}
	if readerCalls != 0 {
		t.Fatalf("account reader calls = %d, want 0", readerCalls)
	}
}

func TestAccountSnapshotReadsCursorIdentityFromDesktopState(t *testing.T) {
	runtime := &applicationLifecycleRuntime{
		cursorAccountReader: func(context.Context) (cursorprovider.DesktopAccountSnapshot, error) {
			return cursorprovider.DesktopAccountSnapshot{
				Email: "person@example.com", MembershipType: "pro", SubscriptionStatus: "active",
			}, nil
		},
	}
	account, err := runtime.AccountSnapshot(
		context.Background(),
		agentprovider.Scope{Provider: agentprovider.Cursor},
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(cursor) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Cursor ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType == nil || *account.Account.PlanType != "pro" {
		t.Fatalf("AccountSnapshot(cursor) = %#v", account)
	}
}

func TestAccountSnapshotStartGuardRechecksConfirmedHome(t *testing.T) {
	t.Parallel()

	homeA := t.TempDir()
	homeB := t.TempDir()
	snapshotA := accountRuntimePreferences(t, homeA, 13)
	snapshotB := accountRuntimePreferences(t, homeB, 14)
	loader := &accountSequencePreferencesLoader{snapshots: []preferences.Snapshot{
		snapshotA,
		snapshotA,
		snapshotB,
	}}
	processStarts := 0
	runtime := &applicationLifecycleRuntime{
		settingsLoader: loader,
		accountReader: func(
			ctx context.Context,
			_ appserver.ConfirmedHome,
			options appserver.ProcessOptions,
		) (*appserver.AccountSnapshot, error) {
			if err := options.BeforeStart(ctx); err != nil {
				return nil, err
			}
			processStarts++
			return &appserver.AccountSnapshot{Type: "chatgpt"}, nil
		},
	}

	account, err := runtime.AccountSnapshot(context.Background(), agentprovider.Scope{Provider: agentprovider.Codex})
	if !errors.Is(err, ErrApplicationLifecycleRuntime) || account.Account != nil {
		t.Fatalf("AccountSnapshot(switched at start guard) = %#v, %v", account, err)
	}
	if processStarts != 0 {
		t.Fatalf("simulated App Server starts = %d, want 0", processStarts)
	}
}

func TestAccountSnapshotDiscardsResultAfterConcurrentConfirmedHomeSwitch(t *testing.T) {
	t.Parallel()

	homeA := t.TempDir()
	homeB := t.TempDir()
	snapshotA := accountRuntimePreferences(t, homeA, 21)
	snapshotB := accountRuntimePreferences(t, homeB, 22)
	loader := &accountMutablePreferencesLoader{snapshot: snapshotA}
	readerStarted := make(chan appserver.ConfirmedHome, 1)
	releaseReader := make(chan struct{})
	runtime := &applicationLifecycleRuntime{
		settingsLoader: loader,
		accountReader: func(
			ctx context.Context,
			home appserver.ConfirmedHome,
			options appserver.ProcessOptions,
		) (*appserver.AccountSnapshot, error) {
			if err := options.BeforeStart(ctx); err != nil {
				return nil, err
			}
			readerStarted <- home
			<-releaseReader
			email := "wrong@example.com"
			plan := "pro"
			return &appserver.AccountSnapshot{
				Type: "chatgpt", Email: &email, PlanType: &plan,
			}, nil
		},
	}

	type result struct {
		hasAccount bool
		err        error
	}
	done := make(chan result, 1)
	go func() {
		account, err := runtime.AccountSnapshot(context.Background(), agentprovider.Scope{Provider: agentprovider.Codex})
		done <- result{hasAccount: account.Account != nil, err: err}
	}()
	startedHome := <-readerStarted
	if startedHome.Generation != 21 ||
		startedHome.Path != snapshotA.CodexHome.Source.Path ||
		startedHome.DeviceID != snapshotA.CodexHome.Source.DeviceID ||
		startedHome.Inode != snapshotA.CodexHome.Source.Inode {
		t.Fatalf("reader confirmed Home = %#v", startedHome)
	}
	loader.set(snapshotB)
	close(releaseReader)

	got := <-done
	if !errors.Is(got.err, ErrApplicationLifecycleRuntime) || got.hasAccount {
		t.Fatalf("AccountSnapshot(switched after read) hasAccount=%t, error=%v", got.hasAccount, got.err)
	}
}

type accountSequencePreferencesLoader struct {
	mu        sync.Mutex
	snapshots []preferences.Snapshot
	next      int
}

func (loader *accountSequencePreferencesLoader) LoadPreferences(
	ctx context.Context,
) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.Lock()
	defer loader.mu.Unlock()
	index := loader.next
	if index >= len(loader.snapshots) {
		index = len(loader.snapshots) - 1
	}
	loader.next++
	return loader.snapshots[index], nil
}

type accountMutablePreferencesLoader struct {
	mu       sync.RWMutex
	snapshot preferences.Snapshot
}

func (loader *accountMutablePreferencesLoader) LoadPreferences(
	ctx context.Context,
) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.RLock()
	defer loader.mu.RUnlock()
	return loader.snapshot, nil
}

func (loader *accountMutablePreferencesLoader) set(snapshot preferences.Snapshot) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.snapshot = snapshot
}

func accountRuntimePreferences(
	t testing.TB,
	home string,
	generation uint64,
) preferences.Snapshot {
	t.Helper()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	directory, err := os.Open(canonicalHome)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer directory.Close()
	identity, err := homeidentity.FromDescriptor(int(directory.Fd()))
	if err != nil {
		t.Fatalf("homeidentity.FromDescriptor() error = %v", err)
	}
	return preferences.Snapshot{CodexHome: preferences.CodexHomePreferences{
		Source: preferences.ConfirmedSource{
			Path:          canonicalHome,
			DeviceID:      identity.DeviceID,
			Inode:         identity.Inode,
			ConfirmedAtMS: 1,
		},
		Generation:   generation,
		DataStoreKey: "synthetic",
	}}
}
