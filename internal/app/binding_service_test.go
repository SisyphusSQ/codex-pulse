package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBindingServiceExposesExactAllowlistAndContract(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})

	gotMethods := make([]string, 0, reflect.TypeOf(service).NumMethod())
	for index := 0; index < reflect.TypeOf(service).NumMethod(); index++ {
		gotMethods = append(gotMethods, reflect.TypeOf(service).Method(index).Name)
	}
	wantMethods := []string{
		"AnalyzeSessionIndexRepair", "Bootstrap", "ConfirmHomeSwitch", "Contracts", "Health",
		"Job", "ListHealth", "ListJobs", "ListProjects", "ListSessions", "ListSources",
		"PlanHomeSwitch", "ProjectDetail", "QuotaCurrent", "RecoverHomeSwitch",
		"RequestQuotaRefresh", "RunRuntimeAction", "SessionDetail", "Settings", "Source",
		"UpdateSettings", "UsageCost",
	}
	wantCommandMethods := []string{
		"RequestQuotaRefresh", "UpdateSettings", "PlanHomeSwitch", "ConfirmHomeSwitch",
		"RecoverHomeSwitch", "RunRuntimeAction", "AnalyzeSessionIndexRepair",
	}
	slices.Sort(wantMethods)
	if !slices.Equal(gotMethods, wantMethods) {
		t.Fatalf("binding methods = %v, want %v", gotMethods, wantMethods)
	}

	contract := service.Contracts()
	if contract.Version != BindingContractVersion || contract.QueryVersion != basequery.ContractVersion ||
		contract.UsageCostVersion != usagecost.ContractVersion ||
		contract.RuntimeInfoVersion != runtimeinfo.ContractVersion ||
		contract.ErrorExample.Version != basequery.ContractVersion ||
		contract.ErrorExample.Error.Code != basequery.ErrorInternal ||
		!slices.Equal(contract.CommandMethods, wantCommandMethods) ||
		len(contract.Methods) != len(wantMethods) {
		t.Fatalf("binding contract = %#v", contract)
	}
	contractNames := make([]string, len(contract.Methods))
	for index, method := range contract.Methods {
		wantKind := BindingMethodQuery
		if slices.Contains(wantCommandMethods, method.Name) {
			wantKind = BindingMethodCommand
		}
		if method.Kind != wantKind {
			t.Fatalf("binding method %q kind = %q, want %q", method.Name, method.Kind, wantKind)
		}
		contractNames[index] = method.Name
	}
	slices.Sort(contractNames)
	if !slices.Equal(contractNames, wantMethods) {
		t.Fatalf("contract methods = %v, want %v", contractNames, wantMethods)
	}

	content, err := json.Marshal(service)
	if err != nil || string(content) != "{}" {
		t.Fatalf("marshal Service = %s, %v; dependencies must remain private", content, err)
	}
}

func TestBindingServiceDelegatesEveryQuery(t *testing.T) {
	usage := &usageCostBindingStub{}
	runtime := &runtimeInfoBindingStub{}
	service := newBindingTestService(t, usage, runtime)
	ctx := context.Background()

	_, _ = service.UsageCost(ctx, usagecost.UsageCostRequest{})
	_, _ = service.ListSessions(ctx, basequery.Request{})
	_, _ = service.SessionDetail(ctx, usagecost.SessionDetailRequest{})
	_, _ = service.ListProjects(ctx, basequery.Request{})
	_, _ = service.ProjectDetail(ctx, usagecost.ProjectDetailRequest{})
	_, _ = service.QuotaCurrent(ctx, 123)
	_, _ = service.ListSources(ctx, basequery.Request{})
	_, _ = service.Source(ctx, runtimeinfo.SourceDetailRequest{})
	_, _ = service.ListJobs(ctx, basequery.Request{})
	_, _ = service.Job(ctx, runtimeinfo.JobDetailRequest{})
	_, _ = service.ListHealth(ctx, basequery.Request{})
	_, _ = service.Health(ctx, runtimeinfo.HealthDetailRequest{})
	_, _ = service.Settings(ctx)

	wantUsage := []string{"UsageCost", "ListSessions", "SessionDetail", "ListProjects", "ProjectDetail"}
	wantRuntime := []string{
		"QuotaCurrent", "ListSources", "Source", "ListJobs", "Job", "ListHealth", "Health", "Settings",
	}
	if !slices.Equal(usage.calls, wantUsage) || !slices.Equal(runtime.calls, wantRuntime) ||
		runtime.evaluatedAtMS != 123 {
		t.Fatalf("delegated calls usage=%v runtime=%v evaluatedAt=%d", usage.calls, runtime.calls, runtime.evaluatedAtMS)
	}
}

