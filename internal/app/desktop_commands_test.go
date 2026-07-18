package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type desktopWindowStub struct {
	minimised               bool
	show, focus, unminimise int
}

func (stub *desktopWindowStub) IsMinimised() bool        { return stub.minimised }
func (stub *desktopWindowStub) UnMinimise()              { stub.minimised = false; stub.unminimise++ }
func (stub *desktopWindowStub) Show() application.Window { stub.show++; return nil }
func (stub *desktopWindowStub) Focus()                   { stub.focus++ }

type desktopEmitterStub struct {
	mu     sync.Mutex
	events []DesktopNavigationEvent
}

func (stub *desktopEmitterStub) Emit(name string, data ...any) bool {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if name == desktopNavigateEventName && len(data) == 1 {
		stub.events = append(stub.events, data[0].(DesktopNavigationEvent))
	}
	return false
}

type desktopAboutStub struct{ calls int }

func (stub *desktopAboutStub) ShowAbout() { stub.calls++ }

type desktopRefreshStub struct {
	sources []quotaonline.RefreshSource
	err     error
}

func (stub *desktopRefreshStub) RequestQuotaRefresh(_ context.Context, source quotaonline.RefreshSource) (QuotaRefreshReceipt, error) {
	stub.sources = append(stub.sources, source)
	return QuotaRefreshReceipt{}, stub.err
}

type desktopInvalidationStub struct{ domains []QueryInvalidationDomain }

func (stub *desktopInvalidationStub) Notify(_ context.Context, domain QueryInvalidationDomain) error {
	stub.domains = append(stub.domains, domain)
	return nil
}

type desktopDrainStub struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (stub *desktopDrainStub) Close(ctx context.Context) error {
	if stub.started != nil {
		close(stub.started)
	}
	if stub.release == nil {
		return stub.err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stub.release:
		return stub.err
	}
}

type desktopRecorderStub struct {
	actions []platformtray.MenuAction
	errors  []error
}

func (stub *desktopRecorderStub) Record(action platformtray.MenuAction, err error) {
	stub.actions = append(stub.actions, action)
	stub.errors = append(stub.errors, err)
}

func newDesktopCommandTestCoordinator(t *testing.T, mutate func(*desktopCommandCoordinatorConfig)) (*desktopCommandCoordinator, *desktopWindowStub, *desktopEmitterStub, *desktopAboutStub, *desktopRefreshStub, *desktopInvalidationStub) {
	t.Helper()
	window := &desktopWindowStub{}
	emitter := &desktopEmitterStub{}
	about := &desktopAboutStub{}
	refresh := &desktopRefreshStub{}
	invalidation := &desktopInvalidationStub{}
	config := desktopCommandCoordinatorConfig{
		Window: window, Emitter: emitter, About: about, Refresh: refresh,
		Invalidation: invalidation, Quit: func() {},
	}
	if mutate != nil {
		mutate(&config)
	}
	coordinator, err := newDesktopCommandCoordinator(config)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, window, emitter, about, refresh, invalidation
}

func TestDesktopCommandCoordinatorActivatesHiddenAndMinimisedWindowWithFiniteRoutes(t *testing.T) {
	t.Parallel()
	coordinator, window, emitter, _, _, _ := newDesktopCommandTestCoordinator(t, nil)
	window.minimised = true
	if err := coordinator.Execute(platformtray.MenuActionOpenOverview); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Execute(platformtray.MenuActionOpenSettings); err != nil {
		t.Fatal(err)
	}
	if window.unminimise != 1 || window.show != 2 || window.focus != 2 {
		t.Fatalf("activation = window:%#v", window)
	}
	if len(emitter.events) != 2 || emitter.events[0].Path != "/overview" || emitter.events[1].Path != "/settings" {
		t.Fatalf("routes = %#v", emitter.events)
	}
}

