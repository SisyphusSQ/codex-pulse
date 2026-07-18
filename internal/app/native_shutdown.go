package app

import (
	"context"
	"sync"
)

// nativeQuitPreflight routes Cmd+Q and platform termination requests through
// the same asynchronous drain used by tray Quit and Install. ShouldQuit runs on
// the AppKit main thread, so it must return immediately and let Quit destroy the
// application only after the shared coordinator reaches a terminal result.
type nativeQuitPreflight struct {
	mu       sync.Mutex
	shutdown *applicationShutdownCoordinator
	quit     func()
	started  bool
	complete bool
	install  bool
	ready    bool
}

func (preflight *nativeQuitPreflight) BeginInstall() error {
	if preflight == nil {
		return ErrDesktopCommand
	}
	preflight.mu.Lock()
	defer preflight.mu.Unlock()
	if preflight.started || preflight.complete || preflight.install {
		return ErrDesktopCommand
	}
	preflight.install = true
	preflight.ready = false
	return nil
}

func (preflight *nativeQuitPreflight) AbortInstall() {
	if preflight == nil {
		return
	}
	preflight.mu.Lock()
	preflight.install = false
	preflight.ready = false
	preflight.mu.Unlock()
}

func (preflight *nativeQuitPreflight) MarkInstallReady() {
	if preflight == nil {
		return
	}
	preflight.mu.Lock()
	if preflight.install {
		preflight.ready = true
	}
	preflight.mu.Unlock()
}

func (preflight *nativeQuitPreflight) BeginQuit() error {
	if preflight == nil {
		return ErrDesktopCommand
	}
	preflight.mu.Lock()
	defer preflight.mu.Unlock()
	if preflight.install {
		return ErrDesktopCommand
	}
	preflight.started = true
	return nil
}

func (preflight *nativeQuitPreflight) Bind(shutdown *applicationShutdownCoordinator, quit func()) error {
	if preflight == nil || shutdown == nil || quit == nil {
		return ErrDesktopCommand
	}
	preflight.mu.Lock()
	defer preflight.mu.Unlock()
	if preflight.shutdown != nil || preflight.quit != nil {
		return ErrDesktopCommand
	}
	preflight.shutdown = shutdown
	preflight.quit = quit
	return nil
}

func (preflight *nativeQuitPreflight) ShouldQuit() bool {
	if preflight == nil {
		return false
	}
	preflight.mu.Lock()
	if preflight.install {
		if preflight.ready && preflight.shutdown != nil && preflight.shutdown.Snapshot().Phase == shutdownPhaseClosed {
			preflight.complete = true
			preflight.mu.Unlock()
			return true
		}
		preflight.mu.Unlock()
		return false
	}
	// Tray Quit and Install may have already completed the shared coordinator
	// before AppKit asks ShouldQuit. Allow that first native termination request
	// synchronously so Sparkle's final reply is never bounced or canceled.
	if preflight.shutdown != nil && preflight.shutdown.Snapshot().Phase == shutdownPhaseClosed {
		preflight.complete = true
		preflight.mu.Unlock()
		return true
	}
	if preflight.complete {
		preflight.mu.Unlock()
		return true
	}
	if preflight.started || preflight.shutdown == nil || preflight.quit == nil {
		preflight.mu.Unlock()
		return false
	}
	preflight.started = true
	shutdown := preflight.shutdown
	quit := preflight.quit
	preflight.mu.Unlock()
	go func() {
		// A terminal component failure is observable through the shared snapshot
		// and blocks Install, but native Quit must still finish after all cleanup
		// stages have been attempted.
		_ = shutdown.Close(context.Background())
		preflight.mu.Lock()
		preflight.complete = true
		preflight.mu.Unlock()
		quit()
	}()
	return false
}
