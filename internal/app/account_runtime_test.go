package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptiontier"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestConfirmedApplicationAccountUsesBindingDisplay(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	email := "person@example.com"
	plan := "prolite"
	reader := &accountBindingScriptedReader{accountIDs: []string{"acct-test-a"}}
	sandwichCalls := 0
	account, err := newAccountBindingRuntime(
		repository,
		reader,
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			sandwichCalls++
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID("acct-test-a"),
				AfterID:                  appserver.SensitiveAccountID("acct-test-a"),
				BeforeRateLimitPlanTypes: []string{plan},
				AfterRateLimitPlanTypes:  []string{plan},
				Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	if err := account.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runtime := &applicationLifecycleRuntime{
		repository: repository,
		quota:      &applicationQuotaRuntime{account: account},
	}
	snapshot, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || snapshot.Account == nil || snapshot.Account.Email == nil ||
		*snapshot.Account.Email != email || snapshot.Account.PlanType == nil ||
		*snapshot.Account.PlanType != plan || snapshot.Binding == nil ||
		snapshot.ProTier == nil || snapshot.ProTier.State != subscriptiontier.StateKnown ||
		snapshot.ProTier.Tier == nil || *snapshot.ProTier.Tier != subscriptiontier.Tier5X ||
		snapshot.Binding.State != store.CodexAccountBindingConfirmed ||
		snapshot.Binding.AccountScope == nil ||
		len(*snapshot.Binding.AccountScope) != 64 ||
		strings.Contains(*snapshot.Binding.AccountScope, "acct-test-a") ||
		snapshot.Subscription == nil || !snapshot.Subscription.Current ||
		snapshot.Subscription.DisplayEmail == nil || *snapshot.Subscription.DisplayEmail != email {
		t.Fatalf("AccountSnapshot() = %#v, %v", snapshot, err)
	}

	plan = "pro"
	unchanged, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || unchanged.ProTier == nil || unchanged.ProTier.Tier == nil ||
		*unchanged.ProTier.Tier != subscriptiontier.Tier5X || sandwichCalls != 1 {
		t.Fatalf("cached AccountSnapshot = %#v, %v; sandwich calls = %d", unchanged, err, sandwichCalls)
	}
	refreshAt := quotaRuntimeNowMS + 1_000
	scope := *snapshot.Binding.AccountScope
	if err := repository.UpsertSourceState(context.Background(), store.SourceState{
		SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
		SourceType:       store.QuotaSourceTypeAppServerRateLimits,
		ScopeKey:         scope,
		LastAttemptAtMS:  &refreshAt,
		LastSuccessAtMS:  &refreshAt,
		FreshnessState:   store.SourceFreshnessCurrent,
		UpdatedAtMS:      refreshAt,
	}); err != nil {
		t.Fatalf("UpsertSourceState(quota refreshed) error = %v", err)
	}
	refreshed, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || refreshed.ProTier == nil || refreshed.ProTier.Tier == nil ||
		*refreshed.ProTier.Tier != subscriptiontier.Tier20X {
		t.Fatalf("AccountSnapshot(refreshed tier) = %#v, %v", refreshed, err)
	}
	_, err = runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.calls.Load(); got != 1 || sandwichCalls != 2 {
		t.Fatalf("account probes = rate-limits:%d sandwich:%d, want startup-only/one per quota cycle", got, sandwichCalls)
	}
}

