package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQueryInvalidationPublisherEmitsOnlyFiniteContentFreePayload(t *testing.T) {
	t.Parallel()

	emitter := &recordingQueryInvalidationEmitter{}
	publisher, err := newQueryInvalidationPublisher(QueryInvalidationPublisherConfig{
		Emitter: emitter,
		Clock:   func() time.Time { return time.UnixMilli(1_784_100_000_000) },
	})
	if err != nil {
		t.Fatalf("newQueryInvalidationPublisher() error = %v", err)
	}
	for _, domain := range []QueryInvalidationDomain{
		QueryInvalidationIndex,
		QueryInvalidationQuota,
		QueryInvalidationHealth,
		QueryInvalidationSettings,
	} {
		if err := publisher.Notify(context.Background(), domain); err != nil {
			t.Fatalf("Notify(%q) error = %v", domain, err)
		}
	}
	if len(emitter.events) != 4 {
		t.Fatalf("emitted events = %#v", emitter.events)
	}
	for index, emitted := range emitter.events {
		if emitted.name != QueryInvalidationEventName || len(emitted.data) != 1 {
			t.Fatalf("event[%d] = %#v", index, emitted)
		}
		payload, ok := emitted.data[0].(QueryInvalidationEvent)
		if !ok || payload.Version != QueryInvalidationContractVersion ||
			payload.Domain == "" {
			t.Fatalf("event[%d] payload = %#v", index, emitted.data[0])
		}
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			t.Fatalf("json.Marshal(payload) error = %v", marshalErr)
		}
		var object map[string]any
		if unmarshalErr := json.Unmarshal(encoded, &object); unmarshalErr != nil {
			t.Fatalf("json.Unmarshal(payload) error = %v", unmarshalErr)
		}
		if !reflect.DeepEqual(sortedMapKeys(object), []string{"domain", "version"}) {
			t.Fatalf("payload keys = %#v", sortedMapKeys(object))
		}
	}
	if err := publisher.Notify(context.Background(), QueryInvalidationDomain("session-id")); !errors.Is(err, ErrQueryInvalidation) {
		t.Fatalf("Notify(invalid) error = %v, want ErrQueryInvalidation", err)
	}
	if len(emitter.events) != 4 {
		t.Fatalf("invalid domain emitted event: %#v", emitter.events)
	}
}

func TestQueryInvalidationPublisherRecordsSerializationFailureWithoutEmit(t *testing.T) {
	t.Parallel()

	serializeErr := errors.New("synthetic serializer marker must not be persisted")
	emitter := &recordingQueryInvalidationEmitter{}
	health := &recordingQueryInvalidationHealthWriter{}
	publisher, err := newQueryInvalidationPublisher(QueryInvalidationPublisherConfig{
		Emitter: emitter,
		Health:  health,
		Clock:   func() time.Time { return time.UnixMilli(1_784_100_000_001) },
		Marshal: func(any) ([]byte, error) { return nil, serializeErr },
	})
	if err != nil {
		t.Fatalf("newQueryInvalidationPublisher() error = %v", err)
	}
	err = publisher.Notify(context.Background(), QueryInvalidationQuota)
	if !errors.Is(err, ErrQueryInvalidationSerialization) || !errors.Is(err, serializeErr) {
		t.Fatalf("Notify() error = %v", err)
	}
	if len(emitter.events) != 0 {
		t.Fatalf("events = %#v, want none", emitter.events)
	}
	if len(health.observations) != 1 {
		t.Fatalf("health observations = %#v", health.observations)
	}
	observation := health.observations[0]
	if observation.EventID != QueryInvalidationSerializationHealthEventID ||
		observation.Domain != store.HealthDomainRuntime ||
		observation.Code != store.HealthCodeRuntimeUnknown ||
		observation.Severity != store.HealthWarning || observation.ErrorClass == nil ||
		*observation.ErrorClass != store.RuntimeErrorUnknown ||
		observation.ObservedAtMS != 1_784_100_000_001 ||
		observation.Fingerprint.String() == "" {
		t.Fatalf("health observation = %#v", observation)
	}
	encoded, marshalErr := json.Marshal(observation)
	if marshalErr != nil {
		t.Fatalf("json.Marshal(observation) error = %v", marshalErr)
	}
	if string(encoded) == "" || containsString(string(encoded), "synthetic serializer marker") {
		t.Fatalf("health observation leaked serializer error: %s", encoded)
	}
}

func TestQueryInvalidationPublisherContainsEmitterPanicAndRecordsHealth(t *testing.T) {
	t.Parallel()

	health := &recordingQueryInvalidationHealthWriter{}
	publisher, err := newQueryInvalidationPublisher(QueryInvalidationPublisherConfig{
		Emitter: panickingQueryInvalidationEmitter{},
		Health:  health,
		Clock:   func() time.Time { return time.UnixMilli(1_784_100_000_002) },
	})
	if err != nil {
		t.Fatalf("newQueryInvalidationPublisher() error = %v", err)
	}
	err = publisher.Notify(context.Background(), QueryInvalidationHealth)
	if !errors.Is(err, ErrQueryInvalidationEmission) {
		t.Fatalf("Notify() error = %v, want ErrQueryInvalidationEmission", err)
	}
	if len(health.observations) != 1 ||
		health.observations[0].EventID != QueryInvalidationEmissionHealthEventID {
		t.Fatalf("health observations = %#v", health.observations)
	}
}

type emittedQueryInvalidation struct {
	name string
	data []any
}

type recordingQueryInvalidationEmitter struct {
	events []emittedQueryInvalidation
}

type panickingQueryInvalidationEmitter struct{}

func (panickingQueryInvalidationEmitter) Emit(string, ...any) bool {
	panic("synthetic emitter panic")
}

func (emitter *recordingQueryInvalidationEmitter) Emit(name string, data ...any) bool {
	emitter.events = append(emitter.events, emittedQueryInvalidation{name: name, data: data})
	return false
}

type recordingQueryInvalidationHealthWriter struct {
	observations []store.HealthObservation
}

type recordingQueryInvalidationNotifier struct {
	mu      sync.Mutex
	domains []QueryInvalidationDomain
}

func (notifier *recordingQueryInvalidationNotifier) Notify(
	_ context.Context,
	domain QueryInvalidationDomain,
) error {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	notifier.domains = append(notifier.domains, domain)
	return nil
}

func (notifier *recordingQueryInvalidationNotifier) reset() {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	notifier.domains = nil
}

func (notifier *recordingQueryInvalidationNotifier) count(
	domain QueryInvalidationDomain,
) int {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	count := 0
	for _, recorded := range notifier.domains {
		if recorded == domain {
			count++
		}
	}
	return count
}

func (writer *recordingQueryInvalidationHealthWriter) ObserveHealthEvent(
	_ context.Context,
	observation store.HealthObservation,
) (store.HealthEvent, error) {
	writer.observations = append(writer.observations, observation)
	return store.HealthEvent{}, nil
}

func sortedMapKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	if len(keys) == 2 && keys[0] > keys[1] {
		keys[0], keys[1] = keys[1], keys[0]
	}
	return keys
}

func containsString(value, marker string) bool {
	for index := 0; index+len(marker) <= len(value); index++ {
		if value[index:index+len(marker)] == marker {
			return true
		}
	}
	return false
}