func TestBindingServiceDelegatesRedactedQuotaRefreshCommand(t *testing.T) {
	nextDueAtMS := int64(1_784_100_060_000)
	lastManualAtMS := int64(1_784_100_000_000)
	claimID := "synthetic-claim-must-not-cross-binding"
	command := &quotaRefreshBindingStub{schedule: factstore.SourceRefreshSchedule{
		SourceInstanceID: "synthetic-source-instance", SourceType: "wham",
		ScopeKey: "synthetic-scope", NextDueAtMS: &nextDueAtMS,
		Reason: factstore.RefreshReasonNormalInterval, LastManualAtMS: &lastManualAtMS,
		ActiveClaimID: &claimID, Revision: 17,
	}}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{},
		QuotaRefresh: command,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	receipt, err := service.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota)
	if err != nil || command.source != quotaonline.RefreshSourceQuota ||
		receipt.Source != quotaonline.RefreshSourceQuota ||
		receipt.NextDueAtMS == nil || *receipt.NextDueAtMS != nextDueAtMS ||
		receipt.LastManualAtMS == nil || *receipt.LastManualAtMS != lastManualAtMS ||
		receipt.Reason != factstore.RefreshReasonNormalInterval {
		t.Fatalf("RequestQuotaRefresh() = %#v, source=%q, error=%v", receipt, command.source, err)
	}
	content, err := json.Marshal(receipt)
	if err != nil || strings.Contains(string(content), "synthetic-") ||
		strings.Contains(string(content), "claim") || strings.Contains(string(content), "revision") {
		t.Fatalf("receipt leaked internal schedule fields: %s, %v", content, err)
	}
}

func TestBindingServiceRejectsInvalidQuotaRefreshSource(t *testing.T) {
	service, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{},
		QuotaRefresh: &quotaRefreshBindingStub{},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	_, err = service.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSource("invalid"))
	assertBindingFailure(t, err, basequery.ErrorValidation, "source", "invalid")
}

func TestBindingServiceBindsQuotaRefreshExactlyOnceBeforeUse(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	_, err := service.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota)
	assertBindingFailure(t, err, basequery.ErrorInternal, "", "synthetic-marker-not-present")

	command := &quotaRefreshBindingStub{}
	if err := service.bindQuotaRefresh(command); err != nil {
		t.Fatalf("bindQuotaRefresh() error = %v", err)
	}
	if err := service.bindQuotaRefresh(&quotaRefreshBindingStub{}); !errors.Is(err, ErrBindingService) {
		t.Fatalf("bindQuotaRefresh(duplicate) error = %v, want ErrBindingService", err)
	}
	if err := service.bindQuotaRefresh(nil); !errors.Is(err, ErrBindingService) {
		t.Fatalf("bindQuotaRefresh(nil) error = %v, want ErrBindingService", err)
	}
	if _, err := service.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota); err != nil ||
		command.source != quotaonline.RefreshSourceQuota {
		t.Fatalf("RequestQuotaRefresh() source=%q, error=%v", command.source, err)
	}
}

