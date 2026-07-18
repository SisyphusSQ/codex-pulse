package tray

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type runtimeReaderStub struct {
	mu       sync.Mutex
	calls    int
	snapshot Snapshot
	err      error
}

func (stub *runtimeReaderStub) Read(context.Context, int64) (Snapshot, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls++
	return stub.snapshot, stub.err
}

func (stub *runtimeReaderStub) count() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}

type runtimeRendererStub struct {
	mu        sync.Mutex
	models    []StatusViewModel
	closed    int
	updateErr error
}

func (stub *runtimeRendererStub) Update(model StatusViewModel) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.models = append(stub.models, model)
	return stub.updateErr
}

func (stub *runtimeRendererStub) Close() error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.closed++
	return nil
}

func (stub *runtimeRendererStub) snapshot() ([]StatusViewModel, int) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]StatusViewModel(nil), stub.models...), stub.closed
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met")
}

func TestRuntimeCoalescesRefreshAndSkipsIdenticalPaint(t *testing.T) {
	t.Parallel()

	reader := &runtimeReaderStub{snapshot: trustedSnapshot(62, 71)}
	renderer := &runtimeRendererStub{}
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Reader: reader, Renderer: renderer, TickInterval: time.Hour,
		MinimumRefreshInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	waitFor(t, func() bool { return reader.count() == 1 })

	for range 20 {
		runtime.Invalidate()
	}
	waitFor(t, func() bool { return reader.count() == 2 })
	time.Sleep(40 * time.Millisecond)
	if calls := reader.count(); calls != 2 {
		t.Fatalf("event burst was not coalesced: %d calls", calls)
	}
	models, _ := renderer.snapshot()
	if len(models) != 1 {
		t.Fatalf("identical model repainted %d times", len(models))
	}
}

func TestRuntimePaintsFailureFromLastTrustedAndClosesOnce(t *testing.T) {
	t.Parallel()

	reader := &runtimeReaderStub{snapshot: trustedSnapshot(62, 71)}
	renderer := &runtimeRendererStub{}
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Reader: reader, Renderer: renderer, TickInterval: time.Hour,
		MinimumRefreshInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return reader.count() == 1 })
	reader.mu.Lock()
	reader.err = errors.New("read failed")
	reader.mu.Unlock()
	runtime.Invalidate()
	waitFor(t, func() bool { return reader.count() == 2 })
	waitFor(t, func() bool { models, _ := renderer.snapshot(); return len(models) == 2 })
	models, _ := renderer.snapshot()
	if models[1].State != DisplayStale || models[1].Rows[0].Value != "62%" {
		t.Fatalf("failure projection is wrong: %#v", models[1])
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, closed := renderer.snapshot()
	if closed != 1 {
		t.Fatalf("renderer closed %d times", closed)
	}
}

func TestStartRuntimeValidatesConfigurationAndReportsRendererError(t *testing.T) {
	t.Parallel()

	if _, err := StartRuntime(context.Background(), RuntimeConfig{}); !errors.Is(err, ErrTrayRuntime) {
		t.Fatalf("unexpected validation error: %v", err)
	}
	reader := &runtimeReaderStub{snapshot: trustedSnapshot(62, 71)}
	renderer := &runtimeRendererStub{updateErr: errors.New("paint failed")}
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		Reader: reader, Renderer: renderer, TickInterval: time.Hour,
		MinimumRefreshInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return reader.count() == 1 })
	if err := runtime.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "paint failed") {
		t.Fatalf("renderer failure was not returned: %v", err)
	}
}
