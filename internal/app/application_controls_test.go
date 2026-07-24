package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationControlsUpdateSettingsPreservesReadOnlyPreferences(t *testing.T) {
	t.Parallel()

	runtime, database, preferenceStore := startApplicationControlsTestRuntime(t)
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before update) error = %v", err)
	}
	skippedVersion := "0.9.0"
	snoozeUntilMS := int64(1_784_000_100_000)
	lastCheckAtMS := int64(1_784_000_000_000)
	current.Revision++
	current.Updates.SkippedVersion = &skippedVersion
	current.Updates.SnoozeUntilMS = &snoozeUntilMS
	current.Updates.LastCheckAtMS = &lastCheckAtMS
	if err := preferenceStore.CompareAndSwap(context.Background(), current.Revision-1, current); err != nil {
		t.Fatalf("CompareAndSwap(read-only fixture) error = %v", err)
	}

	receipt, err := runtime.UpdateSettings(context.Background(), SettingsUpdateRequest{
		ExpectedRevision: strconv.FormatUint(current.Revision, 10),
		Online: SettingsOnlineUpdate{
			QuotaEnabled: true, ResetCreditsEnabled: true,
		},
		Refresh: SettingsRefreshUpdate{
			QuotaIntervalSeconds: 600, ResetCreditsIntervalSeconds: 3600,
			ReconcileIntervalSeconds: 7200, JSONLDebounceMilliseconds: 5000,
		},
		Updates: SettingsUpdatesUpdate{
			AutoCheckEnabled: false, CheckIntervalSeconds: 7200,
		},
		UI: SettingsUIUpdate{LaunchBehavior: "main_window", OverviewRange: "thirty_days"},
	})
	if err != nil || receipt.Result != SettingsUpdateApplied || receipt.Revision == "" {
		t.Fatalf("UpdateSettings() = %#v, %v", receipt, err)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(after update) error = %v", err)
	}
	if readback.Revision != current.Revision+1 || !readback.Online.QuotaEnabled ||
		!readback.Online.ResetCreditsEnabled || readback.Refresh.QuotaIntervalSeconds != 600 ||
		readback.Updates.AutoCheckEnabled || readback.Updates.CheckIntervalSeconds != 7200 ||
		readback.UI.LaunchBehavior != preferences.LaunchBehaviorMainWindow ||
		readback.UI.OverviewRange != preferences.OverviewRangeThirtyDays {
		t.Fatalf("editable settings readback = %#v", readback)
	}
	if readback.Updates.AutoDownloadEnabled || readback.Updates.Channel != preferences.UpdateChannelStable ||
		readback.Updates.SkippedVersion == nil || *readback.Updates.SkippedVersion != skippedVersion ||
		readback.Updates.SnoozeUntilMS == nil || *readback.Updates.SnoozeUntilMS != snoozeUntilMS ||
		readback.Updates.LastCheckAtMS == nil || *readback.Updates.LastCheckAtMS != lastCheckAtMS ||
		readback.UI.Locale != "zh-CN" {
		t.Fatalf("read-only settings changed = %#v", readback)
	}
	closeApplicationControlsTestRuntime(t, runtime, database)
}

func TestApplicationControlsReturnsCommittedReceiptWhenReconcileRequiresRecovery(t *testing.T) {
	t.Parallel()

	runtime, database, preferenceStore := startApplicationControlsTestRuntime(t)
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before update) error = %v", err)
	}
	reconcileFailure := errors.New("synthetic reconcile failure")
	runtime.quota.reconcilePreferences = func(context.Context) error { return reconcileFailure }
	receipt, err := runtime.UpdateSettings(context.Background(), settingsRequestFromSnapshot(current))
	if err != nil || receipt.Result != SettingsUpdateReconcileRequired || receipt.Revision == "" {
		t.Fatalf("UpdateSettings(reconcile failure) = %#v, %v", receipt, err)
	}
	readback, loadErr := preferenceStore.LoadPreferences(context.Background())
	if loadErr != nil || strconv.FormatUint(readback.Revision, 10) != receipt.Revision {
		t.Fatalf("committed readback = %#v, %v", readback, loadErr)
	}
	closeApplicationControlsTestRuntime(t, runtime, database)
}

