package health

import (
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestEvaluatorProducesStableComponentProjectionAndEventPlan(t *testing.T) {
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	input := healthyInput()
	first, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(healthy) error = %v", err)
	}
	if first.Level != LevelHealthy || first.Primary != nil || len(first.Components) != 7 ||
		len(first.EventBatch.Observations) != 0 || len(first.EventBatch.ManagedEvents) == 0 {
		t.Fatalf("Evaluate(healthy) = %#v", first)
	}
	updater := componentByName(t, first.Components, ComponentUpdater)
	if updater.Evidence != EvidenceKnown || updater.Reason != ReasonHealthy {
		t.Fatalf("updater = %#v, want known healthy", updater)
	}

	input.Updater = UpdaterNotConfigured
	notConfigured, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(not configured) error = %v", err)
	}
	updater = componentByName(t, notConfigured.Components, ComponentUpdater)
	if notConfigured.Level != LevelHealthy || updater.Evidence != EvidenceNotConfigured ||
		updater.Reason != ReasonNotConfigured || updater.Level != LevelHealthy {
		t.Fatalf("not-configured updater = %#v; summary=%s", updater, notConfigured.Level)
	}
	if !reflect.DeepEqual(first.EventBatch.ManagedEvents, notConfigured.EventBatch.ManagedEvents) {
		t.Fatal("managed event identities changed with current facts")
	}
}

func TestEvaluatorRulePriorityAndFiniteDescriptors(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*Input)
		wantLevel      Level
		wantComponent  Component
		wantCode       store.HealthCode
		wantImpact     Impact
		wantProtection Protection
		wantAction     RecoveryAction
	}{
		{
			name: "live queue stalled", mutate: func(input *Input) {
				input.Snapshot.Metrics.RuntimeSamples[0].OldestLiveWaitMS = 30_001
			},
			wantLevel: LevelDegraded, wantComponent: ComponentLiveQueue,
			wantCode: store.HealthCodeJobLiveQueueStalled, wantImpact: ImpactLiveDataDelayed,
			wantProtection: ProtectionRetryBackoff,
			wantAction:     RecoveryRetry,
		},
		{
			name: "backfill stalled", mutate: func(input *Input) {
				input.Snapshot.Metrics.RuntimeSamples[0].BackfillQueueDepth = 1
				input.Snapshot.Metrics.RuntimeSamples[0].OldestBackfillWaitMS = 300_001
				progress := input.EvaluatedAtMS - 300_001
				input.Snapshot.Metrics.Scheduler.LastProgressAtMS = &progress
			},
			wantLevel: LevelDegraded, wantComponent: ComponentHistoryBackfill,
			wantCode: store.HealthCodeJobBackfillStalled, wantImpact: ImpactHistoryIncomplete,
			wantProtection: ProtectionRetryBackoff,
			wantAction:     RecoveryRetry,
		},
		{
			name: "quota auth required", mutate: func(input *Input) {
				input.Snapshot.Metrics.Sources.Current = 0
				input.Snapshot.Metrics.Sources.Unavailable = 1
				input.Snapshot.Metrics.Sources.CurrentFailureCodes = []store.SourceFailureCodeMetric{{
					FailureCode: store.SourceFailureAuthRequired, Count: 1,
				}}
			},
			wantLevel: LevelDegraded, wantComponent: ComponentOnlineQuota,
			wantCode: store.HealthCodeSourceAuthRequired, wantImpact: ImpactOnlineQuotaUnavailable,
			wantProtection: ProtectionAutoRetryStopped,
			wantAction:     RecoveryGrantPermission,
		},
		{
			name: "disk low warning", mutate: func(input *Input) {
				input.Snapshot.Metrics.RuntimeSamples[0].DiskFreeBytes = (1 << 30) - 1
			},
			wantLevel: LevelBusy, wantComponent: ComponentStorage,
			wantCode: store.HealthCodeStoreDiskLow, wantImpact: ImpactStorageAtRisk,
			wantProtection: ProtectionObservationOnly,
			wantAction:     RecoveryFreeSpace,
		},
		{
			name: "lifecycle blocked wins", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.Transition = store.LifecycleTransitionBlocked
				input.Snapshot.Metrics.RuntimeSamples[0].DiskFreeBytes = (1 << 30) - 1
			},
			wantLevel: LevelBlocked, wantComponent: ComponentLocalIndex,
			wantCode: store.HealthCodeSourceUnavailable, wantImpact: ImpactIndexingStopped,
			wantProtection: ProtectionWritesStopped,
			wantAction:     RecoveryCheckSource,
		},
		{
			name: "durable pause precedes degraded", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.UserPauseScope = store.LifecyclePauseAll
				input.Snapshot.Metrics.Sources.Current = 0
				input.Snapshot.Metrics.Sources.Unavailable = 1
			},
			wantLevel: LevelPaused, wantComponent: ComponentLocalIndex,
			wantImpact: ImpactIndexingPaused, wantProtection: ProtectionUserPauseRetained,
			wantAction: RecoveryNone,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			evaluator, err := NewEvaluator(DefaultThresholds())
			if err != nil {
				t.Fatalf("NewEvaluator() error = %v", err)
			}
			input := healthyInput()
			testCase.mutate(&input)
			result, err := evaluator.Evaluate(input)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if result.Level != testCase.wantLevel || result.Primary == nil ||
				result.Primary.Component != testCase.wantComponent ||
				result.Primary.Impact != testCase.wantImpact ||
				result.Primary.Protection != testCase.wantProtection ||
				result.Primary.RecoveryAction != testCase.wantAction {
				t.Fatalf("Evaluate() primary = %#v; level=%s", result.Primary, result.Level)
			}
			if testCase.wantCode != "" {
				if result.Primary.EventCode != testCase.wantCode ||
					!hasObservationCode(result.EventBatch.Observations, testCase.wantCode) {
					t.Fatalf("Evaluate() events = %#v; primary=%#v", result.EventBatch.Observations, result.Primary)
				}
			}
		})
	}
}

