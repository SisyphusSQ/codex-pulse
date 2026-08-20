package app

import (
	"context"
	"errors"
	"testing"
)

func TestBackgroundWorkerNormalizesCancellation(t *testing.T) {
	t.Parallel()

	worker := startBackgroundWorker(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err, terminal := worker.stopAndWait(context.Background()); err != nil || !terminal {
		t.Fatalf("stopAndWait() = %v, %t; want nil, true", err, terminal)
	}
}

func TestBackgroundWorkerReturnsTerminalError(t *testing.T) {
	t.Parallel()

	want := errors.New("worker failed")
	worker := startBackgroundWorker(context.Background(), func(context.Context) error { return want })
	if err, terminal := worker.stopAndWait(context.Background()); !errors.Is(err, want) || !terminal {
		t.Fatalf("stopAndWait() = %v, %t; want worker error, true", err, terminal)
	}
}

func TestBackgroundWorkerCanWaitAgainAfterCallerCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	worker := startBackgroundWorker(context.Background(), func(context.Context) error {
		<-release
		return nil
	})
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if err, terminal := worker.stopAndWait(waitCtx); !errors.Is(err, context.Canceled) || terminal {
		t.Fatalf("stopAndWait(cancelled) = %v, %t; want context.Canceled, false", err, terminal)
	}
	close(release)
	if err, terminal := worker.stopAndWait(context.Background()); err != nil || !terminal {
		t.Fatalf("stopAndWait(retry) = %v, %t; want nil, true", err, terminal)
	}
}
