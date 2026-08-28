package app

import (
	"context"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/providerrefresh"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type globalRefreshOwner struct {
	runtime *applicationLifecycleRuntime
}

func (owner *globalRefreshOwner) Refresh(ctx context.Context, trigger string) {
	if owner == nil || owner.runtime == nil {
		return
	}
	owner.runtime.refreshGlobal(ctx, trigger)
}

func (runtime *applicationLifecycleRuntime) SetGlobalRefresh(orchestrator *providerrefresh.Orchestrator) {
	if runtime == nil || orchestrator == nil {
		return
	}
	runtime.globalRefreshMu.Lock()
	runtime.globalRefresh = orchestrator
	runtime.globalRefreshMu.Unlock()
	orchestrator.BindCodex(providerrefresh.NewCodexAdapter(runtime, runtime, nil))
	if runtime.controlCtx != nil {
		go func() {
			_, _ = orchestrator.Refresh(runtime.controlCtx, providerrefresh.TriggerStartup)
			providerrefresh.StartScheduledLoop(runtime.controlCtx, orchestrator, 0)
		}()
	}
}

func (runtime *applicationLifecycleRuntime) refreshGlobal(ctx context.Context, trigger string) {
	if runtime == nil {
		return
	}
	runtime.globalRefreshMu.Lock()
	orchestrator := runtime.globalRefresh
	runtime.globalRefreshMu.Unlock()
	if orchestrator == nil {
		if runtime.quota != nil {
			_ = runtime.quota.requestLifecycleRefresh(ctx, providerrefresh.StoreTrigger(trigger))
		}
		return
	}
	_, _ = orchestrator.Refresh(ctx, trigger)
}

func (runtime *applicationLifecycleRuntime) RefreshLocal(ctx context.Context) providerrefresh.ComponentResult {
	if runtime == nil || runtime.lightRun == nil {
		return providerrefresh.ComponentResult{
			Status: providerrefresh.StatusSkippedUnavailable, ReasonCode: providerrefresh.ReasonUnavailable,
		}
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return providerrefresh.ComponentResult{
				Status: providerrefresh.StatusFailed, ReasonCode: providerrefresh.ReasonCancelled,
			}
		}
	}
	if runtime.lightRun.Trigger() {
		return providerrefresh.ComponentResult{
			Status: providerrefresh.StatusRefreshed, ReasonCode: providerrefresh.StatusRefreshed, Attempted: true,
		}
	}
	return providerrefresh.ComponentResult{
		Status: providerrefresh.StatusSkippedUnavailable, ReasonCode: providerrefresh.ReasonUnavailable,
	}
}

func (runtime *applicationLifecycleRuntime) RequestRefreshResult(
	ctx context.Context,
	source quotaonline.RefreshSource,
	trigger store.SourceRefreshTrigger,
) (store.SourceRefreshSchedule, bool, error) {
	if runtime == nil || runtime.quota == nil {
		return store.SourceRefreshSchedule{}, false, ErrApplicationLifecycleRuntime
	}
	return runtime.quota.RequestRefreshResult(ctx, source, trigger)
}
