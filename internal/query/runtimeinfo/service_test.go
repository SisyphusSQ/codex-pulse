package runtimeinfo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQuotaCurrentAndSettingsReturnVersionedRedactedFacts(t *testing.T) {
	t.Parallel()

	quotaReader := &quotaStub{query: func(_ context.Context, atMS int64) (quotaquery.CurrentResponse, error) {
		return quotaquery.CurrentResponse{
			Version: quotaquery.CurrentContractVersion, AccountScope: "default",
			EvaluatedAtMS: atMS, Windows: []quotaquery.CurrentWindow{}, Sources: []quotaquery.CurrentSource{},
		}, nil
	}}
	preferencesReader := &preferencesStub{load: func(context.Context) (preferences.Snapshot, error) {
		return validSensitivePreferences(), nil
	}}
	service := newTestService(t, quotaReader, &runtimeStub{}, preferencesReader)

	quotaResponse, err := service.QuotaCurrent(context.Background(), 500)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	if quotaResponse.Meta.Version != basequery.ContractVersion ||
		quotaResponse.Meta.Status != basequery.ResponseComplete ||
		quotaResponse.Current.Version != quotaquery.CurrentContractVersion ||
		quotaResponse.Current.EvaluatedAtMS != 500 {
		t.Fatalf("QuotaCurrent() = %#v", quotaResponse)
	}

	settings, err := service.Settings(context.Background())
	if err != nil {
		t.Fatalf("Settings() error = %v", err)
	}
	if settings.Meta.Status != basequery.ResponseComplete || settings.Snapshot.Revision != "7" ||
		settings.Snapshot.Home.Generation != "3" || !settings.Snapshot.Home.Configured ||
		settings.Snapshot.Home.SwitchStatus != HomeSwitchPending ||
		len(settings.EditableFields) < 10 {
		t.Fatalf("Settings() = %#v", settings)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("json.Marshal(settings) error = %v", err)
	}
	for _, forbidden := range []string{
		"/Users/private/.codex", "private-device", "private-store-key", "private-switch-id",
		"0123456789abcdef0123456789abcdef", "detached-private", "path", "deviceId", "inode", "dataStoreKey",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Settings() leaked %q: %s", forbidden, encoded)
		}
	}
	assertEditableField(t, settings.EditableFields, "refresh.quotaIntervalSeconds", true, int64Pointer(60), int64Pointer(1800))
	assertEditableField(t, settings.EditableFields, "updates.channel", false, nil, nil)
	assertEditableOptions(
		t, settings.EditableFields, "ui.overviewRange",
		[]string{"quota_week", "today", "seven_days", "thirty_days"},
	)
}