func TestBindingServiceDelegatesFiniteRuntimeControlsWithRedactedReceipts(t *testing.T) {
	command := &runtimeControlBindingStub{
		settingsReceipt: SettingsUpdateReceipt{Revision: "8", Result: SettingsUpdateApplied},
		planReceipt: HomeSwitchPlanReceipt{
			Strategy: HomeSwitchIndependentDatabase, TargetGeneration: "3", PreservesOldFacts: true,
		},
		homeReceipt: HomeSwitchReceipt{Revision: "9", Generation: "3", Result: HomeSwitchCompletedResult},
		actionReceipt: RuntimeActionReceipt{
			Action: RuntimeActionReconcile, PauseScope: "none", SourceState: "available", Transition: "steady",
		},
		repairReceipt: RepairDryRunReceipt{AnalyzedAtMS: 1, ActionCount: 2, ConflictCount: 1},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{}, RuntimeControls: command,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	settingsRequest := validBindingSettingsUpdateRequest()
	planRequest := HomeSwitchPlanRequest{
		TargetPath: filepath.Join(string(filepath.Separator), "synthetic", "next-home"),
		Strategy:   HomeSwitchIndependentDatabase,
	}
	settings, err := service.UpdateSettings(context.Background(), settingsRequest)
	if err != nil || settings != command.settingsReceipt || command.settingsRequest != settingsRequest {
		t.Fatalf("UpdateSettings() = %#v, request=%#v, error=%v", settings, command.settingsRequest, err)
	}
	plan, err := service.PlanHomeSwitch(context.Background(), planRequest)
	if err != nil || plan != command.planReceipt || command.planRequest != planRequest {
		t.Fatalf("PlanHomeSwitch() = %#v, request=%#v, error=%v", plan, command.planRequest, err)
	}
	confirmed, confirmErr := service.ConfirmHomeSwitch(context.Background())
	recovered, recoverErr := service.RecoverHomeSwitch(context.Background())
	action, actionErr := service.RunRuntimeAction(context.Background(), RuntimeActionReconcile)
	repair, repairErr := service.AnalyzeSessionIndexRepair(context.Background())
	if confirmErr != nil || recoverErr != nil || actionErr != nil || repairErr != nil ||
		confirmed != command.homeReceipt || recovered != command.homeReceipt ||
		action != command.actionReceipt || repair != command.repairReceipt ||
		command.action != RuntimeActionReconcile || command.confirmCalls != 1 ||
		command.recoverCalls != 1 || command.repairCalls != 1 {
		t.Fatalf("runtime receipts confirm=%#v recover=%#v action=%#v repair=%#v errors=%v/%v/%v/%v stub=%#v",
			confirmed, recovered, action, repair, confirmErr, recoverErr, actionErr, repairErr, command)
	}
	content, err := json.Marshal([]any{settings, plan, confirmed, recovered, action, repair})
	if err != nil || strings.Contains(string(content), "next-home") ||
		strings.Contains(string(content), "plan") || strings.Contains(string(content), "path") ||
		strings.Contains(string(content), "session") {
		t.Fatalf("runtime control receipts leaked private fields: %s, %v", content, err)
	}
}

func TestBindingServiceRejectsInvalidRuntimeControlInputsBeforeDelegation(t *testing.T) {
	command := &runtimeControlBindingStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{}, RuntimeControls: command,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	invalidSettings := validBindingSettingsUpdateRequest()
	invalidSettings.ExpectedRevision = "private-revision"
	_, settingsErr := service.UpdateSettings(context.Background(), invalidSettings)
	assertBindingFailure(t, settingsErr, basequery.ErrorValidation, "settings", "private-revision")
	_, pathErr := service.PlanHomeSwitch(context.Background(), HomeSwitchPlanRequest{
		TargetPath: "relative/private-home", Strategy: HomeSwitchIndependentDatabase,
	})
	assertBindingFailure(t, pathErr, basequery.ErrorValidation, "targetPath", "private-home")
	_, strategyErr := service.PlanHomeSwitch(context.Background(), HomeSwitchPlanRequest{
		TargetPath: filepath.Join(string(filepath.Separator), "synthetic", "home"),
		Strategy:   HomeSwitchStrategy("private-strategy"),
	})
	assertBindingFailure(t, strategyErr, basequery.ErrorValidation, "strategy", "private-strategy")
	_, actionErr := service.RunRuntimeAction(context.Background(), RuntimeAction("private-action"))
	assertBindingFailure(t, actionErr, basequery.ErrorValidation, "action", "private-action")
	if command.settingsRequest.ExpectedRevision != "" || command.planRequest.TargetPath != "" || command.action != "" {
		t.Fatalf("invalid commands reached dependency: %#v", command)
	}
}

func TestBindingServiceBindsRuntimeControlsExactlyOnce(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	if _, err := service.AnalyzeSessionIndexRepair(context.Background()); err == nil {
		t.Fatal("AnalyzeSessionIndexRepair() before bind unexpectedly succeeded")
	}
	command := &runtimeControlBindingStub{}
	if err := service.bindRuntimeControls(command); err != nil {
		t.Fatalf("bindRuntimeControls() error = %v", err)
	}
	if err := service.bindRuntimeControls(&runtimeControlBindingStub{}); !errors.Is(err, ErrBindingService) {
		t.Fatalf("bindRuntimeControls(duplicate) error = %v, want ErrBindingService", err)
	}
	if err := service.bindRuntimeControls(nil); !errors.Is(err, ErrBindingService) {
		t.Fatalf("bindRuntimeControls(nil) error = %v, want ErrBindingService", err)
	}
	if _, err := service.AnalyzeSessionIndexRepair(context.Background()); err != nil || command.repairCalls != 1 {
		t.Fatalf("AnalyzeSessionIndexRepair() repairCalls=%d, error=%v", command.repairCalls, err)
	}
}

func TestBindingServiceErrorsAreTypedContentFreeAndCancellable(t *testing.T) {
	const secretMarker = "synthetic-secret-binding-marker"

	t.Run("validation", func(t *testing.T) {
		usage := &usageCostBindingStub{
			err: basequery.NewValidationFailure("page.limit", errors.New(secretMarker)),
		}
		service := newBindingTestService(t, usage, &runtimeInfoBindingStub{})
		_, err := service.ListSessions(context.Background(), basequery.Request{})
		assertBindingFailure(t, err, basequery.ErrorValidation, "page.limit", secretMarker)
	})

	t.Run("internal", func(t *testing.T) {
		usage := &usageCostBindingStub{err: errors.New(secretMarker)}
		service := newBindingTestService(t, usage, &runtimeInfoBindingStub{})
		_, err := service.ListSessions(context.Background(), basequery.Request{})
		assertBindingFailure(t, err, basequery.ErrorInternal, "", secretMarker)
	})

	t.Run("command internal", func(t *testing.T) {
		service, err := NewService(ServiceConfig{
			UsageCost: &usageCostBindingStub{}, RuntimeInfo: &runtimeInfoBindingStub{},
			QuotaRefresh: &quotaRefreshBindingStub{err: errors.New(secretMarker)},
		})
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}
		_, err = service.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota)
		assertBindingFailure(t, err, basequery.ErrorInternal, "", secretMarker)
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		usage := &usageCostBindingStub{useContextError: true}
		service := newBindingTestService(t, usage, &runtimeInfoBindingStub{})
		_, err := service.ListSessions(ctx, basequery.Request{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ListSessions(cancelled) error = %v, want context.Canceled", err)
		}
		assertBindingFailure(t, err, basequery.ErrorCancelled, "", secretMarker)
	})

	t.Run("panic", func(t *testing.T) {
		usage := &usageCostBindingStub{panicValue: secretMarker}
		service := newBindingTestService(t, usage, &runtimeInfoBindingStub{})
		var recovered any
		var err error
		func() {
			defer func() { recovered = recover() }()
			_, err = service.ListSessions(context.Background(), basequery.Request{})
		}()
		if recovered != nil {
			t.Fatalf("ListSessions() leaked dependency panic: %v", recovered)
		}
		assertBindingFailure(t, err, basequery.ErrorInternal, "", secretMarker)
	})
}

