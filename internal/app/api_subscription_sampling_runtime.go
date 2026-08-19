package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
)

const apiSubscriptionSamplingInterval = 15 * time.Minute

var errAPISubscriptionSamplingRuntime = errors.New("API subscription sampling runtime is unavailable")

type apiSubscriptionSamplingSource interface {
	Current(context.Context, int64) (apisubscriptions.CurrentSnapshot, error)
}

type apiSubscriptionSamplingRuntimeConfig struct {
	Source   apiSubscriptionSamplingSource
	Interval time.Duration
	Now      func() time.Time
	Ticks    <-chan time.Time
}

type apiSubscriptionSamplingRuntime struct {
	cancel     context.CancelFunc
	workerDone chan struct{}
	stopTicker func()

	closeOnce sync.Once
}

func startAPISubscriptionSamplingRuntime(
	ctx context.Context,
	config apiSubscriptionSamplingRuntimeConfig,
) (*apiSubscriptionSamplingRuntime, error) {
	if ctx == nil || config.Source == nil {
		return nil, errAPISubscriptionSamplingRuntime
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	ticks := config.Ticks
	stopTicker := func() {}
	if ticks == nil {
		interval := config.Interval
		if interval == 0 {
			interval = apiSubscriptionSamplingInterval
		}
		if interval < 0 {
			return nil, errAPISubscriptionSamplingRuntime
		}
		ticker := time.NewTicker(interval)
		ticks = ticker.C
		stopTicker = ticker.Stop
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &apiSubscriptionSamplingRuntime{
		cancel: cancel, workerDone: make(chan struct{}), stopTicker: stopTicker,
	}
	go func() {
		defer close(runtime.workerDone)
		_, _ = config.Source.Current(runCtx, config.Now().UnixMilli())
		for {
			select {
			case <-runCtx.Done():
				return
			case tick, ok := <-ticks:
				if !ok {
					return
				}
				_, _ = config.Source.Current(runCtx, tick.UnixMilli())
			}
		}
	}()
	return runtime, nil
}

func (runtime *apiSubscriptionSamplingRuntime) Close(ctx context.Context) error {
	if runtime == nil || runtime.cancel == nil || runtime.workerDone == nil || runtime.stopTicker == nil || ctx == nil {
		return errAPISubscriptionSamplingRuntime
	}
	runtime.closeOnce.Do(func() {
		runtime.stopTicker()
		runtime.cancel()
	})
	select {
	case <-runtime.workerDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", errAPISubscriptionSamplingRuntime, ctx.Err())
	}
}