func TestApplicationControlsKeepHomePlanPrivateAndConsumeItOnce(t *testing.T) {
	t.Parallel()

	homeB := writeSyntheticAuthHome(t, "synthetic-control-home-b-token")
	prepareApplicationControlsHome(t, homeB)
	runtime, database, preferenceStore := startApplicationControlsTestRuntime(t)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime.invalidation = invalidation
	plan, err := runtime.PlanHomeSwitch(context.Background(), HomeSwitchPlanRequest{
		TargetPath: homeB, Strategy: HomeSwitchClearAndRebuild,
	})
	if err != nil || plan.TargetGeneration != "2" || !plan.ClearsDerivedFacts {
		t.Fatalf("PlanHomeSwitch() = %#v, %v", plan, err)
	}
	encodedPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal(plan) error = %v", err)
	}
	for _, privateValue := range []string{homeB, "synthetic-control-home-b-token", "home-switch:"} {
		if strings.Contains(string(encodedPlan), privateValue) {
			t.Fatalf("public plan contains private value %q: %s", privateValue, encodedPlan)
		}
	}
	receipt, err := runtime.ConfirmHomeSwitch(context.Background())
	if err != nil || (receipt.Result != HomeSwitchCompletedResult && receipt.Result != HomeSwitchRecoveryRequired) ||
		receipt.Generation != "2" {
		t.Fatalf("ConfirmHomeSwitch() = %#v, %v", receipt, err)
	}
	if receipt.Result == HomeSwitchRecoveryRequired {
		recovered, recoverErr := runtime.RecoverHomeSwitch(context.Background())
		if recoverErr != nil || recovered.Result != HomeSwitchRecoveryRequired || recovered.Generation != "2" {
			t.Fatalf("RecoverHomeSwitch() = %#v, %v", recovered, recoverErr)
		}
	}
	wantInvalidations := 1
	if receipt.Result == HomeSwitchRecoveryRequired {
		wantInvalidations = 2
	}
	if invalidation.count(QueryInvalidationIndex) != wantInvalidations ||
		invalidation.count(QueryInvalidationSettings) != wantInvalidations {
		t.Fatalf(
			"Home invalidations = index:%d settings:%d, want %d each",
			invalidation.count(QueryInvalidationIndex),
			invalidation.count(QueryInvalidationSettings),
			wantInvalidations,
		)
	}
	encodedReceipt, err := json.Marshal(receipt)
	if err != nil || strings.Contains(string(encodedReceipt), homeB) || strings.Contains(string(encodedReceipt), "home-switch:") {
		t.Fatalf("public Home receipt leaked private data: %s, %v", encodedReceipt, err)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	homeBCanonical := quotaRuntimePreferencesForHome(t, homeB).CodexHome.Source.Path
	if err != nil || readback.CodexHome.Generation != 2 || readback.CodexHome.Source.Path != homeBCanonical {
		t.Fatalf("switched preferences = %#v, %v", readback, err)
	}
	if _, err := runtime.ConfirmHomeSwitch(context.Background()); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("ConfirmHomeSwitch(second) error = %v, want public unavailable", err)
	}
	closeApplicationControlsTestRuntime(t, runtime, database)
}

func TestApplicationControlsClearOnlyTheHomePlanItConsumed(t *testing.T) {
	t.Parallel()

	runtime := &applicationLifecycleRuntime{homePlanID: "plan-b"}
	runtime.clearHomePlanIfCurrent("plan-a")
	if runtime.homePlanID != "plan-b" {
		t.Fatalf("clear stale plan A removed latest plan = %q", runtime.homePlanID)
	}
	runtime.clearHomePlanIfCurrent("plan-b")
	if runtime.homePlanID != "" {
		t.Fatalf("clear current plan B left plan = %q", runtime.homePlanID)
	}
}

