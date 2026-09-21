package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	quotaRuntimeTestAccountID = "acct-test-a"
	quotaRuntimeReadQuota     = "quota"
	quotaRuntimeReadReset     = "reset_credits"
)

func quotaRuntimeTestScopeKey() [32]byte {
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x42}, 32))
	return key
}

func confirmQuotaRuntimeBinding(
	t testing.TB,
	repository *store.Repository,
	nowMS int64,
) ([32]byte, quotaonline.AccountBindingFence) {
	t.Helper()
	key := quotaRuntimeTestScopeKey()
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, nowMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scope, err := accountbinding.DeriveScope(stored, []byte(quotaRuntimeTestAccountID))
	if err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scope, nowMS, store.CodexAccountBindingReasonStartup,
	)
	if err != nil || binding.AccountScope == nil {
		t.Fatalf("ConfirmCodexAccountBinding() = %#v, %v", binding, err)
	}
	return stored, quotaonline.AccountBindingFence{
		AccountScope: *binding.AccountScope, BindingGeneration: binding.BindingGeneration,
	}
}

func boundQuotaRuntimeFields(
	t testing.TB,
	repository *store.Repository,
	reader quotaonline.AccountRateLimitsReader,
) (quotaonline.AccountRateLimitsReader, [32]byte, quotaonline.AccountBindingFence) {
	t.Helper()
	key, fence := confirmQuotaRuntimeBinding(t, repository, quotaRuntimeNowMS)
	if reader == nil {
		reader = newQuotaRuntimeSuccessReader(nil)
	}
	return reader, key, fence
}

type quotaRuntimeAccountReader struct {
	mu           sync.Mutex
	accountID    string
	missingID    atomic.Bool
	calls        chan<- string
	started      chan struct{}
	block        <-chan struct{}
	homeCalls    chan<- quotaHomeRequestEvent
	homePath     func() string
	failWhenHome func(string) bool
	err          error
	quotaFails   atomic.Bool
}

func newQuotaRuntimeSuccessReader(calls chan<- string) *quotaRuntimeAccountReader {
	return &quotaRuntimeAccountReader{accountID: quotaRuntimeTestAccountID, calls: calls}
}

func (reader *quotaRuntimeAccountReader) Read(
	ctx context.Context,
	excludeResetCreditDetails bool,
) (appserver.AccountRateLimitsSnapshot, error) {
	if reader == nil {
		return appserver.AccountRateLimitsSnapshot{}, errors.New("quota runtime reader is unavailable")
	}
	kind := quotaRuntimeReadQuota
	if !excludeResetCreditDetails {
		kind = quotaRuntimeReadReset
	}
	if reader.calls != nil {
		select {
		case reader.calls <- kind:
		default:
			reader.calls <- kind
		}
	}
	path := ""
	if reader.homePath != nil {
		path = reader.homePath()
	}
	if reader.homeCalls != nil {
		select {
		case reader.homeCalls <- quotaHomeRequestEvent{home: path, kind: kind}:
		default:
		}
	}
	if reader.failWhenHome != nil && reader.failWhenHome(path) {
		return appserver.AccountRateLimitsSnapshot{}, errors.New("synthetic App Server unavailable for current Home")
	}
	if reader.started != nil {
		select {
		case reader.started <- struct{}{}:
		default:
		}
	}
	if reader.block != nil {
		select {
		case <-ctx.Done():
			return appserver.AccountRateLimitsSnapshot{}, ctx.Err()
		case <-reader.block:
		}
	}
	if err := ctx.Err(); err != nil {
		return appserver.AccountRateLimitsSnapshot{}, err
	}
	reader.mu.Lock()
	err := reader.err
	reader.mu.Unlock()
	if err != nil {
		return appserver.AccountRateLimitsSnapshot{}, err
	}
	accountID := reader.accountID
	if reader.missingID.Load() {
		accountID = ""
	}
	return quotaRuntimeAccountSnapshot(accountID), nil
}

func quotaRuntimeAccountSnapshot(accountID string) appserver.AccountRateLimitsSnapshot {
	minsPrimary := int64(300)
	minsSecondary := int64(10_080)
	resetPrimary := quotaRuntimeNowMS/1000 + 3600
	resetSecondary := quotaRuntimeNowMS/1000 + 604800
	plan := "team"
	limitID := "codex"
	granted := int64(1_783_000_000)
	expires := int64(1_784_691_200)
	var account appserver.SensitiveAccountID
	if accountID != "" {
		account = appserver.SensitiveAccountID(accountID)
	}
	return appserver.AccountRateLimitsSnapshot{
		AccountID: account,
		RateLimits: appserver.RateLimitSnapshot{
			LimitID: &limitID, PlanType: &plan,
			Primary:   &appserver.RateLimitWindow{UsedPercent: 25, WindowDurationMins: &minsPrimary, ResetsAtSeconds: &resetPrimary},
			Secondary: &appserver.RateLimitWindow{UsedPercent: 40, WindowDurationMins: &minsSecondary, ResetsAtSeconds: &resetSecondary},
		},
		RateLimitResetCredits: &appserver.RateLimitResetCreditsSummary{
			AvailableCount: 1,
			Credits: []appserver.RateLimitResetCredit{{
				ID: "credit-runtime-synthetic", Status: "available", ResetType: "codexRateLimits",
				GrantedAtSeconds: granted, ExpiresAtSeconds: &expires,
			}},
		},
	}
}

type quotaRuntimePreferencesLoader struct {
	mu       sync.RWMutex
	snapshot preferences.Snapshot
	err      error
}

type quotaRuntimeSequencePreferencesLoader struct {
	mu        sync.Mutex
	snapshots []preferences.Snapshot
	next      int
}

