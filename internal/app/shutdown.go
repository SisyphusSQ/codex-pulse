package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrApplicationShutdown = errors.New("application shutdown is unavailable")

type shutdownPhase string

const (
	shutdownPhaseRunning  shutdownPhase = "running"
	shutdownPhaseDraining shutdownPhase = "draining"
	shutdownPhaseClosed   shutdownPhase = "closed"
)

type shutdownSnapshot struct {
	Phase       shutdownPhase
	Stage       string
	FailedStage string
}

type shutdownComponent struct {
	Name  string
	Close func(context.Context) error
}

type applicationShutdownCoordinator struct {
	mu         sync.RWMutex
	components []shutdownComponent
	phase      shutdownPhase
	stage      string
	failed     string
	done       chan struct{}
	closeOnce  sync.Once
	closeErr   error
}

func newApplicationShutdownCoordinator(components ...shutdownComponent) (*applicationShutdownCoordinator, error) {
	if len(components) == 0 {
		return nil, ErrApplicationShutdown
	}
	for _, component := range components {
		if component.Name == "" || component.Close == nil {
			return nil, ErrApplicationShutdown
		}
	}
	return &applicationShutdownCoordinator{
		components: append([]shutdownComponent(nil), components...),
		phase:      shutdownPhaseRunning, done: make(chan struct{}),
	}, nil
}

func (coordinator *applicationShutdownCoordinator) Close(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrApplicationShutdown
	}
	coordinator.closeOnce.Do(func() {
		coordinator.mu.Lock()
		coordinator.phase = shutdownPhaseDraining
		coordinator.stage = coordinator.components[0].Name
		coordinator.mu.Unlock()
		go coordinator.shutdown()
	})
	select {
	case <-ctx.Done():
		snapshot := coordinator.Snapshot()
		return fmt.Errorf("shutdown timed out during %s: %w", snapshot.Stage, ctx.Err())
	case <-coordinator.done:
		return coordinator.closeErr
	}
}

func (coordinator *applicationShutdownCoordinator) Snapshot() shutdownSnapshot {
	if coordinator == nil {
		return shutdownSnapshot{}
	}
	coordinator.mu.RLock()
	defer coordinator.mu.RUnlock()
	return shutdownSnapshot{Phase: coordinator.phase, Stage: coordinator.stage, FailedStage: coordinator.failed}
}

func (coordinator *applicationShutdownCoordinator) shutdown() {
	var closeErr error
	for _, component := range coordinator.components {
		coordinator.mu.Lock()
		coordinator.stage = component.Name
		coordinator.mu.Unlock()
		if err := component.Close(context.Background()); err != nil {
			coordinator.mu.Lock()
			if coordinator.failed == "" {
				coordinator.failed = component.Name
			}
			coordinator.mu.Unlock()
			closeErr = errors.Join(closeErr, fmt.Errorf("close %s: %w", component.Name, err))
		}
	}
	coordinator.mu.Lock()
	coordinator.phase = shutdownPhaseClosed
	coordinator.stage = "closed"
	coordinator.closeErr = closeErr
	coordinator.mu.Unlock()
	close(coordinator.done)
}
