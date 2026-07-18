package app

import (
	"context"
	"errors"
	"sync"
	"time"

	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type trayProjectionQuery interface {
	QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error)
	HealthProjection(context.Context) (HealthProjectionResponse, error)
}

type traySnapshotReader struct {
	query trayProjectionQuery
}

func (reader traySnapshotReader) Read(ctx context.Context, evaluatedAtMS int64) (platformtray.Snapshot, error) {
	if reader.query == nil {
		return platformtray.Snapshot{}, ErrBindingService
	}
	quota, quotaErr := reader.query.QuotaCurrent(ctx, evaluatedAtMS)
	health, healthErr := reader.query.HealthProjection(ctx)
	snapshot := platformtray.Snapshot{
		Windows: make([]platformtray.WindowSnapshot, 0, len(quota.Current.Windows)),
		Health:  mapTrayHealth(health, healthErr),
	}
	for _, window := range quota.Current.Windows {
		kind, ok := mapTrayWindowKind(window.WindowKind)
		if !ok {
			continue
		}
		snapshot.Windows = append(snapshot.Windows, platformtray.WindowSnapshot{
			Kind: kind, RemainingPercent: cloneTrayFloat64(window.RemainingPercent),
			ResetRemainingMS: cloneBindingInt64(window.ResetRemainingMS),
			Freshness:        platformtray.Freshness(window.Freshness),
			Conflict:         window.Conflict == store.QuotaConflictPresent,
		})
	}
	// Health is an independent signal: a health query failure degrades the dot,
	// but must not discard an otherwise trusted quota snapshot.
	return snapshot, quotaErr
}

func mapTrayWindowKind(kind store.QuotaWindowKind) (platformtray.WindowKind, bool) {
	switch kind {
	case store.QuotaWindowPrimary:
		return platformtray.WindowPrimary, true
	case store.QuotaWindowSecondary:
		return platformtray.WindowSecondary, true
	default:
		return "", false
	}
}

func mapTrayHealth(response HealthProjectionResponse, queryErr error) platformtray.HealthState {
	if queryErr != nil || !response.HasValue || response.Stale || response.Level == nil {
		return platformtray.HealthDegraded
	}
	switch *response.Level {
	case HealthProjectionBlocked:
		return platformtray.HealthBlocked
	case HealthProjectionDegraded:
		return platformtray.HealthDegraded
	default:
		return platformtray.HealthNone
	}
}

func cloneTrayFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

type trayLaunchRegistrar interface {
	OnApplicationEvent(events.ApplicationEventType, func(*application.ApplicationEvent)) func()
}

type trayRuntimeHost struct {
	mu       sync.Mutex
	query    trayProjectionQuery
	runtime  *platformtray.Runtime
	pending  bool
	startErr error
	starting bool
	cancel   func()
	quit     func()
	closed   bool
	closeErr error
}

func newTrayRuntimeHost(registrar trayLaunchRegistrar, query trayProjectionQuery, quit func()) (*trayRuntimeHost, error) {
	if registrar == nil || query == nil || quit == nil {
		return nil, platformtray.ErrTrayRuntime
	}
	host := &trayRuntimeHost{query: query, quit: quit}
	host.cancel = registrar.OnApplicationEvent(events.Mac.ApplicationDidFinishLaunching, func(*application.ApplicationEvent) {
		host.start()
	})
	if host.cancel == nil {
		return nil, platformtray.ErrTrayRuntime
	}
	return host, nil
}

func (host *trayRuntimeHost) start() {
	host.mu.Lock()
	if host.closed || host.starting || host.runtime != nil || host.startErr != nil {
		host.mu.Unlock()
		return
	}
	host.starting = true
	host.mu.Unlock()
	renderer, err := platformtray.NewNativeStatusItem()
	if err == nil {
		var runtime *platformtray.Runtime
		runtime, err = platformtray.StartRuntime(context.Background(), platformtray.RuntimeConfig{
			Reader: traySnapshotReader{query: host.query}, Renderer: renderer,
			TickInterval: 30 * time.Second, MinimumRefreshInterval: 500 * time.Millisecond,
		})
		if err == nil {
			host.mu.Lock()
			host.starting = false
			if host.closed {
				host.mu.Unlock()
				_ = runtime.Close(context.Background())
				return
			}
			host.runtime = runtime
			pending := host.pending
			host.pending = false
			host.mu.Unlock()
			if pending {
				runtime.Invalidate()
			}
			return
		}
		_ = renderer.Close()
	}
	host.mu.Lock()
	host.starting = false
	host.startErr = err
	host.mu.Unlock()
	go host.quit()
}

func (host *trayRuntimeHost) Invalidate(domain QueryInvalidationDomain) {
	if host == nil || (domain != QueryInvalidationQuota && domain != QueryInvalidationHealth) {
		return
	}
	host.mu.Lock()
	runtime := host.runtime
	if runtime == nil && !host.closed {
		host.pending = true
	}
	host.mu.Unlock()
	if runtime != nil {
		runtime.Invalidate()
	}
}

func (host *trayRuntimeHost) Close(ctx context.Context) error {
	if host == nil {
		return nil
	}
	host.mu.Lock()
	if host.closed {
		err := host.closeErr
		host.mu.Unlock()
		return err
	}
	host.closed = true
	cancel := host.cancel
	runtime := host.runtime
	startErr := host.startErr
	host.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if runtime != nil {
		host.mu.Lock()
		host.closeErr = errors.Join(startErr, runtime.Close(ctx))
		err := host.closeErr
		host.mu.Unlock()
		return err
	}
	host.mu.Lock()
	host.closeErr = startErr
	host.mu.Unlock()
	return startErr
}
