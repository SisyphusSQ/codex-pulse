package app

import (
	"context"
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/health"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestLightIndexFailureIsVisibleUntilCompleteScan(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	defer func() { _ = database.Close(context.Background()) }()
	observer := lightIndexHealthObserver{repository: repository}
	observer.failed(errors.New("source path must not be stored"))
	event, err := repository.RuntimeHealth(t.Context(), lightIndexFailureEventID)
	if err != nil || event.Code != store.HealthCodeSourceUnavailable || event.ResolvedAtMS != nil {
		t.Fatalf("failed refresh event = %#v, %v", event, err)
	}
	if descriptor, ok := health.DescribeEvent(event.Domain, event.Code); !ok ||
		descriptor.Component != health.ComponentLocalIndex || descriptor.RecoveryAction != health.RecoveryCheckSource {
		t.Fatalf("failed refresh descriptor = %#v, %t", descriptor, ok)
	}
	observer.observed(lightindex.RefreshObservation{Phase: "scan", Complete: false})
	event, err = repository.RuntimeHealth(t.Context(), lightIndexFailureEventID)
	if err != nil || event.ResolvedAtMS != nil {
		t.Fatalf("partial scan event = %#v, %v", event, err)
	}
	observer.observed(lightindex.RefreshObservation{Phase: "scan", Complete: true})
	event, err = repository.RuntimeHealth(t.Context(), lightIndexFailureEventID)
	if err != nil || event.ResolvedAtMS == nil {
		t.Fatalf("complete scan event = %#v, %v", event, err)
	}
	observer.failed(errors.New("retry failed"))
	event, err = repository.RuntimeHealth(t.Context(), lightIndexFailureEventID)
	if err != nil || event.ResolvedAtMS != nil || event.OccurrenceCount != 2 {
		t.Fatalf("reopened event = %#v, %v", event, err)
	}
}