func TestWailsBindingRuntimeBoundary(t *testing.T) {
	if os.Getenv("CODEX_PULSE_WAILS_BOUNDARY_CHILD") == "1" {
		runWailsBindingRuntimeBoundary(t)
		return
	}
	command := exec.Command(
		os.Args[0], "-test.run=^TestWailsBindingRuntimeBoundary$", "-test.count=1",
	)
	command.Env = append(os.Environ(), "CODEX_PULSE_WAILS_BOUNDARY_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Wails boundary subprocess: %v\n%s", err, output)
	}
}

func runWailsBindingRuntimeBoundary(t *testing.T) {
	const methodID = uint32(270001)
	application.RegisterBindingMethodID((*Service).ListSessions, methodID)
	t.Cleanup(func() { application.UnregisterBindingMethodID((*Service).ListSessions) })

	usage := &usageCostBindingStub{useContextError: true}
	service := newBindingTestService(t, usage, &runtimeInfoBindingStub{})
	application.New(application.Options{Name: "Codex Pulse binding boundary test"})
	bindings := application.NewBindings(nil, nil)
	if err := bindings.Add(wailsBindingService(service)); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}
	method := bindings.GetByID(methodID)
	if method == nil {
		t.Fatal("ListSessions binding is missing")
	}
	request, err := json.Marshal(basequery.Request{})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = method.Call(ctx, []json.RawMessage{request})
	assertWailsRuntimeFailure(t, err, basequery.ErrorCancelled, "")

	const secretMarker = "synthetic-secret-bound-panic"
	usage.useContextError = false
	usage.panicValue = secretMarker
	_, err = method.Call(context.Background(), []json.RawMessage{request})
	assertWailsRuntimeFailure(t, err, basequery.ErrorInternal, secretMarker)

	_, err = method.Call(context.Background(), []json.RawMessage{json.RawMessage(`{"page":`)})
	var typeError *application.CallError
	if !errors.As(err, &typeError) || typeError.Kind != application.TypeError {
		t.Fatalf("invalid JSON error = %#v, want Wails TypeError", err)
	}
}