func TestEvaluatorRequiresContinuousResourcePressure(t *testing.T) {
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	input := healthyInput()
	input.Snapshot.Metrics.Jobs.Running = 1
	input.Snapshot.Metrics.RuntimeSamples = []store.AppRuntimeSample{
		runtimeSample(input.EvaluatedAtMS, 21, 513<<20),
		runtimeSample(input.EvaluatedAtMS-60_000, 22, 514<<20),
		runtimeSample(input.EvaluatedAtMS-120_000, 23, 515<<20),
	}
	result, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(sustained) error = %v", err)
	}
	if result.Level != LevelDegraded || !hasObservationCode(
		result.EventBatch.Observations, store.HealthCodeRuntimeCPUPressure,
	) || !hasObservationCode(result.EventBatch.Observations, store.HealthCodeRuntimeMemoryPressure) {
		t.Fatalf("Evaluate(sustained) = %#v", result)
	}

	input.Snapshot.Metrics.RuntimeSamples[1].CapturedAtMS = input.EvaluatedAtMS - 70_000
	input.Snapshot.Metrics.RuntimeSamples[2].CapturedAtMS = input.EvaluatedAtMS - 140_000
	gap, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(gap) error = %v", err)
	}
	if hasObservationCode(gap.EventBatch.Observations, store.HealthCodeRuntimeCPUPressure) ||
		hasObservationCode(gap.EventBatch.Observations, store.HealthCodeRuntimeMemoryPressure) {
		t.Fatalf("sample gap triggered sustained pressure: %#v", gap.EventBatch.Observations)
	}
}

func TestEvaluatorAcceptsContinuousPressureAcrossUnalignedWindowBoundary(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	input := healthyInput()
	input.Snapshot.Metrics.Jobs.Running = 1
	input.Snapshot.Metrics.RuntimeSamples = []store.AppRuntimeSample{
		runtimeSample(input.EvaluatedAtMS, 25, 600<<20),
		runtimeSample(input.EvaluatedAtMS-60_000, 25, 600<<20),
		runtimeSample(input.EvaluatedAtMS-119_000, 25, 600<<20),
		runtimeSample(input.EvaluatedAtMS-124_000, 25, 600<<20),
	}
	result, err := evaluator.Evaluate(input)
	if err != nil || !hasObservationCode(result.EventBatch.Observations, store.HealthCodeRuntimeCPUPressure) {
		t.Fatalf("Evaluate(unaligned sustained pressure) = %#v, %v", result, err)
	}
}