func TestConfirmedAccountSnapshotProbesAndTransitionsToNewAccount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository := openAccountBindingTestRepository(t)
	key, _, scopeB := accountBindingTestScopes(t, repository)
	email := "b@example.com"
	plan := "pro"
	reader := &accountBindingScriptedReader{accountIDs: []string{
		"acct-test-a", "acct-test-b", "acct-test-b",
	}}
	sandwichCalls := 0
	account, err := newAccountBindingRuntime(
		repository,
		reader,
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			sandwichCalls++
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID("acct-test-b"),
				AfterID:                  appserver.SensitiveAccountID("acct-test-b"),
				BeforeRateLimitPlanTypes: []string{plan},
				AfterRateLimitPlanTypes:  []string{plan},
				Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	if err := account.Start(ctx); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	runtime := &applicationLifecycleRuntime{
		repository: repository,
		quota:      &applicationQuotaRuntime{account: account},
	}
	snapshot, err := runtime.AccountSnapshot(ctx, lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || snapshot.Binding == nil || snapshot.Binding.AccountScope == nil ||
		*snapshot.Binding.AccountScope != scopeB || snapshot.Account == nil ||
		snapshot.Account.Email == nil || *snapshot.Account.Email != email ||
		snapshot.ProTier == nil || snapshot.ProTier.Tier == nil ||
		*snapshot.ProTier.Tier != subscriptiontier.Tier20X {
		t.Fatalf("AccountSnapshot(B) = %#v, %v", snapshot, err)
	}
	if got := reader.calls.Load(); got != 1 || sandwichCalls != 1 {
		t.Fatalf("B confirmation probes = rate-limits:%d sandwich:%d, want startup-only/one", got, sandwichCalls)
	}
}

func TestAccountSnapshotConfirmsPendingBindingWithOneSandwich(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := openAccountBindingTestRepository(t)
	key, _, scopeB := accountBindingTestScopes(t, repository)
	if _, err := repository.MarkCodexAccountBindingPending(ctx, quotaRuntimeNowMS, store.CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	reader := &accountBindingScriptedReader{accountIDs: []string{"acct-test-b"}}
	sandwichCalls := 0
	email := "b@example.com"
	account, err := newAccountBindingRuntime(
		repository, reader, key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{}, nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			sandwichCalls++
			return appserver.AccountSandwich{
				BeforeID: appserver.SensitiveAccountID("acct-test-b"),
				AfterID:  appserver.SensitiveAccountID("acct-test-b"),
				Account:  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &applicationLifecycleRuntime{repository: repository, quota: &applicationQuotaRuntime{account: account}}
	snapshot, err := runtime.AccountSnapshot(ctx, lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || snapshot.Binding == nil || snapshot.Binding.State != store.CodexAccountBindingConfirmed ||
		snapshot.Binding.AccountScope == nil || *snapshot.Binding.AccountScope != scopeB ||
		snapshot.Account == nil || snapshot.Account.Email == nil || *snapshot.Account.Email != email {
		t.Fatalf("confirmed pending snapshot = %#v, %v", snapshot, err)
	}
	if got := reader.calls.Load(); got != 0 || sandwichCalls != 1 {
		t.Fatalf("pending confirmation probes = rate-limits:%d sandwich:%d, want 0/1", got, sandwichCalls)
	}
}

func TestConcurrentAccountSnapshotsShareAccountSandwich(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repository := openAccountBindingTestRepository(t)
	key, _, scope := accountBindingTestScopes(t, repository)
	reader := &accountBindingScriptedReader{accountIDs: []string{"acct-test-b"}}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	email := "b@example.com"
	account, err := newAccountBindingRuntime(
		repository, reader, key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{}, nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-release
			}
			return appserver.AccountSandwich{
				BeforeID: appserver.SensitiveAccountID("acct-test-b"),
				AfterID:  appserver.SensitiveAccountID("acct-test-b"),
				Account:  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &applicationLifecycleRuntime{repository: repository, quota: &applicationQuotaRuntime{account: account}}
	type result struct {
		snapshot core.AccountSnapshot
		err      error
	}
	first := make(chan result, 1)
	second := make(chan result, 1)
	go func() {
		snapshot, err := runtime.AccountSnapshot(ctx, lifecycleAccountQuery(agentprovider.Codex))
		first <- result{snapshot, err}
	}()
	<-started
	go func() {
		snapshot, err := runtime.AccountSnapshot(ctx, lifecycleAccountQuery(agentprovider.Codex))
		second <- result{snapshot, err}
	}()
	select {
	case <-second:
		t.Fatal("second account query returned before the shared read completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for _, done := range []<-chan result{first, second} {
		got := <-done
		if got.err != nil || got.snapshot.Binding == nil || got.snapshot.Binding.AccountScope == nil ||
			*got.snapshot.Binding.AccountScope != scope || got.snapshot.Account == nil ||
			got.snapshot.Account.Email == nil || *got.snapshot.Account.Email != email {
			t.Fatalf("shared AccountSnapshot() = %#v, %v", got.snapshot, got.err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("account sandwich calls = %d, want 1", got)
	}
}

func TestAccountSnapshotRetriesWhenSharedCallerIsCanceled(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	reader := &accountBindingScriptedReader{accountIDs: []string{"acct-test-b"}}
	started := make(chan struct{})
	var calls atomic.Int32
	email := "b@example.com"
	account, err := newAccountBindingRuntime(
		repository, reader, key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{}, nil,
		func(ctx context.Context) (appserver.AccountSandwich, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return appserver.AccountSandwich{}, ctx.Err()
			}
			return appserver.AccountSandwich{
				BeforeID: appserver.SensitiveAccountID("acct-test-b"),
				AfterID:  appserver.SensitiveAccountID("acct-test-b"),
				Account:  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &applicationLifecycleRuntime{repository: repository, quota: &applicationQuotaRuntime{account: account}}
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	defer cancelFirst()
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() {
		_, err := runtime.AccountSnapshot(firstCtx, lifecycleAccountQuery(agentprovider.Codex))
		first <- err
	}()
	<-started
	go func() {
		snapshot, err := runtime.AccountSnapshot(t.Context(), lifecycleAccountQuery(agentprovider.Codex))
		if err == nil && (snapshot.Account == nil || snapshot.Account.Email == nil ||
			*snapshot.Account.Email != email) {
			err = errors.New("retry did not return the current account")
		}
		second <- err
	}()
	select {
	case <-second:
		t.Fatal("second account query returned before the first was canceled")
	case <-time.After(50 * time.Millisecond):
	}
	cancelFirst()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first account query error = %v, want canceled", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second account query error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("account sandwich calls = %d, want 2", got)
	}
}

func TestAccountSnapshotPendingOmitsAccountKeepsBinding(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	pending, err := repository.MarkCodexAccountBindingPending(
		context.Background(), quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	)
	if err != nil {
		t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
	}
	runtime := &applicationLifecycleRuntime{repository: repository}
	snapshot, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
	if err != nil || snapshot.Account != nil || snapshot.Binding == nil ||
		snapshot.Binding.State != store.CodexAccountBindingPending ||
		snapshot.Binding.BindingGeneration != pending.BindingGeneration ||
		snapshot.Binding.AccountScope != nil {
		t.Fatalf("pending AccountSnapshot() = %#v, %v", snapshot, err)
	}
}

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

	account, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
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
		lifecycleAccountQuery(agentprovider.Grok),
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(grok) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Grok ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType != nil || account.Binding != nil {
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
		lifecycleAccountQuery(agentprovider.Grok),
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
		lifecycleAccountQuery(agentprovider.Grok),
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
		lifecycleAccountQuery(agentprovider.Cursor),
	)
	if err != nil {
		t.Fatalf("AccountSnapshot(cursor) error = %v", err)
	}
	if account.Account == nil || account.Account.Type != agentprovider.Cursor ||
		account.Account.Email == nil || *account.Account.Email != "person@example.com" ||
		account.Account.PlanType == nil || *account.Account.PlanType != "pro" ||
		account.Binding != nil {
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

	account, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
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
		account, err := runtime.AccountSnapshot(context.Background(), lifecycleAccountQuery(agentprovider.Codex))
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
	return preferences.Snapshot{
		CodexHome: preferences.CodexHomePointer(preferences.CodexHomePreferences{
			Source: preferences.ConfirmedSource{
				Path:          canonicalHome,
				DeviceID:      identity.DeviceID,
				Inode:         identity.Inode,
				ConfirmedAtMS: 1,
			},
			Generation:   generation,
			DataStoreKey: "synthetic",
		}),
		CodexAccounts: preferences.DefaultCodexAccountPreferences(),
	}
}

func lifecycleAccountQuery(provider string) core.AccountSnapshotQuery {
	return core.AccountSnapshotQuery{
		Scope:         agentprovider.Scope{Provider: provider},
		EvaluatedAtMS: quotaRuntimeNowMS,
		TimeZone:      "UTC",
	}
}