func assertWailsRuntimeFailure(
	t *testing.T,
	err error,
	wantCode basequery.ErrorCode,
	secretMarker string,
) {
	t.Helper()
	var callError *application.CallError
	if !errors.As(err, &callError) || callError.Kind != application.RuntimeError ||
		callError.Message != ErrBindingQuery.Error() ||
		(secretMarker != "" && strings.Contains(callError.Message, secretMarker)) {
		t.Fatalf("bound runtime error = %#v, want fixed content-free RuntimeError", err)
	}
	cause, ok := callError.Cause.(json.RawMessage)
	if !ok || (secretMarker != "" && strings.Contains(string(cause), secretMarker)) {
		t.Fatalf("bound runtime cause = %#v, want content-free json.RawMessage", callError.Cause)
	}
	var envelope basequery.ErrorEnvelope
	if unmarshalErr := json.Unmarshal(cause, &envelope); unmarshalErr != nil ||
		envelope.Error.Code != wantCode {
		t.Fatalf("bound runtime envelope = %#v, %v; want %q", envelope, unmarshalErr, wantCode)
	}
}

func TestBindingServiceComposesRealStoreQueries(t *testing.T) {
	ctx := context.Background()
	databaseDirectory := t.TempDir()
	if err := os.Chmod(databaseDirectory, 0o700); err != nil {
		t.Fatalf("secure database directory: %v", err)
	}
	owned, err := openConfiguredStore(
		ctx,
		storesqlite.Config{Path: filepath.Join(databaseDirectory, "binding.db")},
	)
	if err != nil {
		t.Fatalf("openConfiguredStore() error = %v", err)
	}
	database := owned.(*storesqlite.Store)
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	preferenceStore, err := preferences.NewFileStore(
		filepath.Join(t.TempDir(), "private", "preferences.json"),
	)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	service, err := composeBindingService(database, preferenceStore)
	if err != nil {
		t.Fatalf("composeBindingService() error = %v", err)
	}
	if _, err := factstore.NewRepository(database).RebuildCostLedger(
		ctx,
		factstore.RebuildCostLedgerRequest{
			GenerationID: "binding-empty", ReportingTimezone: "UTC",
			PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
			CalculatedAtMS: 1,
		},
	); err != nil {
		t.Fatalf("RebuildCostLedger(empty) error = %v", err)
	}
	usage, err := service.UsageCost(ctx, usagecost.UsageCostRequest{
		Range: basequery.LocalDateRange{
			StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "UTC",
		},
		Granularity: usagecost.TrendDay,
	})
	if err != nil || usage.Meta.Status != basequery.ResponseComplete || usage.Trend == nil ||
		len(usage.Trend) != 0 {
		t.Fatalf("UsageCost(empty real Store) = %#v, %v", usage, err)
	}
	quota, err := service.QuotaCurrent(ctx, 270)
	if err != nil || quota.Meta.Status != basequery.ResponseComplete ||
		quota.Current.EvaluatedAtMS != 270 || quota.Current.Windows == nil ||
		quota.Current.Sources == nil {
		t.Fatalf("QuotaCurrent(empty real Store) = %#v, %v", quota, err)
	}
	if err := preferenceStore.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: filepath.Join(t.TempDir(), "codex-home"), DeviceID: "synthetic-device",
			Inode: 270, ConfirmedAtMS: 270,
		},
		OnlineQuotaEnabled: false, ResetCreditsEnabled: true,
	}); err != nil {
		t.Fatalf("Confirm(shared Preferences) error = %v", err)
	}
	settings, err := service.Settings(ctx)
	if err != nil || settings.Meta.Status != basequery.ResponseComplete ||
		settings.Snapshot.Online.QuotaEnabled ||
		!settings.Snapshot.Online.ResetCreditsEnabled {
		t.Fatalf("Settings(shared Preferences) = %#v, %v", settings, err)
	}
	response, err := service.ListSources(ctx, basequery.Request{})
	if err != nil || response.Meta.Status != basequery.ResponseComplete || response.Items == nil ||
		response.MatchedCount.Value == nil || *response.MatchedCount.Value != 0 {
		t.Fatalf("ListSources(empty real Store) = %#v, %v", response, err)
	}
}