func TestApplicationControlsConfirmDoesNotClearAPlanCreatedWhileWrapperFinishes(t *testing.T) {
	t.Parallel()

	homeB := writeSyntheticAuthHome(t, "synthetic-control-home-b-concurrent-token")
	homeC := writeSyntheticAuthHome(t, "synthetic-control-home-c-concurrent-token")
	prepareApplicationControlsHome(t, homeB)
	prepareApplicationControlsHome(t, homeC)
	wrapperFinishEntered := make(chan struct{})
	releaseWrapperFinish := make(chan struct{})
	runtime, database, _ := startApplicationControlsTestRuntime(t)
	runtime.invalidation = &blockingHomeInvalidationNotifier{
		entered: wrapperFinishEntered,
		release: releaseWrapperFinish,
	}
	planA, err := runtime.PlanHomeSwitch(context.Background(), HomeSwitchPlanRequest{
		TargetPath: homeB, Strategy: HomeSwitchClearAndRebuild,
	})
	if err != nil {
		t.Fatalf("PlanHomeSwitch(A) error = %v", err)
	}
	confirmA := make(chan error, 1)
	go func() {
		_, confirmErr := runtime.ConfirmHomeSwitch(context.Background())
		confirmA <- confirmErr
	}()
	select {
	case <-wrapperFinishEntered:
	case <-time.After(2 * time.Second):
		close(releaseWrapperFinish)
		t.Fatal("ConfirmHomeSwitch(A) did not reach wrapper-finish barrier")
	}
	planB, err := runtime.PlanHomeSwitch(context.Background(), HomeSwitchPlanRequest{
		TargetPath: homeC, Strategy: HomeSwitchClearAndRebuild,
	})
	if err != nil || planB.TargetGeneration != "3" {
		close(releaseWrapperFinish)
		t.Fatalf("PlanHomeSwitch(B while A blocked) = %#v, %v", planB, err)
	}
	runtime.homePlanMu.Lock()
	planBID := runtime.homePlanID
	runtime.homePlanMu.Unlock()
	if planBID == "" {
		close(releaseWrapperFinish)
		t.Fatal("PlanHomeSwitch(B) did not publish a latest private plan")
	}
	close(releaseWrapperFinish)
	select {
	case err := <-confirmA:
		if err != nil {
			t.Fatalf("ConfirmHomeSwitch(A) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ConfirmHomeSwitch(A) did not finish")
	}
	if planA.TargetGeneration == planB.TargetGeneration {
		t.Fatalf("plans did not advance generations: A=%#v B=%#v", planA, planB)
	}
	runtime.homePlanMu.Lock()
	latestPlanID := runtime.homePlanID
	runtime.homePlanMu.Unlock()
	if latestPlanID != planBID {
		t.Fatalf("ConfirmHomeSwitch(A) changed latest plan B = %q, want %q", latestPlanID, planBID)
	}
	closeApplicationControlsTestRuntime(t, runtime, database)
}

func TestApplicationControlsExposeFiniteLifecycleAndReadOnlyRepair(t *testing.T) {
	t.Parallel()

	runtime, database, _ := startApplicationControlsTestRuntime(t)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime.invalidation = invalidation
	for _, action := range []RuntimeAction{
		RuntimeActionPauseBackfill, RuntimeActionPauseAll, RuntimeActionResume, RuntimeActionReconcile,
	} {
		receipt, err := runtime.RunRuntimeAction(context.Background(), action)
		if err != nil || receipt.Action != action || receipt.SourceState == "" || receipt.Transition == "" {
			t.Fatalf("RunRuntimeAction(%q) = %#v, %v", action, receipt, err)
		}
	}
	if invalidation.count(QueryInvalidationIndex) != 4 {
		t.Fatalf("index invalidations = %d, want 4", invalidation.count(QueryInvalidationIndex))
	}
	databaseDirectory := filepath.Dir(database.Config().Path)
	before, err := os.ReadFile(filepath.Join(runtimeConfirmedHome(t, runtime), "session_index.jsonl"))
	if err != nil {
		t.Fatalf("os.ReadFile(session index before) error = %v", err)
	}
	receipt, err := runtime.AnalyzeSessionIndexRepair(context.Background())
	if err != nil || receipt.AnalyzedAtMS <= 0 || !receipt.Noop || receipt.ActionCount != 0 ||
		receipt.ConflictCount != 0 {
		t.Fatalf("AnalyzeSessionIndexRepair() = %#v, %v", receipt, err)
	}
	after, err := os.ReadFile(filepath.Join(runtimeConfirmedHome(t, runtime), "session_index.jsonl"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("session index changed during dry-run: %q -> %q, %v", before, after, err)
	}
	if _, err := os.Stat(filepath.Join(databaseDirectory, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repair dry-run created backup directory: %v", err)
	}
	closeApplicationControlsTestRuntime(t, runtime, database)
}

func startApplicationControlsTestRuntime(
	t testing.TB,
) (*applicationLifecycleRuntime, *storesqlite.Store, *preferences.FileStore) {
	t.Helper()
	database, _ := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-control-home-token")
	prepareApplicationControlsHome(t, home)
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, false, false)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		_ = database.Close(context.Background())
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	return runtime, database, preferenceStore
}

type blockingHomeInvalidationNotifier struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (notifier *blockingHomeInvalidationNotifier) Notify(
	ctx context.Context,
	domain QueryInvalidationDomain,
) error {
	if domain != QueryInvalidationIndex {
		return nil
	}
	notifier.once.Do(func() { close(notifier.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-notifier.release:
		return nil
	}
}

func prepareApplicationControlsHome(t testing.TB, home string) {
	t.Helper()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), nil, 0o600); err != nil {
		t.Fatalf("os.WriteFile(session_index.jsonl) error = %v", err)
	}
}

func closeApplicationControlsTestRuntime(
	t testing.TB,
	runtime *applicationLifecycleRuntime,
	database *storesqlite.Store,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func settingsRequestFromSnapshot(snapshot preferences.Snapshot) SettingsUpdateRequest {
	return SettingsUpdateRequest{
		ExpectedRevision: strconv.FormatUint(snapshot.Revision, 10),
		Online: SettingsOnlineUpdate{
			QuotaEnabled:        !snapshot.Online.QuotaEnabled,
			ResetCreditsEnabled: snapshot.Online.ResetCreditsEnabled,
		},
		Refresh: SettingsRefreshUpdate{
			QuotaIntervalSeconds:        snapshot.Refresh.QuotaIntervalSeconds,
			ResetCreditsIntervalSeconds: snapshot.Refresh.ResetCreditsIntervalSeconds,
			ReconcileIntervalSeconds:    snapshot.Refresh.ReconcileIntervalSeconds,
			JSONLDebounceMilliseconds:   snapshot.Refresh.JSONLDebounceMilliseconds,
		},
		Updates: SettingsUpdatesUpdate{
			AutoCheckEnabled:     snapshot.Updates.AutoCheckEnabled,
			CheckIntervalSeconds: snapshot.Updates.CheckIntervalSeconds,
		},
		UI: SettingsUIUpdate{
			LaunchBehavior: string(snapshot.UI.LaunchBehavior),
			OverviewRange:  string(snapshot.UI.OverviewRange),
		},
	}
}

func runtimeConfirmedHome(t testing.TB, runtime *applicationLifecycleRuntime) string {
	t.Helper()
	snapshot, err := runtime.settingsLoader.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(runtime Home) error = %v", err)
	}
	return snapshot.CodexHome.Source.Path
}
