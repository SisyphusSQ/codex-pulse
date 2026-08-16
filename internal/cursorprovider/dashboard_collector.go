package cursorprovider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const dashboardPageSize int32 = 100

type DashboardUsageClient interface {
	GetCurrentPeriodUsage(context.Context) (CurrentPeriodUsage, error)
	GetFilteredUsageEvents(context.Context, UsageEventsRequest) (UsageEventsPage, error)
}

type DashboardSnapshotWriter interface {
	CommitCursorDashboardSnapshot(context.Context, store.CursorDashboardSnapshot) error
	RecordCursorDashboardFailure(context.Context, int64, string) error
}

type DashboardCollectorConfig struct {
	MinimumRefresh time.Duration
	Now            func() time.Time
}

type DashboardCollector struct {
	client DashboardUsageClient
	writer DashboardSnapshotWriter
	config DashboardCollectorConfig
	mu     sync.Mutex
	last   time.Time
}

func NewDashboardCollector(client DashboardUsageClient, writer DashboardSnapshotWriter, config DashboardCollectorConfig) (*DashboardCollector, error) {
	if client == nil || writer == nil || config.Now == nil || config.MinimumRefresh < 0 {
		return nil, ErrDashboardProtocol
	}
	return &DashboardCollector{client: client, writer: writer, config: config}, nil
}