func TestDesktopCommandCoordinatorRefreshesBothSourcesAndInvalidatesAuthoritativeQueries(t *testing.T) {
	t.Parallel()
	coordinator, _, _, _, refresh, invalidation := newDesktopCommandTestCoordinator(t, nil)
	if err := coordinator.Execute(platformtray.MenuActionRefresh); err != nil {
		t.Fatal(err)
	}
	wantSources := []quotaonline.RefreshSource{quotaonline.RefreshSourceQuota, quotaonline.RefreshSourceResetCredits}
	if len(refresh.sources) != 2 || refresh.sources[0] != wantSources[0] || refresh.sources[1] != wantSources[1] {
		t.Fatalf("refresh sources = %#v", refresh.sources)
	}
	if len(invalidation.domains) != 2 || invalidation.domains[0] != QueryInvalidationQuota || invalidation.domains[1] != QueryInvalidationIndex {
		t.Fatalf("invalidations = %#v", invalidation.domains)
	}
}

func TestDesktopCommandCoordinatorShowsAboutAndRejectsUnknownAction(t *testing.T) {
	t.Parallel()
	coordinator, _, _, about, _, _ := newDesktopCommandTestCoordinator(t, nil)
	if err := coordinator.Execute(platformtray.MenuActionAbout); err != nil {
		t.Fatal(err)
	}
	if about.calls != 1 {
		t.Fatalf("about calls = %d", about.calls)
	}
	if err := coordinator.Execute(platformtray.MenuAction("unknown")); !errors.Is(err, ErrDesktopCommand) {
		t.Fatalf("unknown error = %v", err)
	}
}

func TestDesktopCommandCoordinatorDrainsBeforeQuitAndRejectsLaterCommands(t *testing.T) {
	t.Parallel()
	drain := &desktopDrainStub{started: make(chan struct{}), release: make(chan struct{})}
	quit := make(chan struct{})
	coordinator, _, _, _, _, _ := newDesktopCommandTestCoordinator(t, func(config *desktopCommandCoordinatorConfig) {
		config.Drain = drain
		config.Quit = func() { close(quit) }
	})
	done := make(chan error, 1)
	go func() { done <- coordinator.Execute(platformtray.MenuActionQuit) }()
	<-drain.started
	select {
	case <-quit:
		t.Fatal("quit ran before drain")
	default:
	}
	close(drain.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	default:
		t.Fatal("quit did not run")
	}
	if err := coordinator.Execute(platformtray.MenuActionOpenOverview); !errors.Is(err, ErrDesktopShuttingDown) {
		t.Fatalf("post-shutdown error = %v", err)
	}
}

func TestDesktopCommandCoordinatorQuitsWithoutOptionalRuntime(t *testing.T) {
	t.Parallel()
	quit := 0
	coordinator, _, _, _, _, _ := newDesktopCommandTestCoordinator(t, func(config *desktopCommandCoordinatorConfig) {
		config.Drain = nil
		config.Quit = func() { quit++ }
	})
	if err := coordinator.Execute(platformtray.MenuActionQuit); err != nil {
		t.Fatal(err)
	}
	if quit != 1 {
		t.Fatalf("quit calls = %d", quit)
	}
}

func TestDesktopCommandCoordinatorRecordsBoundedShutdownTimeoutAndKeepsAppAlive(t *testing.T) {
	t.Parallel()
	drain := &desktopDrainStub{release: make(chan struct{})}
	recorder := &desktopRecorderStub{}
	quit := 0
	coordinator, _, _, _, _, _ := newDesktopCommandTestCoordinator(t, func(config *desktopCommandCoordinatorConfig) {
		config.Drain = drain
		config.ShutdownTimeout = time.Millisecond
		config.Quit = func() { quit++ }
		config.Recorder = recorder
	})
	coordinator.Handle(platformtray.MenuActionQuit)
	if quit != 0 || len(recorder.errors) != 1 || !errors.Is(recorder.errors[0], context.DeadlineExceeded) {
		t.Fatalf("timeout closeout = quit:%d records:%#v", quit, recorder.errors)
	}
	close(drain.release)
	if err := coordinator.Execute(platformtray.MenuActionQuit); err != nil {
		t.Fatal(err)
	}
	if quit != 1 {
		t.Fatalf("retry quit calls = %d", quit)
	}
}
