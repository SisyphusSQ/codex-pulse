package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/gorm"
)

const (
	accountBindingSecretA      = "acct-secret-a"
	accountBindingSecretB      = "acct-secret-b"
	accountBindingSecretToken  = "token-secret-a"
	accountBindingSecretCredit = "credit-secret-a"
)

func TestAccountBindingSyntheticABAEndToEnd(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	defer closeQuotaRuntimeStore(t, database)
	key := quotaRuntimeTestScopeKey()
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, quotaRuntimeNowMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scopeA, err := accountbinding.DeriveScope(stored, []byte(accountBindingSecretA))
	if err != nil {
		t.Fatalf("DeriveScope(A) error = %v", err)
	}
	scopeB, err := accountbinding.DeriveScope(stored, []byte(accountBindingSecretB))
	if err != nil {
		t.Fatalf("DeriveScope(B) error = %v", err)
	}
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeA, quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	)
	if err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}
	reader := &accountBindingE2EReader{
		accountID: accountBindingSecretA,
		used:      25,
		creditID:  accountBindingSecretCredit,
	}
	home := writeSyntheticAuthHome(t, accountBindingSecretToken)
	loader := &quotaRuntimePreferencesLoader{snapshot: enabledQuotaRuntimePreferences(t, home)}
	var nowMS int64 = quotaRuntimeNowMS
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Reader:      reader,
		ScopeKey:    stored,
		Binding: quotaonline.AccountBindingFence{
			AccountScope: scopeA, BindingGeneration: binding.BindingGeneration,
		},
		Clock: func() time.Time { return time.UnixMilli(nowMS).UTC() },
	})
	if err != nil || runtime == nil || runtime.account == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceAppServer(scopeA), func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil
	})
	firstA := queryBoundCurrent(t, repository, nowMS)
	if firstA.AccountScope != scopeA || firstA.Binding.BindingGeneration <= 0 ||
		len(firstA.Windows) == 0 || firstA.Windows[0].UsedPercent == nil || *firstA.Windows[0].UsedPercent != 25 {
		t.Fatalf("first A current = %#v", firstA)
	}
	if len(firstA.Windows[0].Explanations) == 0 {
		t.Fatalf("first A missing observation timestamp: %#v", firstA)
	}
	firstObserved := firstA.Windows[0].Explanations[0].ObservedAtMS
	encodeAndScanPrivacy(t, firstA, accountSnapshotFromRuntime(t, runtime), scopeA, scopeB)

	reader.set(accountBindingSecretB, 77)
	reader.failIdentityOn = 2
	nowMS++
	if err := runtime.account.HandleObservedScopeChange(context.Background()); err == nil {
		t.Fatal("HandleObservedScopeChange(B first fail) error = nil")
	}
	if runtime.account.Active() != nil {
		t.Fatalf("active after B first fail = %#v", runtime.account.Active())
	}
	pending := queryBoundCurrent(t, repository, nowMS)
	if pending.Binding.State != store.CodexAccountBindingPending || len(pending.Windows) != 0 ||
		pending.ResetCredits.AvailableCount != nil {
		t.Fatalf("pending B leaked A facts: %#v", pending)
	}
	encodeAndScanPrivacy(t, pending, accountSnapshotFromRuntime(t, runtime), scopeA, scopeB)

	reader.failIdentityOn = 0
	nowMS++
	if err := runtime.account.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("HandleObservedScopeChange(B success) error = %v", err)
	}
	requestBoundQuotaRefresh(t, runtime)
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceAppServer(scopeB), func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil
	})
	activeB := queryBoundCurrent(t, repository, nowMS)
	if activeB.AccountScope != scopeB || activeB.Binding.BindingGeneration <= firstA.Binding.BindingGeneration ||
		len(activeB.Windows) == 0 || activeB.Windows[0].UsedPercent == nil || *activeB.Windows[0].UsedPercent != 77 {
		t.Fatalf("B current = %#v", activeB)
	}
	if windowHasUsed(activeB, 25) {
		t.Fatal("B current still contains A used percent")
	}
	encodeAndScanPrivacy(t, activeB, accountSnapshotFromRuntime(t, runtime), scopeA, scopeB)

	reader.set(accountBindingSecretA, 25)
	nowMS++
	if err := runtime.account.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("HandleObservedScopeChange(A restore) error = %v", err)
	}
	restored := queryBoundCurrent(t, repository, nowMS)
	if restored.AccountScope != scopeA || restored.Binding.BindingGeneration <= activeB.Binding.BindingGeneration ||
		len(restored.Windows) == 0 || restored.Windows[0].UsedPercent == nil || *restored.Windows[0].UsedPercent != 25 ||
		restored.Windows[0].Explanations[0].ObservedAtMS != firstObserved {
		t.Fatalf("restored A = %#v firstObserved=%d", restored, firstObserved)
	}
	if windowHasUsed(restored, 77) {
		t.Fatal("restored A contains B used percent")
	}
	encodeAndScanPrivacy(t, restored, accountSnapshotFromRuntime(t, runtime), scopeA, scopeB)
	scanRepositoryPrivacy(t, database, scopeA, scopeB)

	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAccountBindingCrashPendingDoesNotRestoreOldAccount(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	database, repository := openQuotaRuntimeStoreAt(t, path)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeA, quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(
		context.Background(), quotaRuntimeNowMS+1, store.CodexAccountBindingReasonAccountChanged,
	); err != nil {
		t.Fatalf("MarkPending() error = %v", err)
	}
	closeQuotaRuntimeStore(t, database)

	database, repository = openQuotaRuntimeStoreAt(t, path)
	defer closeQuotaRuntimeStore(t, database)
	current, err := repository.CodexAccountBinding(context.Background())
	if err != nil || current.State != store.CodexAccountBindingPending || current.AccountScope != nil {
		t.Fatalf("reopened pending = %#v, %v", current, err)
	}
	response := queryBoundCurrent(t, repository, quotaRuntimeNowMS+2)
	if response.Binding.State != store.CodexAccountBindingPending || len(response.Windows) != 0 {
		t.Fatalf("crash-pending query restored A: %#v", response)
	}
	runtime := mustAccountBindingRuntime(
		t, repository, key, &accountBindingScriptedReader{accountIDs: []string{accountBindingSecretB, accountBindingSecretB}},
		&accountBindingTestQuota{},
	)
	if runtime.Active() != nil {
		t.Fatalf("restarted runtime reused A before Start: %#v", runtime.Active())
	}
	_ = scopeA
}