func TestEvaluatorSeparatesUnknownAndTypedDomainFailuresFromHealthy(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	unknown := healthyInput()
	unknown.Snapshot.Lifecycle = nil
	result, err := evaluator.Evaluate(unknown)
	if err != nil {
		t.Fatal(err)
	}
	local := componentByName(t, result.Components, ComponentLocalIndex)
	if result.Level != LevelDegraded || local.Evidence != EvidenceUnknown || local.Reason != ReasonLifecycleUnknown {
		t.Fatalf("unknown lifecycle result = %#v", result)
	}

	typed := healthyInput()
	typed.Snapshot.ActiveHealth = []store.HealthEventMetric{{
		Domain: store.HealthDomainStore, Severity: store.HealthCritical,
		Code: store.HealthCodeStoreCorrupt, Count: 1,
	}}
	result, err = evaluator.Evaluate(typed)
	if err != nil {
		t.Fatal(err)
	}
	storage := componentByName(t, result.Components, ComponentStorage)
	if result.Level != LevelBlocked || storage.Reason != ReasonStoreCorrupt ||
		storage.RecoveryAction != RecoveryRepairStore || len(result.EventBatch.Observations) != 0 {
		t.Fatalf("typed domain failure result = %#v", result)
	}
}

func TestEvaluatorUsesThreeAndTenSourceFailureBoundaries(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		failures int64
		want     Level
		active   bool
	}{{2, LevelHealthy, false}, {3, LevelBusy, true}, {9, LevelBusy, true}, {10, LevelDegraded, true}} {
		input := healthyInput()
		input.Snapshot.Metrics.Sources.MaxConsecutiveFailures = testCase.failures
		result, err := evaluator.Evaluate(input)
		if err != nil {
			t.Fatal(err)
		}
		observed := hasObservationCode(result.EventBatch.Observations, store.HealthCodeSourceFailureStreak)
		if result.Level != testCase.want || observed != testCase.active {
			t.Fatalf("failures=%d result=%#v", testCase.failures, result)
		}
	}
}

func TestEvaluatorIsInputOrderIndependent(t *testing.T) {
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	input := healthyInput()
	input.Snapshot.Metrics.Jobs.Running = 1
	input.Snapshot.Metrics.RuntimeSamples = []store.AppRuntimeSample{
		runtimeSample(input.EvaluatedAtMS-120_000, 25, 600<<20),
		runtimeSample(input.EvaluatedAtMS, 25, 600<<20),
		runtimeSample(input.EvaluatedAtMS-60_000, 25, 600<<20),
	}
	first, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(shuffled) error = %v", err)
	}
	input.Snapshot.Metrics.RuntimeSamples[0], input.Snapshot.Metrics.RuntimeSamples[2] =
		input.Snapshot.Metrics.RuntimeSamples[2], input.Snapshot.Metrics.RuntimeSamples[0]
	second, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate(reordered) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("evaluation depends on input order:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestEvaluatorMarksMissingOrStaleMetricsAsUnknown(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Input){
		func(input *Input) { input.Snapshot.Metrics.RuntimeSamples = nil },
		func(input *Input) { input.Snapshot.Metrics.RuntimeSamples[0].CapturedAtMS -= 65_001 },
	} {
		input := healthyInput()
		mutate(&input)
		result, err := evaluator.Evaluate(input)
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		runtime := componentByName(t, result.Components, ComponentRuntime)
		if result.Level != LevelDegraded || runtime.Evidence != EvidenceUnknown ||
			runtime.Reason != ReasonMetricsStale ||
			!hasObservationCode(result.EventBatch.Observations, store.HealthCodeRuntimeMetricsStale) {
			t.Fatalf("stale metrics result = %#v", result)
		}
	}
}

