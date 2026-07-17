package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func TestBindingHealthProjectionMapsAuthoritativeFiniteSnapshot(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	projection := healthyBindingProjection()
	projection.Result.Components[4] = healthmodel.ComponentStatus{
		Component: healthmodel.ComponentStorage, Level: healthmodel.LevelBlocked,
		Evidence: healthmodel.EvidenceKnown, Reason: healthmodel.ReasonStoreDiskFull,
		Impact: healthmodel.ImpactStorageAtRisk, Protection: healthmodel.ProtectionWritesStopped,
		RecoveryAction: healthmodel.RecoveryFreeSpace,
	}
	projection.Result.Level = healthmodel.LevelBlocked
	primary := projection.Result.Components[4]
	projection.Result.Primary = &primary
	reader := &healthProjectionStub{value: projection}
	if err := service.bindHealthProjection(reader); err != nil {
		t.Fatalf("bindHealthProjection() error = %v", err)
	}

	response, err := service.HealthProjection(t.Context())
	if err != nil || !response.HasValue || response.Stale || response.Level == nil ||
		*response.Level != HealthProjectionBlocked || response.Primary == nil ||
		response.Primary.Component != "storage" || response.Primary.RecoveryAction != "free_space" ||
		len(response.Components) != 7 || response.EvaluatedAtMS.Value == nil {
		t.Fatalf("HealthProjection() = %#v, %v", response, err)
	}
	content, marshalErr := json.Marshal(response)
	if marshalErr != nil || strings.Contains(string(content), "event") || strings.Contains(string(content), "path") {
		t.Fatalf("projection leaked non-contract data: %s, %v", content, marshalErr)
	}
}

func TestBindingHealthProjectionPreservesUnknownAndLastTrustedStale(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	reader := &healthProjectionStub{value: healthmodel.Projection{Stale: true, Failure: healthmodel.FailureSnapshot}}
	if err := service.bindHealthProjection(reader); err != nil {
		t.Fatalf("bindHealthProjection() error = %v", err)
	}
	unknown, err := service.HealthProjection(t.Context())
	if err != nil || unknown.HasValue || !unknown.Stale || unknown.Level != nil ||
		unknown.EvaluatedAtMS.UnknownReason == nil || *unknown.EvaluatedAtMS.UnknownReason != basequery.UnknownNeverLoaded {
		t.Fatalf("unknown HealthProjection() = %#v, %v", unknown, err)
	}

	reader.value = healthyBindingProjection()
	reader.value.Stale = true
	reader.value.Failure = healthmodel.FailurePersist
	stale, err := service.HealthProjection(t.Context())
	if err != nil || !stale.HasValue || !stale.Stale || stale.Failure != HealthProjectionFailurePersist ||
		stale.Level == nil || *stale.Level != HealthProjectionHealthy {
		t.Fatalf("stale HealthProjection() = %#v, %v", stale, err)
	}
}

func TestBindingHealthProjectionFailsClosedForMissingOrInvalidReader(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	if _, err := service.HealthProjection(t.Context()); err == nil {
		t.Fatal("HealthProjection() without reader error = nil")
	}
	reader := &healthProjectionStub{value: healthyBindingProjection()}
	reader.value.Result.Components = reader.value.Result.Components[:6]
	if err := service.bindHealthProjection(reader); err != nil {
		t.Fatalf("bindHealthProjection() error = %v", err)
	}
	if _, err := service.HealthProjection(t.Context()); err == nil {
		t.Fatal("HealthProjection(invalid) error = nil")
	}
	if err := service.bindHealthProjection(&healthProjectionStub{}); err == nil {
		t.Fatal("bindHealthProjection(duplicate) error = nil")
	}
}

func TestBindingHealthProjectionRejectsInconsistentStateAndPrimary(t *testing.T) {
	tests := []struct {
		name  string
		value healthmodel.Projection
	}{
		{name: "empty without stale failure", value: healthmodel.Projection{}},
		{name: "fresh with failure", value: func() healthmodel.Projection {
			value := healthyBindingProjection()
			value.Failure = healthmodel.FailurePersist
			return value
		}()},
		{name: "stale without failure", value: func() healthmodel.Projection {
			value := healthyBindingProjection()
			value.Stale = true
			return value
		}()},
		{name: "overall level mismatch", value: func() healthmodel.Projection {
			value := healthyBindingProjection()
			value.Result.Level = healthmodel.LevelBusy
			return value
		}()},
		{name: "primary is not selected component", value: func() healthmodel.Projection {
			value := healthyBindingProjection()
			value.Result.Components[1].Level = healthmodel.LevelDegraded
			value.Result.Components[1].Reason = healthmodel.ReasonLiveQueueStalled
			value.Result.Components[1].Impact = healthmodel.ImpactLiveDataDelayed
			value.Result.Components[1].Protection = healthmodel.ProtectionRetryBackoff
			value.Result.Components[1].RecoveryAction = healthmodel.RecoveryRetry
			value.Result.Level = healthmodel.LevelDegraded
			wrong := value.Result.Components[2]
			wrong.Level = healthmodel.LevelDegraded
			value.Result.Primary = &wrong
			return value
		}()},
		{name: "component order mismatch", value: func() healthmodel.Projection {
			value := healthyBindingProjection()
			value.Result.Components[0], value.Result.Components[1] = value.Result.Components[1], value.Result.Components[0]
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mapHealthProjection(test.value); err == nil {
				t.Fatalf("mapHealthProjection(%s) error = nil", test.name)
			}
		})
	}
}

func TestBindingHealthProjectionContainsPanicAndHonorsCancellation(t *testing.T) {
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	reader := &healthProjectionStub{value: healthyBindingProjection(), panicValue: "private panic"}
	if err := service.bindHealthProjection(reader); err != nil {
		t.Fatalf("bindHealthProjection() error = %v", err)
	}
	if _, err := service.HealthProjection(t.Context()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("HealthProjection(panic) error = %v", err)
	}
	reader.panicValue = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.HealthProjection(ctx); err == nil {
		t.Fatal("HealthProjection(cancelled) error = nil")
	}
}

type healthProjectionStub struct {
	value      healthmodel.Projection
	panicValue any
}

func (stub *healthProjectionStub) Projection() healthmodel.Projection {
	if stub.panicValue != nil {
		panic(stub.panicValue)
	}
	return stub.value
}

func healthyBindingProjection() healthmodel.Projection {
	components := []healthmodel.Component{
		healthmodel.ComponentLocalIndex, healthmodel.ComponentLiveQueue,
		healthmodel.ComponentHistoryBackfill, healthmodel.ComponentOnlineQuota,
		healthmodel.ComponentStorage, healthmodel.ComponentRuntime, healthmodel.ComponentUpdater,
	}
	statuses := make([]healthmodel.ComponentStatus, 0, len(components))
	for _, component := range components {
		statuses = append(statuses, healthmodel.ComponentStatus{
			Component: component, Level: healthmodel.LevelHealthy, Evidence: healthmodel.EvidenceKnown,
			Reason: healthmodel.ReasonHealthy, Impact: healthmodel.ImpactNone,
			Protection: healthmodel.ProtectionNone, RecoveryAction: healthmodel.RecoveryNone,
		})
	}
	return healthmodel.Projection{
		HasValue: true, Failure: healthmodel.FailureNone, EvaluatedAtMS: 1_784_100_000_000,
		Result: healthmodel.Result{Level: healthmodel.LevelHealthy, Components: statuses},
	}
}