func (collector *DashboardCollector) Refresh(ctx context.Context) error {
	if collector == nil || ctx == nil {
		return ErrDashboardProtocol
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	now := collector.config.Now()
	if !collector.last.IsZero() && now.Sub(collector.last) < collector.config.MinimumRefresh {
		return nil
	}
	atMS := now.UnixMilli()
	current, err := collector.client.GetCurrentPeriodUsage(ctx)
	if err != nil {
		return collector.recordFailure(ctx, atMS, err)
	}
	if current.BillingCycleStartMS <= 0 || current.BillingCycleStartMS >= atMS ||
		current.BillingCycleEndMS <= current.BillingCycleStartMS {
		return collector.recordFailure(ctx, atMS, ErrDashboardProtocol)
	}
	planUsage, err := dashboardPlanUsage(current.PlanUsage)
	if err != nil {
		return collector.recordFailure(ctx, atMS, err)
	}
	quotaWindows, err := dashboardQuotaWindows(current.PlanUsage)
	if err != nil {
		return collector.recordFailure(ctx, atMS, err)
	}

	events, err := collector.readUsageEvents(ctx, current.BillingCycleStartMS, atMS)
	if err != nil {
		return collector.recordFailure(ctx, atMS, err)
	}
	if err := validateDashboardAggregate(events); err != nil {
		return collector.recordFailure(ctx, atMS, err)
	}
	if err := collector.writer.CommitCursorDashboardSnapshot(ctx, store.CursorDashboardSnapshot{
		Generation: atMS, CollectedAtMS: atMS,
		WindowStartMS: current.BillingCycleStartMS, WindowEndMS: atMS,
		BillingCycleEndMS: current.BillingCycleEndMS, PlanUsage: planUsage,
		QuotaWindows: quotaWindows, Events: events,
	}); err != nil {
		return fmt.Errorf("%w: persist dashboard snapshot", ErrDashboardProtocol)
	}
	collector.last = now
	return nil
}

func dashboardQuotaWindows(value *CurrentPlanUsage) ([]store.CursorDashboardQuotaWindow, error) {
	if value == nil {
		return nil, nil
	}
	windows := make([]store.CursorDashboardQuotaWindow, 0, 2)
	for _, candidate := range []struct {
		limitID string
		percent *float64
	}{
		{limitID: "cursor.models", percent: value.CursorModelsUsedPercent},
		{limitID: "cursor.other_models", percent: value.OtherModelsUsedPercent},
	} {
		if candidate.percent == nil {
			continue
		}
		if math.IsNaN(*candidate.percent) || math.IsInf(*candidate.percent, 0) ||
			*candidate.percent < 0 || *candidate.percent > 100 {
			return nil, ErrDashboardProtocol
		}
		windows = append(windows, store.CursorDashboardQuotaWindow{
			LimitID: candidate.limitID, UsedPercent: *candidate.percent,
		})
	}
	return windows, nil
}

func dashboardPlanUsage(value *CurrentPlanUsage) (*store.CursorDashboardPlanUsage, error) {
	if value == nil {
		return nil, nil
	}
	amounts := []int64{value.TotalSpendCents, value.IncludedSpendCents, value.BonusSpendCents, value.RemainingCents, value.LimitCents}
	for _, amount := range amounts {
		if amount < 0 || amount > basequery.JavaScriptMaxSafeInteger/10_000 {
			return nil, ErrDashboardProtocol
		}
	}
	return &store.CursorDashboardPlanUsage{
		TotalSpendMicros: value.TotalSpendCents * 10_000, IncludedSpendMicros: value.IncludedSpendCents * 10_000,
		BonusSpendMicros: value.BonusSpendCents * 10_000, RemainingMicros: value.RemainingCents * 10_000,
		LimitMicros: value.LimitCents * 10_000,
	}, nil
}

func (collector *DashboardCollector) readUsageEvents(ctx context.Context, startAtMS, endAtMS int64) ([]store.CursorDashboardUsageEvent, error) {
	byFingerprint := make(map[string]store.CursorDashboardUsageEvent)
	var received int64
	for pageNumber := int32(1); ; pageNumber++ {
		page, err := collector.client.GetFilteredUsageEvents(ctx, UsageEventsRequest{
			StartAtMS: startAtMS, EndAtMS: endAtMS, Page: pageNumber, PageSize: dashboardPageSize,
		})
		if err != nil {
			return nil, err
		}
		pageCount := int64(len(page.Events))
		if page.TotalCount < 0 || pageCount > int64(dashboardPageSize) || received+pageCount > page.TotalCount ||
			(pageCount == 0 && received < page.TotalCount) {
			return nil, ErrDashboardProtocol
		}
		for _, event := range page.Events {
			stored, err := dashboardStoreEvent(event, endAtMS)
			if err != nil {
				return nil, err
			}
			if existing, ok := byFingerprint[stored.EventFingerprint]; ok {
				existing.OccurrenceCount++
				byFingerprint[stored.EventFingerprint] = existing
			} else {
				byFingerprint[stored.EventFingerprint] = stored
			}
			received++
		}
		if received >= page.TotalCount {
			break
		}
	}
	events := make([]store.CursorDashboardUsageEvent, 0, len(byFingerprint))
	for _, event := range byFingerprint {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAtMS != events[j].OccurredAtMS {
			return events[i].OccurredAtMS < events[j].OccurredAtMS
		}
		return events[i].EventFingerprint < events[j].EventFingerprint
	})
	return events, nil
}