func TestEvaluatorMapsDurableLifecycleStatesWithStablePriority(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name          string
		mutate        func(*Input)
		wantLevel     Level
		wantComponent Component
		wantReason    Reason
		wantEvidence  Evidence
	}{
		{
			name: "pause outranks source unavailable", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.UserPauseScope = store.LifecyclePauseAll
				input.Snapshot.Lifecycle.SourceState = store.LifecycleSourceUnavailable
			},
			wantLevel: LevelPaused, wantComponent: ComponentLocalIndex,
			wantReason: ReasonIndexPaused, wantEvidence: EvidenceKnown,
		},
		{
			name: "source unavailable is degraded", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.SourceState = store.LifecycleSourceUnavailable
			},
			wantLevel: LevelDegraded, wantComponent: ComponentLocalIndex,
			wantReason: ReasonSourceUnavailable, wantEvidence: EvidenceKnown,
		},
		{
			name: "system sleeping is paused", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.SystemState = store.LifecycleSystemSleeping
			},
			wantLevel: LevelPaused, wantComponent: ComponentLocalIndex,
			wantReason: ReasonSystemSleeping, wantEvidence: EvidenceKnown,
		},
		{
			name: "reconciling is busy", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.Transition = store.LifecycleTransitionReconciling
			},
			wantLevel: LevelBusy, wantComponent: ComponentLocalIndex,
			wantReason: ReasonIndexReconciling, wantEvidence: EvidenceKnown,
		},
		{
			name: "source unknown is degraded unknown", mutate: func(input *Input) {
				input.Snapshot.Lifecycle.SourceState = store.LifecycleSourceUnknown
			},
			wantLevel: LevelDegraded, wantComponent: ComponentLocalIndex,
			wantReason: ReasonSourceUnknown, wantEvidence: EvidenceUnknown,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := healthyInput()
			testCase.mutate(&input)
			result, err := evaluator.Evaluate(input)
			if err != nil {
				t.Fatal(err)
			}
			status := componentByName(t, result.Components, testCase.wantComponent)
			if result.Level != testCase.wantLevel || status.Level != testCase.wantLevel ||
				status.Reason != testCase.wantReason || status.Evidence != testCase.wantEvidence {
				t.Fatalf("Evaluate() result=%#v status=%#v", result, status)
			}
		})
	}

	for _, updatedAtMS := range []int64{-1, healthyInput().EvaluatedAtMS + 1} {
		input := healthyInput()
		input.Snapshot.Lifecycle.UpdatedAtMS = updatedAtMS
		if _, err := evaluator.Evaluate(input); err == nil {
			t.Fatalf("Evaluate(lifecycle updatedAt=%d) succeeded", updatedAtMS)
		}
	}
	blocked := healthyInput()
	blocked.Snapshot.Lifecycle.Transition = store.LifecycleTransitionBlocked
	blocked.Snapshot.Lifecycle.SourceState = store.LifecycleSourceUnavailable
	result, err := evaluator.Evaluate(blocked)
	if err != nil || result.Level != LevelBlocked ||
		countObservationCode(result.EventBatch.Observations, store.HealthCodeSourceUnavailable) != 1 {
		t.Fatalf("blocked+unavailable lifecycle = %#v, %v", result, err)
	}
}

