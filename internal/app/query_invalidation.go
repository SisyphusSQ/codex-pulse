package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	QueryInvalidationEventName = "codex-pulse:query-invalidated"

	QueryInvalidationSerializationHealthEventID = "query-invalidation-serialization"
	QueryInvalidationEmissionHealthEventID      = "query-invalidation-emission"
	queryInvalidationHealthTimeout              = 5 * time.Second
)

var (
	ErrQueryInvalidation              = errors.New("query invalidation is invalid")
	ErrQueryInvalidationSerialization = errors.New("query invalidation serialization failed")
	ErrQueryInvalidationEmission      = errors.New("query invalidation emission failed")
)

type QueryInvalidationDomain string

type QueryInvalidationVersion string

const (
	QueryInvalidationContractVersion QueryInvalidationVersion = "query-invalidation-v1"

	QueryInvalidationIndex    QueryInvalidationDomain = "index"
	QueryInvalidationQuota    QueryInvalidationDomain = "quota"
	QueryInvalidationHealth   QueryInvalidationDomain = "health"
	QueryInvalidationSettings QueryInvalidationDomain = "settings"
)

// QueryInvalidationEvent is a best-effort cache hint. It deliberately carries
// no business fact; the frontend must refetch the authoritative Go query.
type QueryInvalidationEvent struct {
	Version QueryInvalidationVersion `json:"version"`
	Domain  QueryInvalidationDomain  `json:"domain"`
}

func init() {
	application.RegisterEvent[QueryInvalidationEvent](QueryInvalidationEventName)
}

type queryInvalidationEmitter interface {
	Emit(string, ...any) bool
}

type queryInvalidationHealthWriter interface {
	ObserveHealthEvent(context.Context, store.HealthObservation) (store.HealthEvent, error)
}

type queryInvalidationNotifier interface {
	Notify(context.Context, QueryInvalidationDomain) error
}

type QueryInvalidationPublisherConfig struct {
	Emitter queryInvalidationEmitter
	Health  queryInvalidationHealthWriter
	Clock   func() time.Time
	Marshal func(any) ([]byte, error)
}

type queryInvalidationPublisher struct {
	emitter queryInvalidationEmitter
	health  queryInvalidationHealthWriter
	clock   func() time.Time
	marshal func(any) ([]byte, error)
}

func newQueryInvalidationPublisher(
	config QueryInvalidationPublisherConfig,
) (*queryInvalidationPublisher, error) {
	if config.Emitter == nil {
		return nil, ErrQueryInvalidation
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Marshal == nil {
		config.Marshal = json.Marshal
	}
	return &queryInvalidationPublisher{
		emitter: config.Emitter,
		health:  config.Health,
		clock:   config.Clock,
		marshal: config.Marshal,
	}, nil
}

func (publisher *queryInvalidationPublisher) Notify(
	ctx context.Context,
	domain QueryInvalidationDomain,
) error {
	if publisher == nil || publisher.emitter == nil || publisher.clock == nil ||
		publisher.marshal == nil || ctx == nil || !validQueryInvalidationDomain(domain) {
		return ErrQueryInvalidation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	event := QueryInvalidationEvent{
		Version: QueryInvalidationContractVersion,
		Domain:  domain,
	}
	if _, err := publisher.marshal(event); err != nil {
		healthErr := publisher.recordFailureHealth(
			ctx,
			QueryInvalidationSerializationHealthEventID,
		)
		return errors.Join(ErrQueryInvalidationSerialization, err, healthErr)
	}
	if err := emitQueryInvalidation(publisher.emitter, event); err != nil {
		healthErr := publisher.recordFailureHealth(ctx, QueryInvalidationEmissionHealthEventID)
		return errors.Join(err, healthErr)
	}
	return nil
}

func validQueryInvalidationDomain(domain QueryInvalidationDomain) bool {
	switch domain {
	case QueryInvalidationIndex, QueryInvalidationQuota,
		QueryInvalidationHealth, QueryInvalidationSettings:
		return true
	default:
		return false
	}
}

func emitQueryInvalidation(
	emitter queryInvalidationEmitter,
	event QueryInvalidationEvent,
) (returnErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			returnErr = fmt.Errorf("%w: recovered emitter panic", ErrQueryInvalidationEmission)
		}
	}()
	emitter.Emit(QueryInvalidationEventName, event)
	return nil
}

func (publisher *queryInvalidationPublisher) recordFailureHealth(
	ctx context.Context,
	eventID string,
) error {
	if publisher.health == nil {
		return nil
	}
	healthCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		queryInvalidationHealthTimeout,
	)
	defer cancel()
	class := store.RuntimeErrorUnknown
	_, err := publisher.health.ObserveHealthEvent(healthCtx, store.HealthObservation{
		EventID: eventID,
		Fingerprint: store.SHA256DigestOf(
			[]byte("runtime\x00" + eventID),
		),
		Domain:       store.HealthDomainRuntime,
		Severity:     store.HealthWarning,
		Code:         store.HealthCodeRuntimeUnknown,
		ErrorClass:   &class,
		ObservedAtMS: publisher.clock().UnixMilli(),
	})
	return err
}

func notifyQueryInvalidation(
	notifier queryInvalidationNotifier,
	ctx context.Context,
	domain QueryInvalidationDomain,
) {
	if notifier == nil || ctx == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = notifier.Notify(ctx, domain)
}