func TestListSourcesMapsBothKindsRedactsAndRoundTripsCursor(t *testing.T) {
	t.Parallel()

	lastScan, lastAttempt, lastSuccess, nextDue := int64(90), int64(80), int64(70), int64(120)
	errorClass := store.RuntimeErrorPermission
	failureCode := store.SourceFailureAuthRequired
	pages := []store.RuntimeSourcePage{
		{
			Records: []store.RuntimeSourceRecord{
				{
					SourceKey: "local_file:file-a", Kind: store.RuntimeSourceLocalFile, UpdatedAtMS: 100,
					Local: &store.SourceFile{
						SourceFileID: "file-a", Provider: "codex", CurrentPath: "/private/a.jsonl",
						DeviceID: "private-device", Inode: 42, SizeBytes: 100, ParsedOffset: 50,
						State: store.SourceFileActive, LastScannedAtMS: &lastScan,
						UpdatedAtMS: 100,
					},
				},
				{
					SourceKey: "online:quota-a", Kind: store.RuntimeSourceOnline, UpdatedAtMS: 95,
					Online: &store.SourceState{
						SourceInstanceID: "quota-a", SourceType: "wham_quota", ScopeKey: "private-scope",
						LastAttemptAtMS: &lastAttempt, LastSuccessAtMS: &lastSuccess, NextDueAtMS: &nextDue,
						ConsecutiveFailures: 2, LastErrorClass: &errorClass,
						LastFailureCode: &failureCode,
						FreshnessState:  store.SourceFreshnessUnavailable, UpdatedAtMS: 95,
					},
				},
			},
			MatchedCount: 3,
			Summary:      store.RuntimeSourceSummary{Total: 3, LocalFiles: 1, OnlineSources: 2, Attention: 1},
			NextCursor:   &store.RuntimeSourceCursor{UpdatedAtMS: 95, SourceKey: "online:quota-a"},
		},
		{
			Records: []store.RuntimeSourceRecord{}, MatchedCount: 3,
			Summary: store.RuntimeSourceSummary{Total: 3, LocalFiles: 1, OnlineSources: 2, Attention: 1},
		},
	}
	var received []store.RuntimeSourceQuery
	runtimeReader := &runtimeStub{sourcePage: func(
		_ context.Context, filter store.RuntimeSourceQuery,
	) (store.RuntimeSourcePage, error) {
		received = append(received, filter)
		return pages[len(received)-1], nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	request := basequery.Request{
		Page: basequery.PageRequest{Limit: 2},
		Filters: []basequery.FilterTerm{
			{Field: "kind", Operator: basequery.FilterIn, Values: []string{"local_file", "online"}},
			{Field: "state", Operator: basequery.FilterEqual, Values: []string{"unavailable"}},
		},
	}
	first, err := service.ListSources(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSources(first) error = %v", err)
	}
	if first.Meta.Status != basequery.ResponsePartial || first.Meta.Page == nil ||
		first.Meta.Page.NextCursor == nil || len(first.Items) != 2 ||
		first.Summary.Attention.Value == nil || *first.Summary.Attention.Value != 1 ||
		first.Items[0].RecoveryAction.Kind != RecoveryNone ||
		first.Items[1].RecoveryAction.Kind != RecoveryGrantPermission ||
		first.Items[0].SizeBytes.Unit != basequery.NumericBytes ||
		first.Items[1].FailureCode == nil || *first.Items[1].FailureCode != "auth_required" {
		t.Fatalf("ListSources(first) = %#v", first)
	}
	encoded, _ := json.Marshal(first)
	for _, forbidden := range []string{"/private/a.jsonl", "private-device", "private-scope", "inode", "currentPath", "scopeKey"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("ListSources() leaked %q: %s", forbidden, encoded)
		}
	}
	request.Page.Cursor = first.Meta.Page.NextCursor
	second, err := service.ListSources(context.Background(), request)
	if err != nil || len(second.Items) != 0 {
		t.Fatalf("ListSources(second) = %#v, %v", second, err)
	}
	if len(received) != 2 || received[0].Direction != store.RuntimeQueryDescending ||
		received[1].After == nil || received[1].After.SourceKey != "online:quota-a" {
		t.Fatalf("source filters = %#v", received)
	}
}

func TestJobAndHealthQueriesMapProgressLevelsAndRegisteredActions(t *testing.T) {
	t.Parallel()

	progress, total, started, finished := int64(4), int64(10), int64(20), int64(40)
	taskError := store.RuntimeErrorTimeout
	active := true
	runtimeReader := &runtimeStub{
		jobPage: func(context.Context, store.RuntimeJobQuery) (store.RuntimeJobPage, error) {
			return store.RuntimeJobPage{
				Records: []store.RuntimeJobRecord{{
					Job: store.JobRun{
						JobID: "job-a", JobType: "backfill", RequestedBy: "startup", State: store.JobFailed,
						Phase: store.JobPhaseHistoryBackfill, CreatedAtMS: 10, StartedAtMS: &started,
						FinishedAtMS: &finished, ProgressCurrent: &progress, ProgressTotal: &total,
						ErrorClass: &taskError, UpdatedAtMS: 40,
					},
					Task: &store.SchedulerTask{TaskID: "private-task-id"},
					Retry: &store.SchedulerRetryState{
						TaskID: "private-task-id", Disposition: store.SchedulerRetryBlocked,
						FailureCount: 2, LastErrorClass: taskError,
						RecoveryAction: store.SchedulerRecoveryRetry, Revision: 1, UpdatedAtMS: 41,
					},
				}},
				MatchedCount: 1, Summary: store.RuntimeJobSummary{Total: 1, Failed: 1},
			}, nil
		},
		healthPage: func(context.Context, store.RuntimeHealthQuery) (store.RuntimeHealthPage, error) {
			return store.RuntimeHealthPage{
				Records: []store.HealthEvent{{
					EventID: "health-a", Fingerprint: store.SHA256DigestOf([]byte("private-fingerprint")),
					Domain: store.HealthDomainStore, Severity: store.HealthCritical,
					Code: store.HealthCodeStoreDiskFull, ErrorClass: pointerTo(store.RuntimeErrorDiskFull),
					FirstSeenAtMS: 10, LastSeenAtMS: 20, OccurrenceCount: 3, UpdatedAtMS: 20,
				}},
				MatchedCount: 1,
				Summary: store.RuntimeHealthSummary{
					Total: 1, Active: 1, Critical: 1, ActiveCritical: 1,
				},
			}, nil
		},
	}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	jobs, err := service.ListJobs(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs.Items) != 1 || jobs.Items[0].Progress.Current.Value == nil ||
		*jobs.Items[0].Progress.Current.Value != 4 ||
		jobs.Items[0].RecoveryAction.Kind != RecoveryRetry ||
		jobs.Items[0].RecoveryAction.CommandKey == nil ||
		*jobs.Items[0].RecoveryAction.CommandKey != CommandRetryJob {
		t.Fatalf("ListJobs() = %#v", jobs)
	}
	encodedJobs, _ := json.Marshal(jobs)
	if strings.Contains(string(encodedJobs), "private-task-id") {
		t.Fatalf("ListJobs() leaked scheduler task ID: %s", encodedJobs)
	}

	health, err := service.ListHealth(context.Background(), basequery.Request{
		Filters: []basequery.FilterTerm{{Field: "active", Operator: basequery.FilterEqual, Values: []string{"true"}}},
	})
	if err != nil {
		t.Fatalf("ListHealth() error = %v", err)
	}
	if len(health.Items) != 1 || health.Summary.Level != HealthBlocked ||
		health.Items[0].Component != "storage" || health.Items[0].Rule != "store_disk_full" ||
		health.Items[0].Impact != "storage_at_risk" || health.Items[0].Protection != "writes_stopped" ||
		health.Items[0].RecoveryAction.Kind != RecoveryFreeSpace ||
		health.Items[0].RecoveryAction.CommandKey == nil ||
		*health.Items[0].RecoveryAction.CommandKey != CommandFreeSpace {
		t.Fatalf("ListHealth() = %#v", health)
	}
	encodedHealth, _ := json.Marshal(health)
	if strings.Contains(string(encodedHealth), "private-fingerprint") || strings.Contains(string(encodedHealth), "fingerprint") {
		t.Fatalf("ListHealth() leaked fingerprint: %s", encodedHealth)
	}
	if !active {
		t.Fatal("unreachable guard")
	}
}

func TestRuntimeInfoErrorsAreContentFreeAndValidationStopsReaders(t *testing.T) {
	t.Parallel()

	privateCause := errors.New("dial /private/path with secret-token")
	calls := 0
	runtimeReader := &runtimeStub{
		sourcePage: func(context.Context, store.RuntimeSourceQuery) (store.RuntimeSourcePage, error) {
			calls++
			return store.RuntimeSourcePage{}, privateCause
		},
		source: func(context.Context, string) (store.RuntimeSourceRecord, error) {
			return store.RuntimeSourceRecord{}, store.ErrNotFound
		},
	}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	if _, err := service.ListSources(context.Background(), basequery.Request{
		Filters: []basequery.FilterTerm{{Field: "path", Operator: basequery.FilterContains, Values: []string{"/private"}}},
	}); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("ListSources(invalid) error = %v, want validation", err)
	}
	if calls != 0 {
		t.Fatalf("invalid request reached reader %d times", calls)
	}
	if _, err := service.ListSources(context.Background(), basequery.Request{}); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("ListSources(failure) error = %v, want unavailable", err)
	} else if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("failure leaked cause: %v", err)
	}
	if _, err := service.Source(context.Background(), SourceDetailRequest{SourceKey: "online:missing"}); !errors.Is(err, basequery.ErrNotFound) {
		t.Fatalf("Source(missing) error = %v, want not found", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ListJobs(ctx, basequery.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListJobs(cancelled) error = %v, want canceled", err)
	}
}

func TestHealthLevelUsesOnlyActiveEventsAndHonorsLifecyclePause(t *testing.T) {
	t.Parallel()

	resolvedCritical := store.RuntimeHealthSummary{
		Total: 1, Resolved: 1, Critical: 1,
	}
	healthy, err := mapHealthSummary(resolvedCritical, &store.SchedulerLifecycle{
		UserPauseScope: store.LifecyclePauseNone,
		SystemState:    store.LifecycleSystemAwake,
		Transition:     store.LifecycleTransitionSteady,
		SourceState:    store.LifecycleSourceAvailable,
	})
	if err != nil || healthy.Level != HealthHealthy {
		t.Fatalf("mapHealthSummary(resolved critical) = %#v, %v", healthy, err)
	}
	paused, err := mapHealthSummary(store.RuntimeHealthSummary{}, &store.SchedulerLifecycle{
		UserPauseScope: store.LifecyclePauseAll,
		SystemState:    store.LifecycleSystemAwake,
		Transition:     store.LifecycleTransitionSteady,
		SourceState:    store.LifecycleSourceAvailable,
	})
	if err != nil || paused.Level != HealthPaused {
		t.Fatalf("mapHealthSummary(paused) = %#v, %v", paused, err)
	}
	for name, lifecycle := range map[string]*store.SchedulerLifecycle{
		"missing": nil,
		"source unknown": {
			UserPauseScope: store.LifecyclePauseNone, SystemState: store.LifecycleSystemAwake,
			Transition: store.LifecycleTransitionSteady, SourceState: store.LifecycleSourceUnknown,
		},
		"source unavailable": {
			UserPauseScope: store.LifecyclePauseNone, SystemState: store.LifecycleSystemAwake,
			Transition: store.LifecycleTransitionSteady, SourceState: store.LifecycleSourceUnavailable,
		},
	} {
		mapped, err := mapHealthSummary(store.RuntimeHealthSummary{}, lifecycle)
		if err != nil || mapped.Level != HealthDegraded {
			t.Fatalf("mapHealthSummary(%s) = %#v, %v", name, mapped, err)
		}
	}
}

func TestRuntimeInfoEmptyListsAreCompleteAndCorruptTypedFactsFailClosed(t *testing.T) {
	t.Parallel()

	service := newTestService(t, &quotaStub{}, &runtimeStub{}, &preferencesStub{})
	for name, query := range map[string]func() (basequery.ResponseMeta, int, error){
		"sources": func() (basequery.ResponseMeta, int, error) {
			response, err := service.ListSources(context.Background(), basequery.Request{})
			return response.Meta, len(response.Items), err
		},
		"jobs": func() (basequery.ResponseMeta, int, error) {
			response, err := service.ListJobs(context.Background(), basequery.Request{})
			return response.Meta, len(response.Items), err
		},
		"health": func() (basequery.ResponseMeta, int, error) {
			response, err := service.ListHealth(context.Background(), basequery.Request{})
			return response.Meta, len(response.Items), err
		},
	} {
		meta, count, err := query()
		if err != nil || count != 0 || meta.Status != basequery.ResponseComplete ||
			meta.Page == nil || meta.Page.HasMore {
			t.Fatalf("%s empty response = %#v, count=%d, err=%v", name, meta, count, err)
		}
	}

	corruptReader := &runtimeStub{jobPage: func(context.Context, store.RuntimeJobQuery) (store.RuntimeJobPage, error) {
		return store.RuntimeJobPage{
			Records: []store.RuntimeJobRecord{{Job: store.JobRun{
				JobID: "job-corrupt", JobType: "safe", RequestedBy: "test",
				State: store.JobFailed, Phase: store.JobPhaseLive,
				CreatedAtMS: 1, UpdatedAtMS: 2,
				ErrorClass: pointerTo(store.RuntimeErrorClass("/private/raw-error")),
			}}},
			MatchedCount: 1, Summary: store.RuntimeJobSummary{Total: 1, Failed: 1},
		}, nil
	}}
	corruptService := newTestService(t, &quotaStub{}, corruptReader, &preferencesStub{})
	if _, err := corruptService.ListJobs(context.Background(), basequery.Request{}); !errors.Is(err, basequery.ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("ListJobs(corrupt fact) error = %v, want content-free unavailable", err)
	}
}

func TestRuntimeFiltersRejectDuplicateFieldsAndValuesBeforeReader(t *testing.T) {
	t.Parallel()

	calls := 0
	runtimeReader := &runtimeStub{sourcePage: func(
		context.Context, store.RuntimeSourceQuery,
	) (store.RuntimeSourcePage, error) {
		calls++
		return store.RuntimeSourcePage{}, nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	requests := []basequery.Request{
		{Filters: []basequery.FilterTerm{
			{Field: "kind", Operator: basequery.FilterEqual, Values: []string{"online"}},
			{Field: "kind", Operator: basequery.FilterIn, Values: []string{"local_file"}},
		}},
		{Filters: []basequery.FilterTerm{{
			Field: "kind", Operator: basequery.FilterIn, Values: []string{"online", "online"},
		}}},
	}
	for _, request := range requests {
		if _, err := service.ListSources(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ListSources(%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid filters reached reader %d times", calls)
	}
}

func TestSettingsRejectsJavaScriptUnsafeTimestamps(t *testing.T) {
	t.Parallel()

	preferencesReader := &preferencesStub{load: func(context.Context) (preferences.Snapshot, error) {
		snapshot := validSensitivePreferences()
		unsafe := basequery.JavaScriptMaxSafeInteger + 1
		snapshot.Updates.SnoozeUntilMS = &unsafe
		return snapshot, nil
	}}
	service := newTestService(t, &quotaStub{}, &runtimeStub{}, preferencesReader)
	if _, err := service.Settings(context.Background()); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("Settings(unsafe timestamp) error = %v, want unavailable", err)
	}
}

func TestSourcePartialIncludesUnavailableKindAndPreservesItems(t *testing.T) {
	t.Parallel()

	runtimeReader := &runtimeStub{sourcePage: func(
		context.Context, store.RuntimeSourceQuery,
	) (store.RuntimeSourcePage, error) {
		return store.RuntimeSourcePage{
			Records: []store.RuntimeSourceRecord{{
				SourceKey: "online:partial", Kind: store.RuntimeSourceOnline, UpdatedAtMS: 10,
				Online: &store.SourceState{
					SourceInstanceID: "partial", SourceType: "quota", ScopeKey: "private",
					FreshnessState: store.SourceFreshnessCurrent, UpdatedAtMS: 10,
				},
			}},
			MatchedCount:     1,
			Summary:          store.RuntimeSourceSummary{Total: 1, OnlineSources: 1},
			UnavailableKinds: []store.RuntimeSourceKind{store.RuntimeSourceLocalFile},
		}, nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	response, err := service.ListSources(context.Background(), basequery.Request{})
	if err != nil || response.Meta.Status != basequery.ResponsePartial || len(response.Items) != 1 ||
		!reflect.DeepEqual(response.UnavailableKinds, []SourceKind{SourceLocalFile}) {
		t.Fatalf("ListSources(partial kind) = %#v, %v", response, err)
	}
}

func TestRecoveryActionMatricesCoverTypedFailuresAndRegisteredCommands(t *testing.T) {
	t.Parallel()

	sourceCases := []struct {
		name        string
		errorClass  *store.RuntimeErrorClass
		failureCode *store.SourceFailureCode
		attention   bool
		want        RecoveryActionKind
	}{
		{name: "timeout", errorClass: pointerTo(store.RuntimeErrorTimeout), attention: true, want: RecoveryRetry},
		{name: "busy", errorClass: pointerTo(store.RuntimeErrorBusy), attention: true, want: RecoveryRetry},
		{name: "permission", errorClass: pointerTo(store.RuntimeErrorPermission), attention: true, want: RecoveryGrantPermission},
		{name: "disk full", errorClass: pointerTo(store.RuntimeErrorDiskFull), attention: true, want: RecoveryFreeSpace},
		{name: "corrupt", errorClass: pointerTo(store.RuntimeErrorCorrupt), attention: true, want: RecoveryRepairStore},
		{name: "read only", errorClass: pointerTo(store.RuntimeErrorReadOnly), attention: true, want: RecoveryGrantPermission},
		{name: "io", errorClass: pointerTo(store.RuntimeErrorIO), attention: true, want: RecoveryRetry},
		{name: "unavailable", errorClass: pointerTo(store.RuntimeErrorUnavailable), attention: true, want: RecoveryRetry},
		{name: "unknown", errorClass: pointerTo(store.RuntimeErrorUnknown), attention: true, want: RecoveryRetry},
		{name: "cancelled error", errorClass: pointerTo(store.RuntimeErrorCanceled), attention: true, want: RecoveryNone},
		{name: "invalid input", errorClass: pointerTo(store.RuntimeErrorInvalid), attention: true, want: RecoveryNone},
		{name: "auth failure", failureCode: pointerTo(store.SourceFailureAuthRequired), attention: true, want: RecoveryGrantPermission},
		{name: "network failure", failureCode: pointerTo(store.SourceFailureNetworkUnavailable), attention: true, want: RecoveryRetry},
		{name: "timeout failure", failureCode: pointerTo(store.SourceFailureTimeout), attention: true, want: RecoveryRetry},
		{name: "rate limit", failureCode: pointerTo(store.SourceFailureHTTP429), attention: true, want: RecoveryRetry},
		{name: "server failure", failureCode: pointerTo(store.SourceFailureServerError), attention: true, want: RecoveryRetry},
		{name: "schema mismatch", failureCode: pointerTo(store.SourceFailureSchemaIncompatible), attention: true, want: RecoveryCheckSource},
		{
			name:        "production network pair",
			errorClass:  pointerTo(store.RuntimeErrorUnavailable),
			failureCode: pointerTo(store.SourceFailureNetworkUnavailable),
			attention:   true,
			want:        RecoveryRetry,
		},
		{
			name:        "production timeout pair",
			errorClass:  pointerTo(store.RuntimeErrorTimeout),
			failureCode: pointerTo(store.SourceFailureTimeout),
			attention:   true,
			want:        RecoveryRetry,
		},
		{
			name:        "production auth pair",
			errorClass:  pointerTo(store.RuntimeErrorPermission),
			failureCode: pointerTo(store.SourceFailureAuthRequired),
			attention:   true,
			want:        RecoveryGrantPermission,
		},
		{
			name:        "production rate limit pair",
			errorClass:  pointerTo(store.RuntimeErrorUnavailable),
			failureCode: pointerTo(store.SourceFailureHTTP429),
			attention:   true,
			want:        RecoveryRetry,
		},
		{
			name:        "production server pair",
			errorClass:  pointerTo(store.RuntimeErrorUnavailable),
			failureCode: pointerTo(store.SourceFailureServerError),
			attention:   true,
			want:        RecoveryRetry,
		},
		{
			name:        "production schema mismatch pair",
			errorClass:  pointerTo(store.RuntimeErrorInvalid),
			failureCode: pointerTo(store.SourceFailureSchemaIncompatible),
			attention:   true,
			want:        RecoveryCheckSource,
		},
		{
			name:        "production cancelled pair",
			errorClass:  pointerTo(store.RuntimeErrorCanceled),
			failureCode: pointerTo(store.SourceFailureCancelled),
			attention:   true,
			want:        RecoveryNone,
		},
		{name: "cancelled", failureCode: pointerTo(store.SourceFailureCancelled), attention: true, want: RecoveryNone},
		{name: "state only", attention: true, want: RecoveryCheckSource},
		{name: "healthy", want: RecoveryNone},
	}
	for _, testCase := range sourceCases {
		t.Run("source "+testCase.name, func(t *testing.T) {
			action := sourceRecovery(testCase.errorClass, testCase.failureCode, testCase.attention)
			assertRegisteredRecovery(t, action, testCase.want)
		})
	}

	healthCases := []struct {
		name   string
		domain store.HealthDomain
		code   store.HealthCode
		want   RecoveryActionKind
	}{
		{name: "source timeout", domain: store.HealthDomainSource, code: store.HealthCodeSourceTimeout, want: RecoveryRetry},
		{name: "source unavailable", domain: store.HealthDomainSource, code: store.HealthCodeSourceUnavailable, want: RecoveryCheckSource},
		{name: "source permission", domain: store.HealthDomainSource, code: store.HealthCodeSourcePermission, want: RecoveryGrantPermission},
		{name: "source corrupt", domain: store.HealthDomainSource, code: store.HealthCodeSourceCorrupt, want: RecoveryRepairStore},
		{name: "source stale", domain: store.HealthDomainSource, code: store.HealthCodeSourceStale, want: RecoveryCheckSource},
		{name: "job interrupted", domain: store.HealthDomainJob, code: store.HealthCodeJobInterrupted, want: RecoveryRetry},
		{name: "job failed", domain: store.HealthDomainJob, code: store.HealthCodeJobFailed, want: RecoveryRetry},
		{name: "job cancelled", domain: store.HealthDomainJob, code: store.HealthCodeJobCancelled, want: RecoveryNone},
		{name: "store busy", domain: store.HealthDomainStore, code: store.HealthCodeStoreBusy, want: RecoveryRetry},
		{name: "store disk full", domain: store.HealthDomainStore, code: store.HealthCodeStoreDiskFull, want: RecoveryFreeSpace},
		{name: "store read only", domain: store.HealthDomainStore, code: store.HealthCodeStoreReadOnly, want: RecoveryGrantPermission},
		{name: "store permission", domain: store.HealthDomainStore, code: store.HealthCodeStorePermission, want: RecoveryGrantPermission},
		{name: "store io", domain: store.HealthDomainStore, code: store.HealthCodeStoreIO, want: RecoveryRetry},
		{name: "store corrupt", domain: store.HealthDomainStore, code: store.HealthCodeStoreCorrupt, want: RecoveryRepairStore},
		{name: "store unavailable", domain: store.HealthDomainStore, code: store.HealthCodeStoreUnavailable, want: RecoveryRetry},
		{name: "store unknown", domain: store.HealthDomainStore, code: store.HealthCodeStoreUnknown, want: RecoveryRetry},
		{name: "pricing unavailable", domain: store.HealthDomainPricing, code: store.HealthCodePricingUnavailable, want: RecoveryRetry},
		{name: "pricing invalid", domain: store.HealthDomainPricing, code: store.HealthCodePricingInvalid, want: RecoveryRepairStore},
		{name: "runtime unknown", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeUnknown, want: RecoveryRetry},
		{name: "source auth required", domain: store.HealthDomainSource, code: store.HealthCodeSourceAuthRequired, want: RecoveryGrantPermission},
		{name: "source failure streak", domain: store.HealthDomainSource, code: store.HealthCodeSourceFailureStreak, want: RecoveryRetry},
		{name: "live queue stalled", domain: store.HealthDomainJob, code: store.HealthCodeJobLiveQueueStalled, want: RecoveryRetry},
		{name: "backfill stalled", domain: store.HealthDomainJob, code: store.HealthCodeJobBackfillStalled, want: RecoveryRetry},
		{name: "store disk low", domain: store.HealthDomainStore, code: store.HealthCodeStoreDiskLow, want: RecoveryFreeSpace},
		{name: "store wal pressure", domain: store.HealthDomainStore, code: store.HealthCodeStoreWALPressure, want: RecoveryRetry},
		{name: "runtime cpu pressure", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeCPUPressure, want: RecoveryRetry},
		{name: "runtime memory pressure", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeMemoryPressure, want: RecoveryRetry},
		{name: "runtime metrics stale", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeMetricsStale, want: RecoveryRetry},
		{name: "updater unavailable", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeUpdaterUnavailable, want: RecoveryRetry},
		{name: "updater unknown", domain: store.HealthDomainRuntime, code: store.HealthCodeRuntimeUpdaterUnknown, want: RecoveryRetry},
	}
	for _, testCase := range healthCases {
		t.Run("health "+testCase.name, func(t *testing.T) {
			action := healthRecovery(store.HealthEvent{
				EventID: "matrix-event", Fingerprint: store.SHA256DigestOf([]byte(testCase.name)),
				Domain: testCase.domain, Severity: store.HealthError, Code: testCase.code,
				FirstSeenAtMS: 1, LastSeenAtMS: 1, OccurrenceCount: 1, UpdatedAtMS: 1,
			})
			assertRegisteredRecovery(t, action, testCase.want)
		})
	}

	storedCases := []struct {
		action store.SchedulerRecoveryAction
		want   RecoveryActionKind
	}{
		{action: store.SchedulerRecoveryNone, want: RecoveryNone},
		{action: store.SchedulerRecoveryRetry, want: RecoveryRetry},
		{action: store.SchedulerRecoveryCheckSource, want: RecoveryCheckSource},
		{action: store.SchedulerRecoveryGrantPermission, want: RecoveryGrantPermission},
		{action: store.SchedulerRecoveryFreeSpace, want: RecoveryFreeSpace},
		{action: store.SchedulerRecoveryChooseHome, want: RecoveryChooseHome},
		{action: store.SchedulerRecoveryRepairStore, want: RecoveryRepairStore},
	}
	for _, testCase := range storedCases {
		t.Run("stored "+string(testCase.action), func(t *testing.T) {
			assertRegisteredRecovery(
				t, recoveryForStoredAction(testCase.action, CommandRetryJob), testCase.want,
			)
		})
	}
}

func assertRegisteredRecovery(t *testing.T, action RecoveryAction, want RecoveryActionKind) {
	t.Helper()
	if action.Kind != want {
		t.Fatalf("recovery action = %#v, want kind %s", action, want)
	}
	if want == RecoveryNone {
		if action.CommandKey != nil {
			t.Fatalf("none recovery carried command: %#v", action)
		}
		return
	}
	if action.CommandKey == nil {
		t.Fatalf("recovery action lacks command: %#v", action)
	}
	registered := map[string]struct{}{
		CommandRetrySource: {}, CommandCheckSource: {}, CommandGrantPermission: {},
		CommandFreeSpace: {}, CommandChooseHome: {}, CommandRepairStore: {},
		CommandRetryJob: {}, CommandRetryHealth: {},
	}
	if _, ok := registered[*action.CommandKey]; !ok {
		t.Fatalf("recovery action command is not registered: %#v", action)
	}
}

type quotaStub struct {
	query func(context.Context, int64) (quotaquery.CurrentResponse, error)
}

func (stub *quotaStub) Query(ctx context.Context, atMS int64) (quotaquery.CurrentResponse, error) {
	if stub != nil && stub.query != nil {
		return stub.query(ctx, atMS)
	}
	return quotaquery.CurrentResponse{}, errors.New("quota stub is not configured")
}

type preferencesStub struct {
	load func(context.Context) (preferences.Snapshot, error)
}

func (stub *preferencesStub) LoadPreferences(ctx context.Context) (preferences.Snapshot, error) {
	if stub != nil && stub.load != nil {
		return stub.load(ctx)
	}
	return validSensitivePreferences(), nil
}

type runtimeStub struct {
	metrics    func(context.Context, store.MetricsSnapshotFilter) (store.MetricsSnapshot, error)
	sourcePage func(context.Context, store.RuntimeSourceQuery) (store.RuntimeSourcePage, error)
	source     func(context.Context, string) (store.RuntimeSourceRecord, error)
	jobPage    func(context.Context, store.RuntimeJobQuery) (store.RuntimeJobPage, error)
	job        func(context.Context, string) (store.RuntimeJobRecord, error)
	healthPage func(context.Context, store.RuntimeHealthQuery) (store.RuntimeHealthPage, error)
	health     func(context.Context, string) (store.HealthEvent, error)
}

func (stub *runtimeStub) MetricsSnapshot(ctx context.Context, filter store.MetricsSnapshotFilter) (store.MetricsSnapshot, error) {
	if stub != nil && stub.metrics != nil {
		return stub.metrics(ctx, filter)
	}
	return store.MetricsSnapshot{FromMS: filter.FromMS, UntilMS: filter.UntilMS, RuntimeSamples: []store.AppRuntimeSample{}}, nil
}

func (stub *runtimeStub) RuntimeSourcePage(ctx context.Context, filter store.RuntimeSourceQuery) (store.RuntimeSourcePage, error) {
	if stub != nil && stub.sourcePage != nil {
		return stub.sourcePage(ctx, filter)
	}
	return store.RuntimeSourcePage{Records: []store.RuntimeSourceRecord{}}, nil
}

func (stub *runtimeStub) RuntimeSource(ctx context.Context, key string) (store.RuntimeSourceRecord, error) {
	if stub != nil && stub.source != nil {
		return stub.source(ctx, key)
	}
	return store.RuntimeSourceRecord{}, store.ErrNotFound
}

func (stub *runtimeStub) RuntimeJobPage(ctx context.Context, filter store.RuntimeJobQuery) (store.RuntimeJobPage, error) {
	if stub != nil && stub.jobPage != nil {
		return stub.jobPage(ctx, filter)
	}
	return store.RuntimeJobPage{Records: []store.RuntimeJobRecord{}}, nil
}

func (stub *runtimeStub) RuntimeJob(ctx context.Context, key string) (store.RuntimeJobRecord, error) {
	if stub != nil && stub.job != nil {
		return stub.job(ctx, key)
	}
	return store.RuntimeJobRecord{}, store.ErrNotFound
}

func (stub *runtimeStub) RuntimeHealthPage(ctx context.Context, filter store.RuntimeHealthQuery) (store.RuntimeHealthPage, error) {
	if stub != nil && stub.healthPage != nil {
		return stub.healthPage(ctx, filter)
	}
	return store.RuntimeHealthPage{Records: []store.HealthEvent{}}, nil
}

func (stub *runtimeStub) RuntimeHealth(ctx context.Context, key string) (store.HealthEvent, error) {
	if stub != nil && stub.health != nil {
		return stub.health(ctx, key)
	}
	return store.HealthEvent{}, store.ErrNotFound
}

func newTestService(
	t *testing.T,
	quotaReader QuotaReader,
	runtimeReader RuntimeReader,
	preferencesReader PreferencesReader,
) *Service {
	t.Helper()
	service, err := NewService(Dependencies{
		Quota: quotaReader, Runtime: runtimeReader, Preferences: preferencesReader,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func validSensitivePreferences() preferences.Snapshot {
	active := preferences.CodexHomePreferences{
		Source: preferences.ConfirmedSource{
			Path: "/Users/private/.codex", DeviceID: "private-device", Inode: 99, ConfirmedAtMS: 1,
		},
		Generation: 3, DataStoreKey: "private-store-key",
	}
	previous := preferences.CodexHomePreferences{
		Source: preferences.ConfirmedSource{
			Path: "/Users/private/detached", DeviceID: "detached-private", Inode: 100, ConfirmedAtMS: 1,
		},
		Generation: 2, DataStoreKey: "detached-private-key",
	}
	return preferences.Snapshot{
		SchemaVersion: preferences.CurrentPreferencesSchemaVersion,
		Revision:      7,
		Onboarding: preferences.OnboardingPreferences{
			Version: preferences.CurrentOnboardingVersion, Completed: true,
		},
		CodexHome:     active,
		Online:        preferences.OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: true},
		Refresh:       preferences.DefaultRefreshPreferences(),
		Updates:       preferences.DefaultUpdatePreferences(),
		UI:            preferences.DefaultUIPreferences(),
		DetachedHomes: []preferences.CodexHomePreferences{previous},
		PendingSwitch: &preferences.HomeSwitchJournal{
			SwitchID: "private-switch-id", AttemptID: "0123456789abcdef0123456789abcdef",
			Previous: previous, Target: active,
			Strategy: preferences.HomeSwitchIndependentDatabase, StartedAtMS: 2,
		},
	}
}

func assertEditableField(
	t *testing.T,
	fields []EditableField,
	key string,
	editable bool,
	minimum *int64,
	maximum *int64,
) {
	t.Helper()
	for _, field := range fields {
		if field.Key == key {
			if field.Editable != editable || !equalInt64Pointer(field.Minimum, minimum) ||
				!equalInt64Pointer(field.Maximum, maximum) {
				t.Fatalf("field %s = %#v", key, field)
			}
			return
		}
	}
	t.Fatalf("field %s missing from %#v", key, fields)
}

func assertEditableOptions(t *testing.T, fields []EditableField, key string, want []string) {
	t.Helper()
	for _, field := range fields {
		if field.Key == key {
			if !reflect.DeepEqual(field.Options, want) {
				t.Fatalf("field %s options = %#v, want %#v", key, field.Options, want)
			}
			return
		}
	}
	t.Fatalf("field %s missing from %#v", key, fields)
}

func equalInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func int64Pointer(value int64) *int64 { return &value }

func pointerTo[T any](value T) *T { return &value }