func TestEvaluatorSuppressesQueueStallEventsWhileLaneIsPaused(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*Input)
	}{
		{
			name: "backfill pause suppresses backfill stall",
			mutate: func(input *Input) {
				input.Snapshot.Lifecycle.UserPauseScope = store.LifecyclePauseBackfill
			},
		},
		{
			name: "pause all suppresses every queue stall",
			mutate: func(input *Input) {
				input.Snapshot.Lifecycle.UserPauseScope = store.LifecyclePauseAll
			},
		},
		{
			name: "sleeping suppresses every queue stall",
			mutate: func(input *Input) {
				input.Snapshot.Lifecycle.SystemState = store.LifecycleSystemSleeping
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := healthyInput()
			input.Snapshot.Metrics.RuntimeSamples[0].OldestLiveWaitMS = DefaultThresholds().LiveWaitMS + 1
			input.Snapshot.Metrics.RuntimeSamples[0].BackfillQueueDepth = 1
			input.Snapshot.Metrics.RuntimeSamples[0].OldestBackfillWaitMS = DefaultThresholds().BackfillWaitMS + 1
			testCase.mutate(&input)

			result, err := evaluator.Evaluate(input)
			if err != nil {
				t.Fatal(err)
			}
			if status := componentByName(t, result.Components, ComponentHistoryBackfill); status.Level != LevelPaused {
				t.Fatalf("backfill projection = %#v, want paused", status)
			}
			if hasObservationCode(result.EventBatch.Observations, store.HealthCodeJobBackfillStalled) {
				t.Fatalf("paused backfill produced stalled event: %#v", result.EventBatch.Observations)
			}
			if input.Snapshot.Lifecycle.UserPauseScope == store.LifecyclePauseAll ||
				input.Snapshot.Lifecycle.SystemState == store.LifecycleSystemSleeping {
				if status := componentByName(t, result.Components, ComponentLiveQueue); status.Level != LevelPaused {
					t.Fatalf("live queue projection = %#v, want paused", status)
				}
				if hasObservationCode(result.EventBatch.Observations, store.HealthCodeJobLiveQueueStalled) {
					t.Fatalf("paused live queue produced stalled event: %#v", result.EventBatch.Observations)
				}
			}
		})
	}
}

func TestEvaluatorNeverLetsPauseOrNotConfiguredHideCriticalEvidence(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		metric store.HealthEventMetric
		mutate func(*Input)
	}{
		{
			name: "pause does not hide local critical",
			metric: store.HealthEventMetric{Domain: store.HealthDomainSource, Severity: store.HealthCritical,
				Code: store.HealthCodeSourceCorrupt, Count: 1},
			mutate: func(input *Input) { input.Snapshot.Lifecycle.UserPauseScope = store.LifecyclePauseAll },
		},
		{
			name: "not configured does not hide updater critical",
			metric: store.HealthEventMetric{Domain: store.HealthDomainRuntime, Severity: store.HealthCritical,
				Code: store.HealthCodeRuntimeUpdaterUnavailable, Count: 1},
			mutate: func(input *Input) { input.Updater = UpdaterNotConfigured },
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := healthyInput()
			input.Snapshot.ActiveHealth = []store.HealthEventMetric{testCase.metric}
			testCase.mutate(&input)
			result, err := evaluator.Evaluate(input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Level != LevelBlocked || result.Primary == nil || result.Primary.EventCode != testCase.metric.Code {
				t.Fatalf("critical evidence was hidden: %#v", result)
			}
		})
	}
}

func TestEvaluatorDoesNotDeriveSpecificFailuresFromStaleRuntimeSample(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	input := healthyInput()
	input.Snapshot.Metrics.RuntimeSamples[0].CapturedAtMS = input.EvaluatedAtMS - 65_001
	input.Snapshot.Metrics.RuntimeSamples[0].OldestLiveWaitMS = 30_001
	input.Snapshot.Metrics.RuntimeSamples[0].BackfillQueueDepth = 1
	input.Snapshot.Metrics.RuntimeSamples[0].OldestBackfillWaitMS = 300_001
	input.Snapshot.Metrics.RuntimeSamples[0].DiskFreeBytes = 1
	result, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.EventBatch.Observations) != 1 ||
		result.EventBatch.Observations[0].Code != store.HealthCodeRuntimeMetricsStale {
		t.Fatalf("stale sample produced specific observations: %#v", result.EventBatch.Observations)
	}
}

func TestEvaluatorUsesBackfillLaneProgressInsteadOfRecentLiveProgress(t *testing.T) {
	t.Parallel()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	input := healthyInput()
	input.Snapshot.Metrics.RuntimeSamples[0].BackfillQueueDepth = 1
	input.Snapshot.Metrics.RuntimeSamples[0].OldestBackfillWaitMS = 300_001
	recentLive := input.EvaluatedAtMS - 1
	oldBackfill := input.EvaluatedAtMS - 300_001
	input.Snapshot.Metrics.Scheduler.LastProgressAtMS = &recentLive
	input.Snapshot.Metrics.Scheduler.LastBackfillProgressAtMS = &oldBackfill
	result, err := evaluator.Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !hasObservationCode(result.EventBatch.Observations, store.HealthCodeJobBackfillStalled) {
		t.Fatalf("recent live progress hid backfill stall: %#v", result)
	}
}

