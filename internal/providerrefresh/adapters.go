package providerrefresh

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type CursorService interface {
	RefreshForGlobal(context.Context, bool) cursorprovider.GlobalRefreshReport
}

type GrokService interface {
	RefreshForGlobal(context.Context, bool) grokprovider.GlobalRefreshReport
}

type CursorAdapter struct {
	service CursorService
	now     func() time.Time
}

func NewCursorAdapter(service CursorService, now func() time.Time) *CursorAdapter {
	if now == nil {
		now = time.Now
	}
	return &CursorAdapter{service: service, now: now}
}

func (adapter *CursorAdapter) RefreshProvider(ctx context.Context, trigger string) ProviderResult {
	if adapter == nil || adapter.service == nil {
		return UnavailableProvider(
			agentprovider.Cursor,
			ComponentCursorLocal, ComponentCursorDashboard, ComponentCursorGrokBot,
		)
	}
	observed := adapter.now().UnixMilli()
	report := adapter.service.RefreshForGlobal(ctx, InteractiveTrigger(trigger))
	local := ClassifyLocalError(report.Local.Err, report.Local.Attempted)
	dashboard := classifyCursorOnline(report.Dashboard)
	grokBot := classifyCursorOnline(report.GrokBot)
	return SummarizeProvider(agentprovider.Cursor, []ComponentResult{
		WithComponent(ComponentCursorLocal, local, observed),
		WithComponent(ComponentCursorDashboard, dashboard, observed),
		WithComponent(ComponentCursorGrokBot, grokBot, observed),
	})
}

func classifyCursorOnline(value cursorprovider.OnlineRefreshClass) ComponentResult {
	if value.Err == nil && !value.Attempted {
		if value.Remote {
			return ComponentResult{Status: StatusFailed, ReasonCode: ReasonFailed, Attempted: true}
		}
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue}
	}
	return ClassifyOnlineError(value.Err, value.Remote || value.Attempted && value.Err == nil)
}

type GrokAdapter struct {
	service GrokService
	now     func() time.Time
}

func NewGrokAdapter(service GrokService, now func() time.Time) *GrokAdapter {
	if now == nil {
		now = time.Now
	}
	return &GrokAdapter{service: service, now: now}
}

func (adapter *GrokAdapter) RefreshProvider(ctx context.Context, trigger string) ProviderResult {
	if adapter == nil || adapter.service == nil {
		return UnavailableProvider(agentprovider.Grok, ComponentGrokLocal, ComponentGrokBilling)
	}
	observed := adapter.now().UnixMilli()
	report := adapter.service.RefreshForGlobal(ctx, InteractiveTrigger(trigger))
	local := ClassifyLocalError(report.Local.Err, report.Local.Attempted)
	billing := classifyGrokBilling(report.Billing)
	return SummarizeProvider(agentprovider.Grok, []ComponentResult{
		WithComponent(ComponentGrokLocal, local, observed),
		WithComponent(ComponentGrokBilling, billing, observed),
	})
}

func classifyGrokBilling(value grokprovider.BillingRefreshClass) ComponentResult {
	if value.Disabled {
		return ComponentResult{Status: StatusSkippedDisabled, ReasonCode: ReasonDisabled}
	}
	if value.Err == nil && !value.Attempted {
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue}
	}
	return ClassifyOnlineError(value.Err, value.Remote || value.Attempted && value.Err == nil)
}

type CodexQuotaRefresher interface {
	RequestRefreshResult(context.Context, quotaonline.RefreshSource, store.SourceRefreshTrigger) (store.SourceRefreshSchedule, bool, error)
}

type CodexLocalRefresher interface {
	RefreshLocal(context.Context) ComponentResult
}

type CodexAdapter struct {
	quota CodexQuotaRefresher
	local CodexLocalRefresher
	now   func() time.Time
}

func NewCodexAdapter(quota CodexQuotaRefresher, local CodexLocalRefresher, now func() time.Time) *CodexAdapter {
	if now == nil {
		now = time.Now
	}
	return &CodexAdapter{quota: quota, local: local, now: now}
}

func (adapter *CodexAdapter) RefreshProvider(ctx context.Context, trigger string) ProviderResult {
	observed := adapter.now().UnixMilli()
	local := ComponentResult{Status: StatusSkippedUnavailable, ReasonCode: ReasonUnavailable}
	if adapter != nil && adapter.local != nil {
		local = adapter.local.RefreshLocal(ctx)
	}
	quota := adapter.refreshCodexSource(ctx, quotaonline.RefreshSourceQuota, trigger)
	reset := adapter.refreshCodexSource(ctx, quotaonline.RefreshSourceResetCredits, trigger)
	return SummarizeProvider(agentprovider.Codex, []ComponentResult{
		WithComponent(ComponentCodexLocal, local, observed),
		WithComponent(ComponentCodexQuota, quota, observed),
		WithComponent(ComponentCodexResetCredits, reset, observed),
	})
}

func (adapter *CodexAdapter) refreshCodexSource(
	ctx context.Context,
	source quotaonline.RefreshSource,
	trigger string,
) ComponentResult {
	if adapter == nil || adapter.quota == nil {
		return ComponentResult{Status: StatusSkippedUnavailable, ReasonCode: ReasonUnavailable}
	}
	storeTrigger := StoreTrigger(trigger)
	schedule, fetched, err := adapter.quota.RequestRefreshResult(ctx, source, storeTrigger)
	result := ClassifyCodexSchedule(schedule.Reason, fetched, err)
	if result.Status == StatusRefreshed {
		result.CommittedAtMS = CloneInt64(schedule.LastManualAtMS)
		if result.CommittedAtMS == nil {
			result.CommittedAtMS = TimestampPointer(schedule.UpdatedAtMS)
		}
	}
	return result
}