func TestAccountBindingCrashAfterConfirmedBReplansSameScope(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	database, repository := openQuotaRuntimeStoreAt(t, path)
	key, _, scopeB := accountBindingTestScopes(t, repository)
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeB, quotaRuntimeNowMS, store.CodexAccountBindingReasonAccountChanged,
	)
	if err != nil {
		t.Fatalf("Confirm(B) error = %v", err)
	}
	if _, err := repository.SourceRefreshSchedule(context.Background(), store.QuotaSourceInstanceAppServer(scopeB)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pre-replan schedule = %v, want not found", err)
	}
	closeQuotaRuntimeStore(t, database)

	database, repository = openQuotaRuntimeStoreAt(t, path)
	defer closeQuotaRuntimeStore(t, database)
	home := writeSyntheticAuthHome(t, "synthetic-runtime-access-token")
	loader := &quotaRuntimePreferencesLoader{snapshot: enabledQuotaRuntimePreferences(t, home)}
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Reader:      &quotaRuntimeAccountReader{accountID: "acct-test-b"},
		ScopeKey:    key,
		Binding: quotaonline.AccountBindingFence{
			AccountScope: scopeB, BindingGeneration: binding.BindingGeneration,
		},
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS + 1).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("start after crash B = %#v, %v", runtime, err)
	}
	if err := runtime.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences(restart B) error = %v", err)
	}
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceAppServer(scopeB), func(schedule store.SourceRefreshSchedule) bool {
		return schedule.ScopeKey == scopeB && schedule.BindingGeneration == binding.BindingGeneration &&
			schedule.NextDueAtMS != nil
	})
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAccountBindingCrashAbandonsActiveClaim(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	database, repository := openQuotaRuntimeStoreAt(t, path)
	_, scopeA, _ := accountBindingTestScopes(t, repository)
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeA, quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	)
	if err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}
	due := int64(quotaRuntimeNowMS)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID:  store.QuotaSourceInstanceAppServer(scopeA),
		SourceType:        store.QuotaSourceTypeAppServerRateLimits,
		ScopeKey:          scopeA,
		BindingGeneration: binding.BindingGeneration,
		ExpectedRevision:  0,
		NextDueAtMS:       &due,
		Reason:            store.RefreshReasonStartup,
		AtMS:              quotaRuntimeNowMS,
	})
	if err != nil {
		t.Fatalf("Upsert schedule error = %v", err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), schedule.SourceInstanceID, schedule.Revision,
		"crashed-claim-a", store.RefreshTriggerScheduled, quotaRuntimeNowMS, 1, binding.BindingGeneration,
	)
	if err != nil || !ok || claimed.ActiveClaimID == nil {
		t.Fatalf("Claim() = %#v, %v, %v", claimed, ok, err)
	}
	closeQuotaRuntimeStore(t, database)

	database, repository = openQuotaRuntimeStoreAt(t, path)
	defer closeQuotaRuntimeStore(t, database)
	expired, err := repository.ListExpiredSourceRefreshClaims(context.Background(), quotaRuntimeNowMS+10, 10)
	if err != nil || len(expired) != 1 || expired[0].ActiveClaimID == nil ||
		*expired[0].ActiveClaimID != "crashed-claim-a" {
		t.Fatalf("expired claims = %#v, %v", expired, err)
	}
	if _, released, err := repository.ReleaseExpiredSourceRefreshClaim(context.Background(), store.SourceRefreshClaimRecovery{
		SourceInstanceID: claimed.SourceInstanceID, ClaimID: "crashed-claim-a",
		ExpectedRevision: claimed.Revision, AtMS: quotaRuntimeNowMS + 10,
	}); err != nil || !released {
		t.Fatalf("ReleaseExpired() released=%v err=%v", released, err)
	}
	schedule, err = repository.SourceRefreshSchedule(context.Background(), claimed.SourceInstanceID)
	if err != nil || schedule.ActiveClaimID != nil {
		t.Fatalf("schedule after abandon = %#v, %v", schedule, err)
	}
}

func TestAccountBindingRestartReusesScopeKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	database, repository := openQuotaRuntimeStoreAt(t, path)
	key := quotaRuntimeTestScopeKey()
	first, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, quotaRuntimeNowMS)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	scopeA, err := accountbinding.DeriveScope(first, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	closeQuotaRuntimeStore(t, database)

	database, repository = openQuotaRuntimeStoreAt(t, path)
	defer closeQuotaRuntimeStore(t, database)
	drifted := bytes.Repeat([]byte{0x99}, 32)
	var candidate [32]byte
	copy(candidate[:], drifted)
	second, err := repository.EnsureCodexAccountScopeKey(context.Background(), candidate, quotaRuntimeNowMS+1)
	if err != nil {
		t.Fatalf("Ensure(reopen) error = %v", err)
	}
	if second != first {
		t.Fatalf("scope key drifted after restart: %x vs %x", second, first)
	}
	again, err := accountbinding.DeriveScope(second, []byte("acct-test-a"))
	if err != nil || again != scopeA {
		t.Fatalf("scope drifted: %q vs %q", again, scopeA)
	}
}

func TestAccountBindingBackupRestoreKeepsV32Snapshot(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join(t.TempDir(), "source", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	database, repository := openQuotaRuntimeStoreAt(t, sourcePath)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a"},
	}, &accountBindingTestQuota{})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	due := int64(quotaRuntimeNowMS + 5_000)
	if _, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID:  store.QuotaSourceInstanceAppServer(scopeA),
		SourceType:        store.QuotaSourceTypeAppServerRateLimits,
		ScopeKey:          scopeA,
		BindingGeneration: runtime.Active().Generation,
		ExpectedRevision:  0,
		NextDueAtMS:       &due,
		Reason:            store.RefreshReasonStartup,
		AtMS:              quotaRuntimeNowMS,
	}); err != nil {
		t.Fatalf("Upsert schedule error = %v", err)
	}
	closeQuotaRuntimeStore(t, database)

	backupPath := filepath.Join(t.TempDir(), "backup", "codex-pulse.db")
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	database, repository = openQuotaRuntimeStoreAt(t, backupPath)
	defer closeQuotaRuntimeStore(t, database)
	current, err := repository.CodexAccountBinding(context.Background())
	if err != nil || current.State != store.CodexAccountBindingConfirmed ||
		current.AccountScope == nil || *current.AccountScope != scopeA {
		t.Fatalf("restored binding = %#v, %v", current, err)
	}
	schedule, err := repository.SourceRefreshSchedule(context.Background(), store.QuotaSourceInstanceAppServer(scopeA))
	if err != nil || schedule.ScopeKey != scopeA || schedule.BindingGeneration != current.BindingGeneration {
		t.Fatalf("restored schedule = %#v, %v", schedule, err)
	}
}

