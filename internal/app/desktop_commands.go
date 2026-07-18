package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	desktopNavigateEventName = "codex-pulse:navigate"
	desktopShutdownTimeout   = 15 * time.Second
)

var (
	ErrDesktopCommand      = errors.New("desktop command is unavailable")
	ErrDesktopShuttingDown = errors.New("desktop shutdown is in progress")
)

type DesktopNavigationEvent struct {
	Path string `json:"path"`
}

func init() {
	application.RegisterEvent[DesktopNavigationEvent](desktopNavigateEventName)
}

type desktopMainWindow interface {
	IsMinimised() bool
	UnMinimise()
	Show() application.Window
	Focus()
}

type desktopAboutPresenter interface {
	ShowAbout()
}

type desktopRefreshCommand interface {
	RequestQuotaRefresh(context.Context, quotaonline.RefreshSource) (QuotaRefreshReceipt, error)
}

type desktopRuntimeDrainer interface {
	Close(context.Context) error
}

type desktopCommandErrorRecorder interface {
	Record(platformtray.MenuAction, error)
}

type desktopCommandErrorLog struct{}

func (desktopCommandErrorLog) Record(action platformtray.MenuAction, err error) {
	if err != nil {
		log.Printf("desktop command %s failed (%T)", action, err)
	}
}

type desktopCommandCoordinatorConfig struct {
	Window          desktopMainWindow
	Emitter         queryInvalidationEmitter
	About           desktopAboutPresenter
	Refresh         desktopRefreshCommand
	Invalidation    queryInvalidationNotifier
	Drain           desktopRuntimeDrainer
	Quit            func()
	ShutdownTimeout time.Duration
	Recorder        desktopCommandErrorRecorder
}

type desktopCommandCoordinator struct {
	mu              sync.Mutex
	window          desktopMainWindow
	emitter         queryInvalidationEmitter
	about           desktopAboutPresenter
	refresh         desktopRefreshCommand
	invalidation    queryInvalidationNotifier
	drain           desktopRuntimeDrainer
	quit            func()
	shutdownTimeout time.Duration
	recorder        desktopCommandErrorRecorder
	quitting        bool
}

func newDesktopCommandCoordinator(config desktopCommandCoordinatorConfig) (*desktopCommandCoordinator, error) {
	if config.Window == nil || config.Emitter == nil || config.About == nil || config.Refresh == nil ||
		config.Quit == nil {
		return nil, ErrDesktopCommand
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = desktopShutdownTimeout
	}
	if config.Recorder == nil {
		config.Recorder = desktopCommandErrorLog{}
	}
	return &desktopCommandCoordinator{
		window: config.Window, emitter: config.Emitter, about: config.About,
		refresh: config.Refresh, invalidation: config.Invalidation, drain: config.Drain,
		quit:            config.Quit,
		shutdownTimeout: config.ShutdownTimeout, recorder: config.Recorder,
	}, nil
}

func (coordinator *desktopCommandCoordinator) Handle(action platformtray.MenuAction) {
	if err := coordinator.Execute(action); err != nil && coordinator != nil && coordinator.recorder != nil {
		coordinator.recorder.Record(action, err)
	}
}

func (coordinator *desktopCommandCoordinator) Execute(action platformtray.MenuAction) error {
	if coordinator == nil {
		return ErrDesktopCommand
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.quitting {
		return ErrDesktopShuttingDown
	}

	switch action {
	case platformtray.MenuActionOpenOverview:
		coordinator.openRoute("/overview")
		return nil
	case platformtray.MenuActionRefresh:
		return coordinator.refreshAuthoritativeData()
	case platformtray.MenuActionOpenSettings:
		coordinator.openRoute("/settings")
		return nil
	case platformtray.MenuActionAbout:
		coordinator.about.ShowAbout()
		return nil
	case platformtray.MenuActionQuit:
		coordinator.quitting = true
		err := coordinator.shutdown()
		if err != nil {
			coordinator.quitting = false
		}
		return err
	default:
		return ErrDesktopCommand
	}
}

func (coordinator *desktopCommandCoordinator) openRoute(path string) {
	if coordinator.window.IsMinimised() {
		coordinator.window.UnMinimise()
	}
	coordinator.window.Show()
	coordinator.window.Focus()
	coordinator.emitter.Emit(desktopNavigateEventName, DesktopNavigationEvent{Path: path})
}

func (coordinator *desktopCommandCoordinator) refreshAuthoritativeData() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var refreshErr error
	for _, source := range []quotaonline.RefreshSource{
		quotaonline.RefreshSourceQuota,
		quotaonline.RefreshSourceResetCredits,
	} {
		_, err := coordinator.refresh.RequestQuotaRefresh(ctx, source)
		refreshErr = errors.Join(refreshErr, err)
	}
	notifyQueryInvalidation(coordinator.invalidation, ctx, QueryInvalidationQuota)
	notifyQueryInvalidation(coordinator.invalidation, ctx, QueryInvalidationIndex)
	return refreshErr
}

func (coordinator *desktopCommandCoordinator) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), coordinator.shutdownTimeout)
	defer cancel()
	var drainErr error
	if coordinator.drain != nil {
		drainErr = coordinator.drain.Close(ctx)
	}
	if drainErr != nil {
		return drainErr
	}
	coordinator.quit()
	return nil
}
