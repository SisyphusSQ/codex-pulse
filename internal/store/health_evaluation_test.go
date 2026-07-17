package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestHealthEvaluationSnapshotReadsMetricsAndLifecycleTogether(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	lifecycle := SchedulerLifecycle{
		HomeGeneration: 1, UserPauseScope: LifecyclePauseNone, SystemState: LifecycleSystemAwake,
		Transition: LifecycleTransitionSteady, SourceState: LifecycleSourceAvailable,
		LastEventID: "health-snapshot", Revision: 1, UpdatedAtMS: 100,
	}
	if _, err := repository.InitializeSchedulerLifecycle(t.Context(), lifecycle); err != nil {
		t.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}
	if err := repository.RecordAppRuntimeSample(t.Context(), validAppRuntimeSample(200)); err != nil {
		t.Fatalf("RecordAppRuntimeSample() error = %v", err)
	}
	if _, err := repository.ObserveHealthEvent(t.Context(), healthEvaluationObservation(
		"external-store-failure", HealthDomainStore, HealthCodeStoreDiskFull, 200,
	)); err != nil {
		t.Fatalf("ObserveHealthEvent() error = %v", err)
	}
	if _, err := repository.ObserveHealthEvent(t.Context(), healthEvaluationObservation(
		"health-evaluator-self-managed", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 200,
	)); err != nil {
		t.Fatalf("ObserveHealthEvent(managed) error = %v", err)
	}

	managedObservation := healthEvaluationObservation(
		"health-evaluator-self-managed", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 200,
	)
	snapshot, err := repository.HealthEvaluationSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	}, healthEvaluationManagedEvents(managedObservation))
	if err != nil {
		t.Fatalf("HealthEvaluationSnapshot() error = %v", err)
	}
	if len(snapshot.Metrics.RuntimeSamples) != 1 || snapshot.Metrics.RuntimeSamples[0].CapturedAtMS != 200 ||
		snapshot.Lifecycle == nil || *snapshot.Lifecycle != lifecycle ||
		len(snapshot.ActiveHealth) != 1 || snapshot.ActiveHealth[0] != (HealthEventMetric{
		Domain: HealthDomainStore, Severity: HealthWarning, Code: HealthCodeStoreDiskFull, Count: 1,
	}) {
		t.Fatalf("HealthEvaluationSnapshot() = %#v", snapshot)
	}
}

func TestHealthEvaluationSnapshotUsesOneReadTransactionAcrossConcurrentWriterCommit(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	if err := repository.RecordAppRuntimeSample(t.Context(), validAppRuntimeSample(100)); err != nil {
		t.Fatalf("RecordAppRuntimeSample() error = %v", err)
	}
	managed := healthEvaluationManagedEvents(healthEvaluationObservation(
		"health-evaluator-concurrent-owned", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 100,
	))
	var once sync.Once
	repository.metricsSnapshotReadHook = func(stage string) error {
		if stage != "runtime" {
			return nil
		}
		var hookErr error
		once.Do(func() {
			if _, err := repository.InitializeSchedulerLifecycle(context.Background(), SchedulerLifecycle{
				HomeGeneration: 1, UserPauseScope: LifecyclePauseNone, SystemState: LifecycleSystemAwake,
				Transition: LifecycleTransitionSteady, SourceState: LifecycleSourceAvailable,
				LastEventID: "health-concurrent", Revision: 1, UpdatedAtMS: 150,
			}); err != nil {
				hookErr = err
				return
			}
			_, hookErr = repository.ObserveHealthEvent(context.Background(), healthEvaluationObservation(
				"health-concurrent-external", HealthDomainStore, HealthCodeStoreDiskFull, 150,
			))
		})
		return hookErr
	}
	filter := MetricsSnapshotFilter{FromMS: 0, UntilMS: MetricsSnapshotWindowMS}
	first, err := repository.HealthEvaluationSnapshot(t.Context(), filter, managed)
	if err != nil {
		t.Fatalf("HealthEvaluationSnapshot(first) error = %v", err)
	}
	if first.Lifecycle != nil || len(first.ActiveHealth) != 0 {
		t.Fatalf("first snapshot mixed later commits: %#v", first)
	}
	repository.metricsSnapshotReadHook = nil
	second, err := repository.HealthEvaluationSnapshot(t.Context(), filter, managed)
	if err != nil || second.Lifecycle == nil || len(second.ActiveHealth) != 1 ||
		second.ActiveHealth[0].Code != HealthCodeStoreDiskFull {
		t.Fatalf("second snapshot = %#v, %v", second, err)
	}
}