type accountBindingE2EReader struct {
	mu             sync.Mutex
	accountID      string
	used           int32
	creditID       string
	identityCalls  int
	failIdentityOn int
}

func (reader *accountBindingE2EReader) set(accountID string, used int32) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.accountID = accountID
	reader.used = used
	reader.identityCalls = 0
}

func (reader *accountBindingE2EReader) Read(
	ctx context.Context,
	excludeResetCreditDetails bool,
) (appserver.AccountRateLimitsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return appserver.AccountRateLimitsSnapshot{}, err
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if excludeResetCreditDetails {
		reader.identityCalls++
		if reader.failIdentityOn != 0 && reader.identityCalls == reader.failIdentityOn {
			return appserver.AccountRateLimitsSnapshot{}, errors.New("synthetic confirmation failed")
		}
	}
	snapshot := quotaRuntimeAccountSnapshot(reader.accountID)
	if snapshot.RateLimits.Primary != nil {
		snapshot.RateLimits.Primary.UsedPercent = reader.used
	}
	if snapshot.RateLimits.Secondary != nil {
		snapshot.RateLimits.Secondary.UsedPercent = reader.used
	}
	if snapshot.RateLimitResetCredits != nil && len(snapshot.RateLimitResetCredits.Credits) > 0 {
		snapshot.RateLimitResetCredits.Credits[0].ID = reader.creditID
	}
	return snapshot, nil
}

func requestBoundQuotaRefresh(t testing.TB, runtime *applicationQuotaRuntime) {
	t.Helper()
	for _, source := range []quotaonline.RefreshSource{
		quotaonline.RefreshSourceQuota,
		quotaonline.RefreshSourceResetCredits,
	} {
		if _, err := runtime.RequestRefresh(context.Background(), source, store.RefreshTriggerManual); err != nil {
			t.Fatalf("RequestRefresh(%s) error = %v", source, err)
		}
	}
}

func queryBoundCurrent(t testing.TB, repository *store.Repository, evaluatedAtMS int64) quotaonline.CurrentResponse {
	t.Helper()
	service, err := quotaonline.NewCurrentQueryService(repository)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	response, err := service.Query(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("Query(%d) error = %v", evaluatedAtMS, err)
	}
	return response
}

func accountSnapshotFromRuntime(t testing.TB, runtime *applicationQuotaRuntime) core.AccountSnapshot {
	t.Helper()
	binding, err := runtime.account.repository.CodexAccountBinding(context.Background())
	if err != nil {
		t.Fatalf("CodexAccountBinding() error = %v", err)
	}
	snapshot := core.AccountSnapshot{Binding: &binding}
	if binding.State == store.CodexAccountBindingConfirmed {
		if display, err := runtime.account.LoadDisplay(context.Background()); err == nil && display != nil {
			snapshot.Account = &core.AccountIdentity{Type: display.Type, Email: display.Email, PlanType: display.PlanType}
		}
	}
	return snapshot
}