func TestNewBindingServiceRejectsMissingDependencies(t *testing.T) {
	for _, config := range []ServiceConfig{
		{},
		{UsageCost: &usageCostBindingStub{}},
		{RuntimeInfo: &runtimeInfoBindingStub{}},
	} {
		if _, err := NewService(config); !errors.Is(err, ErrBindingService) {
			t.Fatalf("NewService(%#v) error = %v, want ErrBindingService", config, err)
		}
	}
}

func newBindingTestService(
	t *testing.T,
	usage usageCostBindingQuery,
	runtime runtimeInfoBindingQuery,
) *Service {
	t.Helper()
	service, err := NewService(ServiceConfig{UsageCost: usage, RuntimeInfo: runtime})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func assertBindingFailure(
	t *testing.T,
	err error,
	wantCode basequery.ErrorCode,
	wantField string,
	secretMarker string,
) {
	t.Helper()
	if err == nil || err.Error() != ErrBindingQuery.Error() {
		t.Fatalf("binding error = %v, want fixed %q", err, ErrBindingQuery)
	}
	content := marshalBindingError(err)
	if len(content) == 0 || strings.Contains(string(content), secretMarker) ||
		strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("binding error leaked marker: message=%q payload=%s", err, content)
	}
	var envelope basequery.ErrorEnvelope
	if unmarshalErr := json.Unmarshal(content, &envelope); unmarshalErr != nil {
		t.Fatalf("unmarshal binding envelope: %v", unmarshalErr)
	}
	if envelope.Version != basequery.ContractVersion || envelope.Error.Code != wantCode {
		t.Fatalf("binding envelope = %#v, want code %q", envelope, wantCode)
	}
	if wantField == "" {
		if envelope.Error.Field != nil {
			t.Fatalf("binding field = %q, want nil", *envelope.Error.Field)
		}
	} else if envelope.Error.Field == nil || *envelope.Error.Field != wantField {
		t.Fatalf("binding field = %v, want %q", envelope.Error.Field, wantField)
	}
}

