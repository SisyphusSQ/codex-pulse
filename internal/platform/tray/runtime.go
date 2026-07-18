package tray

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"
)

var ErrTrayRuntime = errors.New("tray runtime is invalid")

type SnapshotReader interface {
	Read(context.Context, int64) (Snapshot, error)
}

type StatusRenderer interface {
	Update(StatusViewModel) error
	Close() error
}

type RuntimeConfig struct {
	Reader                 SnapshotReader
	Renderer               StatusRenderer
	TickInterval           time.Duration
	MinimumRefreshInterval time.Duration
	Now                    func() time.Time
}

type Runtime struct {
	cancel     context.CancelFunc
	invalidate chan struct{}
	done       chan struct{}
	renderer   StatusRenderer

	errorMu       sync.Mutex
	errors        []error
	shutdown      sync.Once
	closeRenderer sync.Once
	closeErr      error
}

func StartRuntime(parent context.Context, config RuntimeConfig) (*Runtime, error) {
	if config.Reader == nil || config.Renderer == nil || config.TickInterval <= 0 ||
		config.MinimumRefreshInterval <= 0 || config.MinimumRefreshInterval > config.TickInterval {
		return nil, ErrTrayRuntime
	}
	if parent == nil {
		parent = context.Background()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	ctx, cancel := context.WithCancel(parent)
	runtime := &Runtime{
		cancel: cancel, invalidate: make(chan struct{}, 1), done: make(chan struct{}),
		renderer: config.Renderer,
	}
	go runtime.run(ctx, config)
	return runtime, nil
}

func (runtime *Runtime) Invalidate() {
	if runtime == nil {
		return
	}
	select {
	case runtime.invalidate <- struct{}{}:
	default:
	}
}

func (runtime *Runtime) run(ctx context.Context, config RuntimeConfig) {
	defer close(runtime.done)
	ticker := time.NewTicker(config.TickInterval)
	defer ticker.Stop()

	var lastModel *StatusViewModel
	projector := NewProjector()
	refresh := func() {
		now := config.Now()
		snapshot, err := config.Reader.Read(ctx, now.UnixMilli())
		if err != nil {
			snapshot.ReadError = err
		}
		model := projector.Project(snapshot)
		if lastModel != nil && reflect.DeepEqual(*lastModel, model) {
			return
		}
		if err := runtime.renderer.Update(model); err != nil {
			runtime.recordError(err)
			return
		}
		copyOfModel := model
		lastModel = &copyOfModel
	}

	refresh()
	lastRefresh := config.Now()
	var delayed *time.Timer
	var delayedC <-chan time.Time
	stopDelayed := func() {
		if delayed != nil && !delayed.Stop() {
			select {
			case <-delayed.C:
			default:
			}
		}
		delayed = nil
		delayedC = nil
	}
	defer stopDelayed()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stopDelayed()
			refresh()
			lastRefresh = config.Now()
		case <-runtime.invalidate:
			if delayedC != nil {
				continue
			}
			remaining := config.MinimumRefreshInterval - config.Now().Sub(lastRefresh)
			if remaining <= 0 {
				refresh()
				lastRefresh = config.Now()
				continue
			}
			delayed = time.NewTimer(remaining)
			delayedC = delayed.C
		case <-delayedC:
			delayed = nil
			delayedC = nil
			refresh()
			lastRefresh = config.Now()
		}
	}
}

func (runtime *Runtime) recordError(err error) {
	if err == nil {
		return
	}
	runtime.errorMu.Lock()
	defer runtime.errorMu.Unlock()
	runtime.errors = append(runtime.errors, err)
}

func (runtime *Runtime) Close(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.shutdown.Do(func() {
		runtime.cancel()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.done:
		runtime.closeRenderer.Do(func() {
			// Run native teardown on the successful caller. In production this
			// is the AppKit main thread after App.Run returns; moving it to a
			// background goroutine can deadlock on dispatch_sync(main).
			runtime.recordError(runtime.renderer.Close())
			runtime.errorMu.Lock()
			runtime.closeErr = errors.Join(runtime.errors...)
			runtime.errorMu.Unlock()
		})
		runtime.errorMu.Lock()
		defer runtime.errorMu.Unlock()
		return runtime.closeErr
	}
}
