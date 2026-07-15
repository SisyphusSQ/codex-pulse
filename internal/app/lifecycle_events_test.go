package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestLifecycleEventAdapterQueuesCallbacksAndClosesRegistrations(t *testing.T) {
	registrar := &fakeLifecycleRegistrar{callbacks: make(map[events.ApplicationEventType]func(*application.ApplicationEvent))}
	coordinator := &fakeSystemLifecycleCoordinator{release: make(chan struct{})}
	sequence := int64(0)
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Registrar: registrar, Coordinator: coordinator, EventTimeout: time.Second,
		NewEventID: func(kind string) string {
			sequence++
			return kind + ":test:" + string(rune('0'+sequence))
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleEventAdapter() error = %v", err)
	}

	sleepCallback := registrar.callbacks[events.Common.SystemWillSleep]
	wakeCallback := registrar.callbacks[events.Common.SystemDidWake]
	sourceCallback := registrar.callbacks[events.Mac.ApplicationDidBecomeActive]
	if sleepCallback == nil || wakeCallback == nil || sourceCallback == nil {
		t.Fatalf("registered callbacks = %#v", registrar.callbacks)
	}
	returned := make(chan struct{})
	go func() {
		sleepCallback(&application.ApplicationEvent{})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wails sleep callback blocked on coordinator work")
	}
	wakeCallback(&application.ApplicationEvent{})
	sourceCallback(&application.ApplicationEvent{})
	close(coordinator.release)

	deadline := time.Now().Add(time.Second)
	for {
		coordinator.mu.Lock()
		calls := append([]string(nil), coordinator.calls...)
		coordinator.mu.Unlock()
		if len(calls) == 3 {
			if !reflect.DeepEqual(calls, []string{"sleep", "wake", "source"}) {
				t.Fatalf("coordinator calls = %v", calls)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("coordinator calls = %v, want sleep,wake,source", calls)
		}
		time.Sleep(time.Millisecond)
	}
	if err := adapter.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if registrar.cancelCalls != 3 {
		t.Fatalf("cancel calls = %d, want 3", registrar.cancelCalls)
	}
	if err := adapter.Close(context.Background()); err != nil {
		t.Fatalf("Close(exact replay) error = %v", err)
	}
	registrar.trigger(events.Common.SystemWillSleep)
	time.Sleep(10 * time.Millisecond)
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if len(coordinator.calls) != 3 {
		t.Fatalf("calls after Close = %v", coordinator.calls)
	}
}

func TestLifecycleEventAdapterReportsCoordinatorFailure(t *testing.T) {
	registrar := &fakeLifecycleRegistrar{callbacks: make(map[events.ApplicationEventType]func(*application.ApplicationEvent))}
	want := errors.New("wake reconcile failed")
	coordinator := &fakeSystemLifecycleCoordinator{err: want}
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Registrar: registrar, Coordinator: coordinator, EventTimeout: time.Second,
		NewEventID: func(kind string) string { return kind + ":failure" },
	})
	if err != nil {
		t.Fatalf("NewLifecycleEventAdapter() error = %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close(context.Background()) })
	registrar.trigger(events.Common.SystemDidWake)
	select {
	case err := <-adapter.Errors():
		if !errors.Is(err, want) {
			t.Fatalf("Errors() = %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not report coordinator failure")
	}
}

type fakeLifecycleRegistrar struct {
	callbacks   map[events.ApplicationEventType]func(*application.ApplicationEvent)
	cancelCalls int
	mu          sync.Mutex
}

func (registrar *fakeLifecycleRegistrar) OnApplicationEvent(
	eventType events.ApplicationEventType,
	callback func(*application.ApplicationEvent),
) func() {
	registrar.mu.Lock()
	registrar.callbacks[eventType] = callback
	registrar.mu.Unlock()
	return func() {
		registrar.mu.Lock()
		delete(registrar.callbacks, eventType)
		registrar.cancelCalls++
		registrar.mu.Unlock()
	}
}

func (registrar *fakeLifecycleRegistrar) trigger(eventType events.ApplicationEventType) {
	registrar.mu.Lock()
	callback := registrar.callbacks[eventType]
	registrar.mu.Unlock()
	if callback != nil {
		callback(&application.ApplicationEvent{})
	}
}

type fakeSystemLifecycleCoordinator struct {
	mu      sync.Mutex
	calls   []string
	release chan struct{}
	err     error
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