func dashboardStoreEvent(event DashboardUsageEvent, updatedAtMS int64) (store.CursorDashboardUsageEvent, error) {
	model := normalizeLabel(event.Model)
	conversationID := normalizeID(event.ConversationID)
	if event.OccurredAtMS < 0 || (event.Model != "" && model == "") || (event.ConversationID != "" && conversationID == "") ||
		!finiteDashboardCents(event.ChargedCents) || !finiteDashboardCents(event.CursorTokenFeeCents) ||
		(event.TokenBased && event.TokenUsage == nil) {
		return store.CursorDashboardUsageEvent{}, ErrDashboardProtocol
	}
	var input, output, cacheWrite, cacheRead int64
	if event.TokenUsage != nil {
		input, output = event.TokenUsage.InputTokens, event.TokenUsage.OutputTokens
		cacheWrite, cacheRead = event.TokenUsage.CacheWriteTokens, event.TokenUsage.CacheReadTokens
		if input < 0 || output < 0 || cacheWrite < 0 || cacheRead < 0 {
			return store.CursorDashboardUsageEvent{}, ErrDashboardProtocol
		}
	}
	reportedMicros := int64(math.Round(event.ChargedCents * 10_000))
	feeMicros := int64(math.Round(event.CursorTokenFeeCents * 10_000))
	fingerprint := digestString(strings.Join([]string{
		strconv.FormatInt(event.OccurredAtMS, 10), model, strconv.FormatInt(int64(event.Kind), 10),
		strconv.FormatBool(event.TokenBased),
		strconv.FormatInt(input, 10), strconv.FormatInt(output, 10), strconv.FormatInt(cacheWrite, 10),
		strconv.FormatInt(cacheRead, 10), strconv.FormatInt(reportedMicros, 10), strconv.FormatInt(feeMicros, 10),
		conversationID,
	}, "\x1f"))
	stored := store.CursorDashboardUsageEvent{
		EventFingerprint: fingerprint, OccurrenceCount: 1, OccurredAtMS: event.OccurredAtMS,
		Kind: int64(event.Kind), TokenBased: event.TokenBased, InputTokens: input, OutputTokens: output,
		CacheWriteTokens: cacheWrite, CacheReadTokens: cacheRead,
		ReportedChargeMicros: reportedMicros, CursorTokenFeeMicros: feeMicros, UpdatedAtMS: updatedAtMS,
	}
	if model != "" {
		stored.ModelKey = &model
	}
	if conversationID != "" {
		stored.ExternalSessionID = &conversationID
	}
	return stored, nil
}

func validateDashboardAggregate(events []store.CursorDashboardUsageEvent) error {
	var turns, input, output, cacheWrite, cacheRead, reported, tokenFee int64
	for _, event := range events {
		var ok bool
		turns, ok = addDashboardAggregate(turns, event.OccurrenceCount, 1)
		if !ok {
			return ErrDashboardProtocol
		}
		for _, component := range []struct {
			total *int64
			value int64
		}{
			{&input, event.InputTokens}, {&output, event.OutputTokens},
			{&cacheWrite, event.CacheWriteTokens}, {&cacheRead, event.CacheReadTokens},
			{&reported, event.ReportedChargeMicros}, {&tokenFee, event.CursorTokenFeeMicros},
		} {
			*component.total, ok = addDashboardAggregate(*component.total, component.value, event.OccurrenceCount)
			if !ok {
				return ErrDashboardProtocol
			}
		}
	}
	if estimate, _, ok := estimateCursorDashboardCost(events); ok && estimate > basequery.JavaScriptMaxSafeInteger {
		return ErrDashboardProtocol
	}
	return nil
}

func addDashboardAggregate(total, value, occurrences int64) (int64, bool) {
	if total < 0 || value < 0 || occurrences <= 0 ||
		value > basequery.JavaScriptMaxSafeInteger/occurrences {
		return 0, false
	}
	value *= occurrences
	if total > basequery.JavaScriptMaxSafeInteger-value {
		return 0, false
	}
	return total + value, true
}

func finiteNonnegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func finiteDashboardCents(value float64) bool {
	return finiteNonnegative(value) && value <= float64(basequery.JavaScriptMaxSafeInteger)/10_000
}

func (collector *DashboardCollector) recordFailure(ctx context.Context, atMS int64, cause error) error {
	failureCode := "read_failed"
	if errors.Is(cause, ErrDesktopAuthExpired) || errors.Is(cause, ErrDesktopAuthUnavailable) {
		failureCode = "auth_expired"
	} else if errors.Is(cause, ErrDashboardProtocol) {
		failureCode = "schema_incompatible"
	}
	if err := collector.writer.RecordCursorDashboardFailure(ctx, atMS, failureCode); err != nil {
		return fmt.Errorf("%w: persist dashboard failure", ErrDashboardProtocol)
	}
	collector.last = collector.config.Now()
	return nil
}
