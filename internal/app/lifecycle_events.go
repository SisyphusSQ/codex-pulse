package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidLifecycleEventAdapter = errors.New("invalid lifecycle event adapter")

type systemLifecycleCoordinator interface {
	SystemWillSleep(context.Context, string) (store.SchedulerLifecycle, error)
	SystemDidWake(context.Context, string) (store.SchedulerLifecycle, error)
	SourceChanged(context.Context, string, bool) (store.SchedulerLifecycle, error)
}

type LifecycleEventAdapterConfig struct {
	Coordinator  systemLifecycleCoordinator
	EventTimeout time.Duration
	NewEventID   func(string) string
	DidWake      func(context.Context) error
}

type systemLifecycleEvent uint8

const (
	systemWillSleep systemLifecycleEvent = iota + 1
	systemDidWake
	sourceAvailabilityCheck
)

// LifecycleEventAdapter serializes finite lifecycle notifications from the
// native client. NotifyLifecycle only enqueues an enum and performs no I/O.
type LifecycleEventAdapter struct {
	coordinator  systemLifecycleCoordinator
	eventTimeout time.Duration
	newEventID   func(string) string
	didWake      func(context.Context) error

	mu        sync.Mutex
	queue     []systemLifecycleEvent
	accepting bool
	closing   bool
	signal    chan struct{}
	done      chan struct{}
	errors    chan error
	closeOnce sync.Once
}

func NewLifecycleEventAdapter(config LifecycleEventAdapterConfig) (*LifecycleEventAdapter, error) {
	if config.Coordinator == nil || config.EventTimeout < 0 {
		return nil, ErrInvalidLifecycleEventAdapter
	}
	if config.EventTimeout == 0 {
		config.EventTimeout = 10 * time.Second
	}
	if config.NewEventID == nil {
		var sequence atomic.Uint64
		config.NewEventID = func(kind string) string {
			return fmt.Sprintf("%s:%d:%d", kind, time.Now().UnixMilli(), sequence.Add(1))
		}
	}
	adapter := &LifecycleEventAdapter{
		coordinator: config.Coordinator, eventTimeout: config.EventTimeout,
		newEventID: config.NewEventID, didWake: config.DidWake, accepting: true,
		signal: make(chan struct{}, 1), done: make(chan struct{}), errors: make(chan error, 16),
	}
	go adapter.run()
	return adapter, nil
}

func (adapter *LifecycleEventAdapter) NotifyLifecycle(ctx context.Context, name string) error {
	if adapter == nil || ctx == nil {
		return ErrInvalidLifecycleEventAdapter
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var event systemLifecycleEvent
	switch name {
	case "system_will_sleep":
		event = systemWillSleep
	case "system_did_wake":
		event = systemDidWake
	case "application_did_become_active":
		event = sourceAvailabilityCheck
	default:
		return ErrInvalidLifecycleEventAdapter
	}
	return adapter.enqueue(event)
}

func (adapter *LifecycleEventAdapter) Errors() <-chan error {
	if adapter == nil {
		return nil
	}
	return adapter.errors
}

func (adapter *LifecycleEventAdapter) Close(ctx context.Context) error {
	if adapter == nil || ctx == nil {
		return ErrInvalidLifecycleEventAdapter
	}
	adapter.closeOnce.Do(func() {
		adapter.mu.Lock()
		adapter.accepting = false
		adapter.closing = true
		adapter.mu.Unlock()
		adapter.notify()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-adapter.done:
		return nil
	}
}

func (adapter *LifecycleEventAdapter) enqueue(event systemLifecycleEvent) error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if !adapter.accepting {
		return ErrInvalidLifecycleEventAdapter
	}
	if len(adapter.queue) == 0 || adapter.queue[len(adapter.queue)-1] != event {
		adapter.queue = append(adapter.queue, event)
	}
	adapter.notify()
	return nil
}

func (adapter *LifecycleEventAdapter) notify() {
	select {
	case adapter.signal <- struct{}{}:
	default:
	}
}

func (adapter *LifecycleEventAdapter) run() {
	defer close(adapter.done)
	defer close(adapter.errors)
	for {
		<-adapter.signal
		for {
			adapter.mu.Lock()
			if len(adapter.queue) == 0 {
				closing := adapter.closing
				adapter.mu.Unlock()
				if closing {
					return
				}
				break
			}
			event := adapter.queue[0]
			adapter.queue = adapter.queue[1:]
			adapter.mu.Unlock()
			adapter.handle(event)
		}
	}
}

func (adapter *LifecycleEventAdapter) handle(event systemLifecycleEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), adapter.eventTimeout)
	var err error
	switch event {
	case systemWillSleep:
		_, err = adapter.coordinator.SystemWillSleep(ctx, adapter.newEventID("system-sleep"))
	case systemDidWake:
		_, err = adapter.coordinator.SystemDidWake(ctx, adapter.newEventID("system-wake"))
		cancel()
		if adapter.didWake != nil {
			wakeCtx, wakeCancel := context.WithTimeout(context.Background(), adapter.eventTimeout)
			err = errors.Join(err, adapter.didWake(wakeCtx))
			wakeCancel()
		}
	case sourceAvailabilityCheck:
		_, err = adapter.coordinator.SourceChanged(ctx, adapter.newEventID("source-check"), true)
	default:
		err = ErrInvalidLifecycleEventAdapter
	}
	cancel()
	if err != nil {
		select {
		case adapter.errors <- err:
		default:
		}
	}
}