func TestDescribeEventCoversLegacyAndEvaluatorCodes(t *testing.T) {
	t.Parallel()
	for _, value := range []struct {
		domain store.HealthDomain
		code   store.HealthCode
	}{
		{store.HealthDomainSource, store.HealthCodeSourceTimeout},
		{store.HealthDomainJob, store.HealthCodeJobLiveQueueStalled},
		{store.HealthDomainStore, store.HealthCodeStoreWALPressure},
		{store.HealthDomainRuntime, store.HealthCodeRuntimeMetricsStale},
		{store.HealthDomainPricing, store.HealthCodePricingInvalid},
	} {
		descriptor, ok := DescribeEvent(value.domain, value.code)
		if !ok || descriptor.Component == "" || descriptor.Rule == "" || descriptor.Impact == "" ||
			descriptor.Protection == "" || descriptor.RecoveryAction == "" {
			t.Fatalf("DescribeEvent(%s, %s) = %#v, %v", value.domain, value.code, descriptor, ok)
		}
	}
	if _, ok := DescribeEvent(store.HealthDomainStore, store.HealthCodeSourceTimeout); ok {
		t.Fatal("DescribeEvent() accepted a domain/code mismatch")
	}
}

func BenchmarkEvaluator(b *testing.B) {
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		b.Fatal(err)
	}
	input := healthyInput()
	input.EvaluatedAtMS = 100_000_000
	input.Snapshot.Metrics.FromMS = input.EvaluatedAtMS - store.MetricsSnapshotWindowMS
	input.Snapshot.Metrics.UntilMS = input.EvaluatedAtMS
	input.Snapshot.Metrics.RuntimeSamples = make([]store.AppRuntimeSample, 0, 17280)
	for index := int64(0); index < 17280; index++ {
		input.Snapshot.Metrics.RuntimeSamples = append(input.Snapshot.Metrics.RuntimeSamples,
			runtimeSample(input.EvaluatedAtMS-index*5_000, 5, 128<<20))
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := evaluator.Evaluate(input); err != nil {
			b.Fatal(err)
		}
	}
}

func healthyInput() Input {
	atMS := int64(1_800_000)
	return Input{
		EvaluatedAtMS: atMS,
		Updater:       UpdaterCurrent,
		Snapshot: store.HealthEvaluationSnapshot{
			Metrics: store.MetricsSnapshot{
				FromMS: atMS - store.MetricsSnapshotWindowMS, UntilMS: atMS,
				RuntimeSamples: []store.AppRuntimeSample{runtimeSample(atMS, 5, 128<<20)},
				Sources:        store.SourceMetrics{Total: 1, Current: 1},
			},
			Lifecycle: &store.SchedulerLifecycle{
				HomeGeneration: 1, UserPauseScope: store.LifecyclePauseNone,
				SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
				SourceState: store.LifecycleSourceAvailable, Revision: 1, LastEventID: "health-fixture",
				UpdatedAtMS: atMS,
			},
		},
	}
}

func runtimeSample(atMS int64, cpu float64, rss int64) store.AppRuntimeSample {
	return store.AppRuntimeSample{
		CapturedAtMS: atMS, CPUPercent: cpu, CPUUserMS: atMS / 2, CPUSystemMS: atMS / 4,
		RSSBytes: rss, PeakRSSBytes: rss, GoroutineCount: 10, DBBytes: 1024, WALBytes: 0,
		DiskFreeBytes: 10 << 30,
	}
}

func componentByName(t *testing.T, values []ComponentStatus, component Component) ComponentStatus {
	t.Helper()
	for _, value := range values {
		if value.Component == component {
			return value
		}
	}
	t.Fatalf("component %q is missing from %#v", component, values)
	return ComponentStatus{}
}

func hasObservationCode(values []store.HealthObservation, code store.HealthCode) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func countObservationCode(values []store.HealthObservation, code store.HealthCode) int {
	count := 0
	for _, value := range values {
		if value.Code == code {
			count++
		}
	}
	return count
}