func TestApplyHealthEvaluationBatchReplayResolveAndReopenLifecycle(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	observation := healthEvaluationObservation("health-evaluator-lifecycle", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 10)
	batch := HealthEvaluationBatch{
		Observations: []HealthObservation{observation}, ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 10,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.ApplyHealthEvaluationBatch(t.Context(), batch); err != nil {
			t.Fatalf("ApplyHealthEvaluationBatch(replay %d) error = %v", attempt, err)
		}
	}
	replayed, err := repository.HealthEvent(t.Context(), observation.EventID)
	if err != nil || replayed.OccurrenceCount != 1 || replayed.LastSeenAtMS != 10 {
		t.Fatalf("replayed event = %#v, %v", replayed, err)
	}

	observation.ObservedAtMS = 20
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		Observations: []HealthObservation{observation}, ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 20,
	}); err != nil {
		t.Fatalf("ApplyHealthEvaluationBatch(advance) error = %v", err)
	}
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 30,
	}); err != nil {
		t.Fatalf("ApplyHealthEvaluationBatch(resolve) error = %v", err)
	}
	observation.ObservedAtMS = 40
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		Observations: []HealthObservation{observation}, ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 40,
	}); err != nil {
		t.Fatalf("ApplyHealthEvaluationBatch(reopen) error = %v", err)
	}
	reopened, err := repository.HealthEvent(t.Context(), observation.EventID)
	if err != nil || reopened.OccurrenceCount != 3 || reopened.FirstSeenAtMS != 10 ||
		reopened.LastSeenAtMS != 40 || reopened.ResolvedAtMS != nil {
		t.Fatalf("reopened event = %#v, %v", reopened, err)
	}
}

func TestApplyHealthEvaluationBatchObservesAndResolvesManagedEventsAtomically(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	first := healthEvaluationObservation("health-evaluator-managed-first", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 10)
	unmanaged := healthEvaluationObservation("unmanaged", HealthDomainRuntime, HealthCodeRuntimeUnknown, 10)
	for _, observation := range []HealthObservation{first, unmanaged} {
		if _, err := repository.ObserveHealthEvent(t.Context(), observation); err != nil {
			t.Fatalf("ObserveHealthEvent(%s) error = %v", observation.EventID, err)
		}
	}
	second := healthEvaluationObservation("health-evaluator-managed-second", HealthDomainStore, HealthCodeStoreDiskLow, 20)
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		Observations:  []HealthObservation{second},
		ManagedEvents: healthEvaluationManagedEvents(first, second), EvaluatedAtMS: 20,
	}); err != nil {
		t.Fatalf("ApplyHealthEvaluationBatch() error = %v", err)
	}
	resolved, err := repository.HealthEvent(t.Context(), first.EventID)
	if err != nil || resolved.ResolvedAtMS == nil || *resolved.ResolvedAtMS != 20 {
		t.Fatalf("managed first = %#v, %v", resolved, err)
	}
	active, err := repository.HealthEvent(t.Context(), second.EventID)
	if err != nil || active.ResolvedAtMS != nil || active.OccurrenceCount != 1 {
		t.Fatalf("managed second = %#v, %v", active, err)
	}
	leftAlone, err := repository.HealthEvent(t.Context(), unmanaged.EventID)
	if err != nil || leftAlone.ResolvedAtMS != nil {
		t.Fatalf("unmanaged = %#v, %v", leftAlone, err)
	}

	want := errors.New("injected batch failure")
	repository.healthEvaluationWriteHook = func(stage string) error {
		if stage == "observed" {
			return want
		}
		return nil
	}
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		Observations: []HealthObservation{
			healthEvaluationObservation(second.EventID, HealthDomainStore, HealthCodeStoreDiskLow, 30),
		},
		ManagedEvents: healthEvaluationManagedEvents(first, second), EvaluatedAtMS: 30,
	}); !errors.Is(err, want) {
		t.Fatalf("ApplyHealthEvaluationBatch(failure) error = %v", err)
	}
	afterRollback, err := repository.HealthEvent(t.Context(), second.EventID)
	if err != nil || afterRollback.LastSeenAtMS != 20 || afterRollback.OccurrenceCount != 1 {
		t.Fatalf("batch rollback event = %#v, %v", afterRollback, err)
	}
}