type usageCostBindingStub struct {
	calls           []string
	err             error
	panicValue      any
	useContextError bool
}

func (stub *usageCostBindingStub) call(ctx context.Context, name string) error {
	stub.calls = append(stub.calls, name)
	if stub.panicValue != nil {
		panic(stub.panicValue)
	}
	if stub.useContextError {
		return ctx.Err()
	}
	return stub.err
}

func (stub *usageCostBindingStub) UsageCost(
	ctx context.Context,
	_ usagecost.UsageCostRequest,
) (usagecost.UsageCostResponse, error) {
	return usagecost.UsageCostResponse{}, stub.call(ctx, "UsageCost")
}

func (stub *usageCostBindingStub) ListSessions(
	ctx context.Context,
	_ basequery.Request,
) (usagecost.SessionListResponse, error) {
	return usagecost.SessionListResponse{}, stub.call(ctx, "ListSessions")
}

func (stub *usageCostBindingStub) SessionDetail(
	ctx context.Context,
	_ usagecost.SessionDetailRequest,
) (usagecost.SessionDetailResponse, error) {
	return usagecost.SessionDetailResponse{}, stub.call(ctx, "SessionDetail")
}

func (stub *usageCostBindingStub) ListProjects(
	ctx context.Context,
	_ basequery.Request,
) (usagecost.ProjectListResponse, error) {
	return usagecost.ProjectListResponse{}, stub.call(ctx, "ListProjects")
}

func (stub *usageCostBindingStub) ProjectDetail(
	ctx context.Context,
	_ usagecost.ProjectDetailRequest,
) (usagecost.ProjectDetailResponse, error) {
	return usagecost.ProjectDetailResponse{}, stub.call(ctx, "ProjectDetail")
}

type runtimeInfoBindingStub struct {
	calls           []string
	err             error
	evaluatedAtMS   int64
	useContextError bool
}

type quotaRefreshBindingStub struct {
	schedule factstore.SourceRefreshSchedule
	source   quotaonline.RefreshSource
	err      error
}

type runtimeControlBindingStub struct {
	settingsRequest SettingsUpdateRequest
	planRequest     HomeSwitchPlanRequest
	action          RuntimeAction
	confirmCalls    int
	recoverCalls    int
	repairCalls     int
	settingsReceipt SettingsUpdateReceipt
	planReceipt     HomeSwitchPlanReceipt
	homeReceipt     HomeSwitchReceipt
	actionReceipt   RuntimeActionReceipt
	repairReceipt   RepairDryRunReceipt
	err             error
}

func (stub *runtimeControlBindingStub) UpdateSettings(
	_ context.Context,
	request SettingsUpdateRequest,
) (SettingsUpdateReceipt, error) {
	stub.settingsRequest = request
	return stub.settingsReceipt, stub.err
}

func (stub *runtimeControlBindingStub) PlanHomeSwitch(
	_ context.Context,
	request HomeSwitchPlanRequest,
) (HomeSwitchPlanReceipt, error) {
	stub.planRequest = request
	return stub.planReceipt, stub.err
}

func (stub *runtimeControlBindingStub) ConfirmHomeSwitch(context.Context) (HomeSwitchReceipt, error) {
	stub.confirmCalls++
	return stub.homeReceipt, stub.err
}

