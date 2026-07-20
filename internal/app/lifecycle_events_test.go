package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestLifecycleEventAdapterQueuesFiniteNotificationsAndCloses(t *testing.T) {
	coordinator := &fakeSystemLifecycleCoordinator{release: make(chan struct{})}
	wakeObserved := make(chan struct{}, 1)
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Coordinator: coordinator, EventTimeout: time.Second,
		DidWake: func(context.Context) error { wakeObserved <- struct{}{}; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"system_will_sleep", "system_did_wake", "application_did_become_active"} {
		if err := adapter.NotifyLifecycle(t.Context(), event); err != nil {
			t.Fatalf("NotifyLifecycle(%q) error = %v", event, err)
		}
	}
	close(coordinator.release)
	eventually(t, func() bool {
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		return reflect.DeepEqual(coordinator.calls, []string{"sleep", "wake", "source"})
	})
	select {
	case <-wakeObserved:
	case <-time.After(time.Second):
		t.Fatal("wake callback was not invoked")
	}
	if err := adapter.NotifyLifecycle(t.Context(), "private-event"); !errors.Is(err, ErrInvalidLifecycleEventAdapter) {
		t.Fatalf("invalid event error = %v", err)
	}
	if err := adapter.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.NotifyLifecycle(t.Context(), "system_will_sleep"); !errors.Is(err, ErrInvalidLifecycleEventAdapter) {
		t.Fatalf("notification after close error = %v", err)
	}
}

func TestLifecycleEventAdapterReportsCoordinatorFailure(t *testing.T) {
	want := errors.New("wake reconcile failed")
	coordinator := &fakeSystemLifecycleCoordinator{err: want}
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Coordinator: coordinator, EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })
	if err := adapter.NotifyLifecycle(t.Context(), "system_did_wake"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-adapter.Errors():
		if !errors.Is(got, want) {
			t.Fatalf("Errors() = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not report coordinator failure")
	}
}

func eventually(t testing.TB, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied")
		}
		time.Sleep(time.Millisecond)
	}
}

type fakeSystemLifecycleCoordinator struct {
	mu      sync.Mutex
	calls   []string
	release chan struct{}
	err     error
}

type recordingQueryInvalidationNotifier struct {
	mu      sync.Mutex
	domains []QueryInvalidationDomain
}

func (notifier *recordingQueryInvalidationNotifier) Notify(
	ctx context.Context,
	domain QueryInvalidationDomain,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	notifier.mu.Lock()
	notifier.domains = append(notifier.domains, domain)
	notifier.mu.Unlock()
	return nil
}

func (notifier *recordingQueryInvalidationNotifier) count(domain QueryInvalidationDomain) int {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	count := 0
	for _, got := range notifier.domains {
		if got == domain {
			count++
		}
	}
	return count
}

func (notifier *recordingQueryInvalidationNotifier) reset() {
	notifier.mu.Lock()
	notifier.domains = nil
	notifier.mu.Unlock()
}

func (coordinator *fakeSystemLifecycleCoordinator) SystemWillSleep(
	ctx context.Context,
	_ string,
) (store.SchedulerLifecycle, error) {
	return coordinator.record(ctx, "sleep")
}

func (coordinator *fakeSystemLifecycleCoordinator) SystemDidWake(
	ctx context.Context,
	_ string,
) (store.SchedulerLifecycle, error) {
	return coordinator.record(ctx, "wake")
}

func (coordinator *fakeSystemLifecycleCoordinator) SourceChanged(
	ctx context.Context,
	_ string,
	available bool,
) (store.SchedulerLifecycle, error) {
	if !available {
		return store.SchedulerLifecycle{}, errors.New("source check must probe availability")
	}
	return coordinator.record(ctx, "source")
}

func (coordinator *fakeSystemLifecycleCoordinator) record(
	ctx context.Context,
	kind string,
) (store.SchedulerLifecycle, error) {
	if coordinator.release != nil {
		select {
		case <-ctx.Done():
			return store.SchedulerLifecycle{}, ctx.Err()
		case <-coordinator.release:
		}
	}
	coordinator.mu.Lock()
	coordinator.calls = append(coordinator.calls, kind)
	coordinator.mu.Unlock()
	return store.SchedulerLifecycle{}, coordinator.err
}
