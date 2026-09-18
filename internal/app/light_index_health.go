package app

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const lightIndexFailureEventID = "light-index-token-scan-failed"

// A failed refresh remains visible until a complete scan pass succeeds.
// Only stable, content-free values are persisted; raw source errors can contain paths.
type lightIndexHealthObserver struct {
	repository *store.Repository
}

func (observer lightIndexHealthObserver) failed(err error) {
	if err == nil || observer.repository == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observedAtMS := time.Now().UnixMilli()
	previous, readErr := observer.repository.RuntimeHealth(ctx, lightIndexFailureEventID)
	if readErr == nil {
		observedAtMS = max(observedAtMS, previous.UpdatedAtMS+1)
	} else if !errors.Is(readErr, store.ErrNotFound) {
		log.Printf("light_index health_event=read_failed error_type=%T", readErr)
		return
	}
	_, writeErr := observer.repository.ObserveHealthEvent(ctx, store.HealthObservation{
		EventID:      lightIndexFailureEventID,
		Fingerprint:  store.SHA256DigestOf([]byte(lightIndexFailureEventID)),
		Domain:       store.HealthDomainSource,
		Severity:     store.HealthError,
		Code:         store.HealthCodeSourceUnavailable,
		ObservedAtMS: observedAtMS,
	})
	if writeErr != nil {
		log.Printf("light_index health_event=observe_failed error_type=%T", writeErr)
	}
}

func (observer lightIndexHealthObserver) observed(refresh lightindex.RefreshObservation) {
	if observer.repository == nil || refresh.Phase != "scan" || !refresh.Complete || refresh.Failed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := observer.repository.RuntimeHealth(ctx, lightIndexFailureEventID)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		log.Printf("light_index health_event=read_failed error_type=%T", err)
		return
	}
	if event.ResolvedAtMS != nil {
		return
	}
	resolvedAtMS := max(time.Now().UnixMilli(), event.LastSeenAtMS)
	if err := observer.repository.ResolveHealthEvent(ctx, lightIndexFailureEventID, resolvedAtMS); err != nil {
		log.Printf("light_index health_event=resolve_failed error_type=%T", err)
	}
}
