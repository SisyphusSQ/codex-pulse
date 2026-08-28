package cursorprovider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// OnlineRefreshClass is the settled outcome of one Cursor online collector
// call. Remote is true only when the HTTP client was invoked.
type OnlineRefreshClass struct {
	Attempted bool
	Remote    bool
	Err       error
}

func (collector *DashboardCollector) RefreshClassified(ctx context.Context, interactive bool) OnlineRefreshClass {
	if collector == nil {
		return OnlineRefreshClass{Err: ErrDashboardProtocol}
	}
	minimum := collector.config.MinimumRefresh
	if interactive {
		minimum = min(minimum, dashboardInteractiveRefreshInterval)
	}
	return collector.refreshClassified(ctx, minimum)
}

func (collector *DashboardCollector) refreshClassified(
	ctx context.Context,
	minimumRefresh time.Duration,
) OnlineRefreshClass {
	if collector == nil || ctx == nil {
		return OnlineRefreshClass{Err: ErrDashboardProtocol}
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < minimumRefresh {
		return OnlineRefreshClass{}
	}
	atMS := now.UnixMilli()
	current, err := collector.client.GetCurrentPeriodUsage(ctx)
	if err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	if current.BillingCycleStartMS <= 0 || current.BillingCycleStartMS >= atMS ||
		current.BillingCycleEndMS <= current.BillingCycleStartMS {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(ErrDashboardProtocol))
	}
	planUsage, err := dashboardPlanUsage(current.PlanUsage)
	if err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	quotaWindows, err := dashboardQuotaWindows(current.PlanUsage, current.BillingCycleStartMS, current.BillingCycleEndMS)
	if err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	events, err := collector.readUsageEvents(ctx, current.BillingCycleStartMS, atMS)
	if err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	if err := validateDashboardAggregate(events); err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	if err := collector.writer.CommitCursorDashboardSnapshot(ctx, store.CursorDashboardSnapshot{
		Generation: atMS, CollectedAtMS: atMS,
		WindowStartMS: current.BillingCycleStartMS, WindowEndMS: atMS,
		BillingCycleEndMS: current.BillingCycleEndMS, PlanUsage: planUsage,
		QuotaWindows: quotaWindows, Events: events,
	}); err != nil {
		return OnlineRefreshClass{
			Attempted: true, Remote: true,
			Err: fmt.Errorf("%w: persist dashboard snapshot", ErrDashboardProtocol),
		}
	}
	collector.last = now
	return OnlineRefreshClass{Attempted: true, Remote: true}
}

func (collector *DashboardCollector) finishClassified(
	ctx context.Context,
	atMS int64,
	result OnlineRefreshClass,
) OnlineRefreshClass {
	if result.Remote || result.Attempted {
		if err := collector.recordFailure(ctx, atMS, result.Err); err != nil {
			result.Err = err
			result.Attempted = true
		}
	}
	return result
}

func (collector *GrokBotCollector) RefreshClassified(ctx context.Context, interactive bool) OnlineRefreshClass {
	if collector == nil || ctx == nil {
		return OnlineRefreshClass{Err: ErrDashboardProtocol}
	}
	minimum := collector.config.MinimumRefresh
	if interactive {
		minimum = min(minimum, dashboardInteractiveRefreshInterval)
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < minimum {
		return OnlineRefreshClass{}
	}
	atMS := now.UnixMilli()
	status, err := collector.client.GetSandUsageStatus(ctx)
	if err != nil {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(err))
	}
	if status.PeriodStartMS < 0 || status.NextResetAtMS <= status.PeriodStartMS ||
		atMS < status.PeriodStartMS || atMS > status.NextResetAtMS || status.NextResetAtMS <= atMS {
		return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(ErrDashboardProtocol))
	}
	commit := store.CursorGrokBotCommit{
		Generation: atMS, CollectedAtMS: atMS, Included: status.Included,
		CycleStartAtMS: status.PeriodStartMS, CycleEndAtMS: status.NextResetAtMS,
	}
	if status.Included {
		if status.UsagePercent == nil {
			return collector.finishClassified(ctx, atMS, classifyCursorOnlineError(ErrDashboardProtocol))
		}
		commit.UsedPercent = status.UsagePercent
	}
	if err := collector.writer.CommitCursorGrokBotObservation(ctx, commit); err != nil {
		return OnlineRefreshClass{
			Attempted: true, Remote: true,
			Err: fmt.Errorf("%w: persist grok bot observation", ErrDashboardProtocol),
		}
	}
	collector.last = now
	return OnlineRefreshClass{Attempted: true, Remote: true}
}

func (collector *GrokBotCollector) finishClassified(
	ctx context.Context,
	atMS int64,
	result OnlineRefreshClass,
) OnlineRefreshClass {
	if result.Remote || result.Attempted {
		if err := collector.recordFailure(ctx, atMS, result.Err); err != nil {
			result.Err = err
			result.Attempted = true
		}
	}
	return result
}

func classifyCursorOnlineError(err error) OnlineRefreshClass {
	if err == nil {
		return OnlineRefreshClass{Attempted: true, Remote: true}
	}
	if errors.Is(err, ErrDesktopAuthUnavailable) || errors.Is(err, ErrDesktopAuthExpired) {
		return OnlineRefreshClass{Err: err}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return OnlineRefreshClass{Attempted: false, Err: err}
	}
	return OnlineRefreshClass{Attempted: true, Remote: true, Err: err}
}