func encodeAndScanPrivacy(
	t testing.TB,
	current quotaonline.CurrentResponse,
	account core.AccountSnapshot,
	scopes ...string,
) {
	t.Helper()
	quota := &corev1.QuotaCurrentResponse{}
	if err := core.EncodeResponse(runtimeinfo.QuotaCurrentResponse{Current: current}, quota); err != nil {
		t.Fatalf("EncodeResponse(quota) error = %v", err)
	}
	encodedQuota, err := protojson.Marshal(quota)
	if err != nil {
		t.Fatal(err)
	}
	accountProto := &corev1.AccountSnapshotResponse{}
	if err := core.EncodeResponse(account, accountProto); err != nil {
		t.Fatalf("EncodeResponse(account) error = %v", err)
	}
	encodedAccount, err := protojson.Marshal(accountProto)
	if err != nil {
		t.Fatal(err)
	}
	jsonCurrent, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	scanPrivacyBytes(t, encodedQuota, encodedAccount, jsonCurrent)
	for _, scope := range scopes {
		if scope != "" && !strings.Contains(string(encodedQuota)+string(encodedAccount), scope) &&
			current.Binding.State == store.CodexAccountBindingConfirmed && current.AccountScope == scope {
			t.Fatalf("confirmed proto omitted current scope")
		}
	}
}

func scanRepositoryPrivacy(t testing.TB, database *storesqlite.Store, allowedScopes ...string) {
	t.Helper()
	var dump strings.Builder
	err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		var names []string
		if err := connection.WithContext(ctx).Raw(
			`SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`,
		).Scan(&names).Error; err != nil {
			return err
		}
		for _, name := range names {
			rows, err := connection.WithContext(ctx).Raw(`SELECT * FROM ` + name).Rows()
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				_ = rows.Close()
				return err
			}
			for rows.Next() {
				values := make([]any, len(columns))
				dest := make([]any, len(columns))
				for index := range values {
					dest[index] = &values[index]
				}
				if err := rows.Scan(dest...); err != nil {
					_ = rows.Close()
					return err
				}
				for _, value := range values {
					switch typed := value.(type) {
					case []byte:
						dump.Write(typed)
						dump.WriteByte('\n')
					case string:
						dump.WriteString(typed)
						dump.WriteByte('\n')
					}
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("dump sqlite privacy view: %v", err)
	}
	content := []byte(dump.String())
	scanPrivacyBytes(t, content)
	for _, scope := range allowedScopes {
		if !bytes.Contains(content, []byte(scope)) {
			t.Fatalf("sqlite dump missing allowed scope %s", scope)
		}
	}
	if path := database.Config().Path; path != "" {
		for _, extra := range []string{path, path + "-wal", path + "-shm"} {
			raw, readErr := os.ReadFile(extra)
			if readErr != nil {
				if errors.Is(readErr, os.ErrNotExist) {
					continue
				}
				t.Fatal(readErr)
			}
			scanPrivacyBytes(t, raw)
		}
	}
}

func scanPrivacyBytes(t testing.TB, blobs ...[]byte) {
	t.Helper()
	forbidden := []string{
		accountBindingSecretA, accountBindingSecretB, accountBindingSecretToken, accountBindingSecretCredit,
		"acct-test-a", "access_token", "authorization",
	}
	for _, blob := range blobs {
		text := string(blob)
		for _, secret := range forbidden {
			if strings.Contains(text, secret) {
				t.Fatalf("privacy leak %q in %q", secret, trimPrivacySample(text, secret))
			}
		}
	}
}

func trimPrivacySample(text, secret string) string {
	index := strings.Index(text, secret)
	if index < 0 {
		return ""
	}
	start := index - 24
	if start < 0 {
		start = 0
	}
	end := index + len(secret) + 24
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func windowHasUsed(response quotaonline.CurrentResponse, used float64) bool {
	for _, window := range response.Windows {
		if window.UsedPercent != nil && *window.UsedPercent == used {
			return true
		}
	}
	return false
}

func openQuotaRuntimeStoreAt(t testing.TB, path string) (*storesqlite.Store, *store.Repository) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open(%q) error = %v", path, err)
	}
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		_ = database.Close(context.Background())
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return database, repository
}

func closeQuotaRuntimeStore(t testing.TB, database *storesqlite.Store) {
	t.Helper()
	if database == nil {
		return
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}