func (loader *quotaRuntimeSequencePreferencesLoader) LoadPreferences(
	ctx context.Context,
) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if len(loader.snapshots) == 0 {
		return preferences.Snapshot{}, errors.New("no synthetic snapshot")
	}
	index := loader.next
	if index >= len(loader.snapshots) {
		index = len(loader.snapshots) - 1
	}
	loader.next++
	return loader.snapshots[index], nil
}

func (loader *quotaRuntimePreferencesLoader) LoadPreferences(ctx context.Context) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	loader.mu.RLock()
	defer loader.mu.RUnlock()
	return loader.snapshot, loader.err
}

func (loader *quotaRuntimePreferencesLoader) setSnapshot(snapshot preferences.Snapshot) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.snapshot = snapshot
}

func writeSyntheticAuthHome(t testing.TB, accessToken string) string {
	t.Helper()
	return writeSyntheticAuthContent(t, []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"synthetic-id","access_token":"`+accessToken+`","refresh_token":"synthetic-refresh","account_id":"synthetic-account"}}`))
}

func writeSyntheticAuthContent(t testing.TB, content []byte) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth.json) error = %v", err)
	}
	return home
}

func quotaRuntimePreferencesForHome(t testing.TB, home string) preferences.Snapshot {
	t.Helper()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	directory, err := os.Open(canonicalHome)
	if err != nil {
		t.Fatalf("os.Open(home) error = %v", err)
	}
	defer directory.Close()
	identity, err := homeidentity.FromDescriptor(int(directory.Fd()))
	if err != nil {
		t.Fatalf("homeidentity.FromDescriptor(home) error = %v", err)
	}
	return preferences.Snapshot{
		CodexHome: preferences.CodexHomePointer(preferences.CodexHomePreferences{
			Source: preferences.ConfirmedSource{
				Path: filepath.Clean(canonicalHome), DeviceID: identity.DeviceID,
				Inode:         identity.Inode,
				ConfirmedAtMS: 1_784_000_000_000,
			},
			Generation: 1, DataStoreKey: preferences.DefaultDataStoreKey,
		}),
		CodexAccounts: preferences.DefaultCodexAccountPreferences(),
	}
}

func withBoundQuotaRuntime(
	t testing.TB,
	repository *store.Repository,
	config ApplicationQuotaRuntimeConfig,
) ApplicationQuotaRuntimeConfig {
	t.Helper()
	reader, key, fence := boundQuotaRuntimeFields(t, repository, config.Reader)
	config.Reader = reader
	config.ScopeKey = key
	config.Binding = fence
	return config
}

func withBoundLifecycleQuota(
	t testing.TB,
	repository *store.Repository,
	config ApplicationLifecycleRuntimeConfig,
) ApplicationLifecycleRuntimeConfig {
	t.Helper()
	reader, key, fence := boundQuotaRuntimeFields(t, repository, config.QuotaReader)
	config.QuotaReader = reader
	config.QuotaScopeKey = key
	config.QuotaBinding = fence
	return config
}

func newQuotaRuntimeHomeReader(
	loader confirmedPreferencesLoader,
	calls chan<- quotaHomeRequestEvent,
) *quotaRuntimeAccountReader {
	reader := newQuotaRuntimeSuccessReader(nil)
	reader.homeCalls = calls
	reader.homePath = func() string {
		snapshot, err := loader.LoadPreferences(context.Background())
		if err != nil {
			return ""
		}
		return snapshot.CodexHome.Source.Path
	}
	return reader
}

func assertQuotaRuntimeHome(t testing.TB, event quotaHomeRequestEvent, home string) {
	t.Helper()
	want := quotaRuntimePreferencesForHome(t, home).CodexHome.Source.Path
	if event.home != want {
		t.Fatalf("quota runtime home = %q, want %q", event.home, want)
	}
}

func quotaRuntimeInstances(t testing.TB, repository *store.Repository) []string {
	t.Helper()
	return []string{
		quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault),
		quotaRuntimeInstance(t, repository, store.ResetCreditsSourceInstanceWhamDefault),
	}
}

func quotaRuntimeInstance(
	t testing.TB,
	repository *store.Repository,
	sourceInstanceID string,
) string {
	t.Helper()
	if sourceInstanceID != store.QuotaSourceInstanceWhamDefault &&
		sourceInstanceID != store.ResetCreditsSourceInstanceWhamDefault {
		return sourceInstanceID
	}
	binding, err := repository.CodexAccountBinding(context.Background())
	if err != nil || binding.AccountScope == nil {
		return sourceInstanceID
	}
	if sourceInstanceID == store.QuotaSourceInstanceWhamDefault {
		return store.QuotaSourceInstanceAppServer(*binding.AccountScope)
	}
	return store.ResetCreditsSourceInstanceAppServer(*binding.AccountScope)
}

func waitForQuotaRuntimeReads(t testing.TB, requests <-chan string, count int) {
	t.Helper()
	seen := make(map[string]int, 2)
	deadline := time.After(2 * time.Second)
	for received := 0; received < count; received++ {
		select {
		case kind := <-requests:
			seen[kind]++
		case <-deadline:
			t.Fatalf("received %d/%d quota runtime reads: %#v", received, count, seen)
		}
	}
	if count < 2 {
		return
	}
	for seen[quotaRuntimeReadQuota] < 1 || seen[quotaRuntimeReadReset] < 1 {
		select {
		case kind := <-requests:
			seen[kind]++
			if seen[quotaRuntimeReadQuota]+seen[quotaRuntimeReadReset] > count+2 {
				t.Fatalf("quota runtime reads = %#v", seen)
			}
		case <-deadline:
			t.Fatalf("quota runtime reads = %#v", seen)
		}
	}
}
