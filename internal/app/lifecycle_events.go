package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var ErrInvalidLifecycleEventAdapter = errors.New("invalid lifecycle event adapter")

type lifecycleEventRegistrar interface {
	OnApplicationEvent(events.ApplicationEventType, func(*application.ApplicationEvent)) func()
}

type systemLifecycleCoordinator interface {
	SystemWillSleep(context.Context, string) (store.SchedulerLifecycle, error)
	SystemDidWake(context.Context, string) (store.SchedulerLifecycle, error)
	SourceChanged(context.Context, string, bool) (store.SchedulerLifecycle, error)
}

type LifecycleEventAdapterConfig struct {
	Registrar    lifecycleEventRegistrar
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

// LifecycleEventAdapter 把Wails common event转换为串行coordinator命令。
// callback只在内存队列追加一个枚举，不执行Store、文件IO或reconcile。
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
	cancels   []func()
	closeOnce sync.Once
}

func NewLifecycleEventAdapter(config LifecycleEventAdapterConfig) (*LifecycleEventAdapter, error) {
	if config.Registrar == nil || config.Coordinator == nil || config.EventTimeout < 0 {
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
	adapter.cancels = []func(){
		config.Registrar.OnApplicationEvent(events.Common.SystemWillSleep, func(*application.ApplicationEvent) {
			adapter.enqueue(systemWillSleep)
		}),
		config.Registrar.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) {
			adapter.enqueue(systemDidWake)
		}),
		config.Registrar.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(*application.ApplicationEvent) {
			adapter.enqueue(sourceAvailabilityCheck)
		}),
	}
	if adapter.cancels[0] == nil || adapter.cancels[1] == nil || adapter.cancels[2] == nil {
		for _, cancel := range adapter.cancels {
			if cancel != nil {
				cancel()
			}
		}
		return nil, ErrInvalidLifecycleEventAdapter
	}
	go adapter.run()
	return adapter, nil
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
		for _, cancel := range adapter.cancels {
			cancel()
		}
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

func (adapter *LifecycleEventAdapter) enqueue(event systemLifecycleEvent) {
	adapter.mu.Lock()
	if !adapter.accepting {
		adapter.mu.Unlock()
		return
	}
	if len(adapter.queue) == 0 || adapter.queue[len(adapter.queue)-1] != event {
		adapter.queue = append(adapter.queue, event)
	}
	adapter.mu.Unlock()
	adapter.notify()
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
			updateCtx, updateCancel := context.WithTimeout(context.Background(), adapter.eventTimeout)
			err = errors.Join(err, adapter.didWake(updateCtx))
			updateCancel()
		}
	case sourceAvailabilityCheck:
		_, err = adapter.coordinator.SourceChanged(ctx, adapter.newEventID("source-check"), true)
	default:
		err = ErrInvalidLifecycleEventAdapter
	}
	cancel()
	if err == nil {
		return
	}
	select {
	case adapter.errors <- err:
	default:
	}
}

var _ lifecycleEventRegistrar = (*application.EventManager)(nil)
