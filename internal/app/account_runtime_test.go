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
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
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

func TestAccountSnapshotReadsGrokIdentityFromAuthWhitelist(t *testing.T) {
	runtime := &applicationLifecycleRuntime{
		grokAccountReader: func() (grokprovider.AccountSnapshot, error) {
			return grokprovider.AccountSnapshot{Email: "person@example.com", PrincipalType: "User"}, nil
		},
	}
	account, err := runtime.AccountSnapshot(
		context.Background(),
		agentprovider.Scope{Provider: agentprovider.Grok},
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(grok) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Grok ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType != nil {
		t.Fatalf("AccountSnapshot(grok) = %#v", account)
	}
}

func TestAccountSnapshotUsesGrokSubscriptionProfile(t *testing.T) {
	runtime := &applicationLifecycleRuntime{
		grokAccountReader: func() (grokprovider.AccountSnapshot, error) {
			return grokprovider.AccountSnapshot{Email: "cached@example.com", PrincipalType: "User"}, nil
		},
		grokProfileReader: func(context.Context) (grokprovider.AccountSnapshot, error) {
			return grokprovider.AccountSnapshot{
				Email: "person@example.com", PrincipalType: "User", Subscription: "GrokPro",
			}, nil
		},
	}
	account, err := runtime.AccountSnapshot(
		context.Background(),
		agentprovider.Scope{Provider: agentprovider.Grok},
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(grok) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Grok ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType == nil || *account.Account.PlanType != "GrokPro" {
		t.Fatalf("AccountSnapshot(grok) = %#v", account.Account)
	}
}

func TestAccountSnapshotCombinesGrokIdentityAndBillingPlan(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "codex-pulse-test.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	if err := repository.ReplaceGrokSnapshot(context.Background(), store.GrokSnapshot{
		Generation:    1,
		CollectedAtMS: 2_000,
	}); err != nil {
		t.Fatalf("ReplaceGrokSnapshot() error = %v", err)
	}
	plan := "SuperGrok Heavy"
	if err := repository.CommitGrokBillingSnapshot(context.Background(), store.GrokBillingSnapshot{
		Generation: 1, CollectedAtMS: 2_000, PeriodType: "weekly",
		PeriodStartMS: 1_000, PeriodEndMS: 8_000, UsedPercent: 12,
		SubscriptionTier: &plan,
	}); err != nil {
		t.Fatalf("CommitGrokBillingSnapshot() error = %v", err)
	}
	runtime := &applicationLifecycleRuntime{
		repository: repository,
		grokAccountReader: func() (grokprovider.AccountSnapshot, error) {
			return grokprovider.AccountSnapshot{Email: "person@example.com", PrincipalType: "User"}, nil
		},
		grokProfileReader: func(context.Context) (grokprovider.AccountSnapshot, error) {
			return grokprovider.AccountSnapshot{}, errors.New("profile unavailable")
		},
	}
	account, err := runtime.AccountSnapshot(
		context.Background(),
		agentprovider.Scope{Provider: agentprovider.Grok},
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(grok) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Grok ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType == nil || *account.Account.PlanType != plan {
		t.Fatalf("AccountSnapshot(grok) = %#v", account.Account)
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