func TestApplyHealthEvaluationBatchRejectsStaleOrUnmanagedObservations(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	observation := healthEvaluationObservation("health-evaluator-managed", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 20)
	mismatched := observation
	mismatched.Domain = HealthDomainStore
	mismatched.Code = HealthCodeStoreDiskLow
	nonReserved := healthEvaluationObservation("external-managed", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 20)
	for name, batch := range map[string]HealthEvaluationBatch{
		"observation missing from managed IDs":  {Observations: []HealthObservation{observation}, EvaluatedAtMS: 20},
		"observation time mismatch":             {Observations: []HealthObservation{observation}, ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 21},
		"observation ownership mismatch":        {Observations: []HealthObservation{mismatched}, ManagedEvents: healthEvaluationManagedEvents(observation), EvaluatedAtMS: 20},
		"duplicate managed ID":                  {ManagedEvents: healthEvaluationManagedEvents(observation, observation), EvaluatedAtMS: 20},
		"managed ID outside reserved namespace": {ManagedEvents: healthEvaluationManagedEvents(nonReserved), EvaluatedAtMS: 20},
	} {
		t.Run(name, func(t *testing.T) {
			if err := repository.ApplyHealthEvaluationBatch(t.Context(), batch); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("ApplyHealthEvaluationBatch() error = %v", err)
			}
		})
	}
}

func TestHealthEvaluationOwnershipKeepsPrefixCollisionsVisibleAndRejectsExactConflicts(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	owned := healthEvaluationObservation(
		"health-evaluator-owned", HealthDomainRuntime, HealthCodeRuntimeCPUPressure, 20,
	)
	prefixCollision := healthEvaluationObservation(
		"health-evaluator-external-prefix", HealthDomainStore, HealthCodeStoreDiskFull, 20,
	)
	if _, err := repository.ObserveHealthEvent(t.Context(), prefixCollision); err != nil {
		t.Fatalf("ObserveHealthEvent(prefix collision) error = %v", err)
	}
	snapshot, err := repository.HealthEvaluationSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	}, healthEvaluationManagedEvents(owned))
	if err != nil || len(snapshot.ActiveHealth) != 1 ||
		snapshot.ActiveHealth[0].Code != HealthCodeStoreDiskFull {
		t.Fatalf("prefix collision snapshot = %#v, %v", snapshot, err)
	}

	exactConflict := healthEvaluationObservation(
		owned.EventID, HealthDomainStore, HealthCodeStoreDiskFull, 30,
	)
	exactConflict.Fingerprint = SHA256DigestOf([]byte("external-exact-conflict"))
	if _, err := repository.ObserveHealthEvent(t.Context(), exactConflict); err != nil {
		t.Fatalf("ObserveHealthEvent(exact conflict) error = %v", err)
	}
	if _, err := repository.HealthEvaluationSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	}, healthEvaluationManagedEvents(owned)); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("HealthEvaluationSnapshot(exact conflict) error = %v", err)
	}
	if err := repository.ApplyHealthEvaluationBatch(t.Context(), HealthEvaluationBatch{
		ManagedEvents: healthEvaluationManagedEvents(owned), EvaluatedAtMS: 40,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ApplyHealthEvaluationBatch(exact conflict) error = %v", err)
	}
	stored, err := repository.HealthEvent(t.Context(), exactConflict.EventID)
	if err != nil || stored.ResolvedAtMS != nil {
		t.Fatalf("exact conflict was resolved: %#v, %v", stored, err)
	}
}

func healthEvaluationObservation(eventID string, domain HealthDomain, code HealthCode, atMS int64) HealthObservation {
	return HealthObservation{
		EventID: eventID, Fingerprint: SHA256DigestOf([]byte(eventID)), Domain: domain,
		Severity: HealthWarning, Code: code, ObservedAtMS: atMS,
	}
}

func healthEvaluationManagedEvents(observations ...HealthObservation) []HealthManagedEvent {
	result := make([]HealthManagedEvent, len(observations))
	for index, observation := range observations {
		result[index] = HealthManagedEvent{
			EventID: observation.EventID, Fingerprint: observation.Fingerprint,
			Domain: observation.Domain, Code: observation.Code,
		}
	}
	return result
}
