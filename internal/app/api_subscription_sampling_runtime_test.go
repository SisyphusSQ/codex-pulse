package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
)

func TestAPISubscriptionSamplingRuntimeSamplesImmediatelyAndOnEveryTick(t *testing.T) {
	t.Parallel()
	ticks := make(chan time.Time, 2)
	source := &apiSubscriptionSamplingStub{calls: make(chan int64, 3)}
	runtime, err := startAPISubscriptionSamplingRuntime(t.Context(), apiSubscriptionSamplingRuntimeConfig{
		Source: source,
		Now:    func() time.Time { return time.UnixMilli(100) },
		Ticks:  ticks,
	})
	if err != nil {
		t.Fatalf("startAPISubscriptionSamplingRuntime() error = %v", err)
	}

	expectAPISubscriptionSample(t, source.calls, 100)
	ticks <- time.UnixMilli(200)
	expectAPISubscriptionSample(t, source.calls, 200)
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAPISubscriptionSamplingRuntimeContinuesAfterSourceFailure(t *testing.T) {
	t.Parallel()
	ticks := make(chan time.Time, 1)
	source := &apiSubscriptionSamplingStub{calls: make(chan int64, 2), failures: 1}
	runtime, err := startAPISubscriptionSamplingRuntime(t.Context(), apiSubscriptionSamplingRuntimeConfig{
		Source: source,
		Now:    func() time.Time { return time.UnixMilli(100) },
		Ticks:  ticks,
	})
	if err != nil {
		t.Fatalf("startAPISubscriptionSamplingRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	expectAPISubscriptionSample(t, source.calls, 100)
	ticks <- time.UnixMilli(200)
	expectAPISubscriptionSample(t, source.calls, 200)
}

type apiSubscriptionSamplingStub struct {
	mu       sync.Mutex
	calls    chan int64
	failures int
}

func (stub *apiSubscriptionSamplingStub) Current(
	_ context.Context,
	evaluatedAtMS int64,
) (apisubscriptions.CurrentSnapshot, error) {
	stub.calls <- evaluatedAtMS
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.failures > 0 {
		stub.failures--
		return apisubscriptions.CurrentSnapshot{}, errors.New("temporary source failure")
	}
	return apisubscriptions.CurrentSnapshot{}, nil
}

func expectAPISubscriptionSample(t testing.TB, calls <-chan int64, want int64) {
	t.Helper()
	select {
	case got := <-calls:
		if got != want {
			t.Fatalf("sample time = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for sample %d", want)
	}
}