func (stub *runtimeControlBindingStub) RecoverHomeSwitch(context.Context) (HomeSwitchReceipt, error) {
	stub.recoverCalls++
	return stub.homeReceipt, stub.err
}

func (stub *runtimeControlBindingStub) RunRuntimeAction(
	_ context.Context,
	action RuntimeAction,
) (RuntimeActionReceipt, error) {
	stub.action = action
	return stub.actionReceipt, stub.err
}

func (stub *runtimeControlBindingStub) AnalyzeSessionIndexRepair(context.Context) (RepairDryRunReceipt, error) {
	stub.repairCalls++
	return stub.repairReceipt, stub.err
}

func validBindingSettingsUpdateRequest() SettingsUpdateRequest {
	return SettingsUpdateRequest{
		ExpectedRevision: "7",
		Online:           SettingsOnlineUpdate{QuotaEnabled: true, ResetCreditsEnabled: false},
		Refresh: SettingsRefreshUpdate{
			QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1800,
			ReconcileIntervalSeconds: 1800, JSONLDebounceMilliseconds: 4000,
		},
		Updates: SettingsUpdatesUpdate{AutoCheckEnabled: true, CheckIntervalSeconds: 3600},
		UI:      SettingsUIUpdate{LaunchBehavior: "tray", OverviewRange: "seven_days"},
	}
}

func (stub *quotaRefreshBindingStub) RequestQuotaRefresh(
	_ context.Context,
	source quotaonline.RefreshSource,
) (factstore.SourceRefreshSchedule, error) {
	stub.source = source
	return stub.schedule, stub.err
}

func (stub *runtimeInfoBindingStub) call(ctx context.Context, name string) error {
	stub.calls = append(stub.calls, name)
	if stub.useContextError {
		return ctx.Err()
	}
	return stub.err
}

func (stub *runtimeInfoBindingStub) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	stub.evaluatedAtMS = evaluatedAtMS
	return runtimeinfo.QuotaCurrentResponse{}, stub.call(ctx, "QuotaCurrent")
}

func (stub *runtimeInfoBindingStub) ListSources(
	ctx context.Context,
	_ basequery.Request,
) (runtimeinfo.SourceListResponse, error) {
	return runtimeinfo.SourceListResponse{}, stub.call(ctx, "ListSources")
}

func (stub *runtimeInfoBindingStub) Source(
	ctx context.Context,
	_ runtimeinfo.SourceDetailRequest,
) (runtimeinfo.SourceDetailResponse, error) {
	return runtimeinfo.SourceDetailResponse{}, stub.call(ctx, "Source")
}

func (stub *runtimeInfoBindingStub) ListJobs(
	ctx context.Context,
	_ basequery.Request,
) (runtimeinfo.JobListResponse, error) {
	return runtimeinfo.JobListResponse{}, stub.call(ctx, "ListJobs")
}

func (stub *runtimeInfoBindingStub) Job(
	ctx context.Context,
	_ runtimeinfo.JobDetailRequest,
) (runtimeinfo.JobDetailResponse, error) {
	return runtimeinfo.JobDetailResponse{}, stub.call(ctx, "Job")
}

func (stub *runtimeInfoBindingStub) ListHealth(
	ctx context.Context,
	_ basequery.Request,
) (runtimeinfo.HealthListResponse, error) {
	return runtimeinfo.HealthListResponse{}, stub.call(ctx, "ListHealth")
}

func (stub *runtimeInfoBindingStub) Health(
	ctx context.Context,
	_ runtimeinfo.HealthDetailRequest,
) (runtimeinfo.HealthDetailResponse, error) {
	return runtimeinfo.HealthDetailResponse{}, stub.call(ctx, "Health")
}

func (stub *runtimeInfoBindingStub) Settings(
	ctx context.Context,
) (runtimeinfo.SettingsResponse, error) {
	return runtimeinfo.SettingsResponse{}, stub.call(ctx, "Settings")
}
