package dshprovider

import (
	"errors"
	"strings"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestDSHUnknownFactsPropagateToActivityAndTotals(t *testing.T) {
	event := store.DSHUsageEvent{ExternalSessionID: "synthetic", OccurredAtMS: 1000, InputTokens: 20, OutputTokens: 10}
	totals, priced := totalsForUsageEvents([]store.DSHUsageEvent{event})
	if priced || totals.TotalTokens.Value != nil || totals.InputTokens.Value != nil || totals.OutputTokens.Value == nil {
		t.Fatal("missing optional facts became known")
	}
	activity := activityDistribution([]store.DSHUsageEvent{event}, basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 2000, TimeZone: "UTC"})
	if activity.Timeline[0].Metrics.TotalTokens.Value != nil || activity.WeekdayHours[0].Metrics.TotalTokens.Value != nil {
		t.Fatal("activity fabricated total")
	}
	snapshot := store.DSHSnapshot{Sources: []store.CursorSourceStatus{{State: "partial", CoverageState: "partial"}}}
	meta := snapshotMeta(snapshot, nil)
	if meta.Status != basequery.ResponsePartial || meta.Version != basequery.ContractVersion || len(meta.Issues) != 1 {
		t.Fatal("partial source presented as complete")
	}
	event.TotalKnown, event.CacheReadKnown, event.CacheWriteKnown = true, true, true
	event.TotalTokens = 30
	totals, _ = totalsForUsageEvents([]store.DSHUsageEvent{event})
	markMissingUsage(&totals, []store.DSHSession{{CoverageState: "partial"}})
	if totals.TotalTokens.Value != nil || totals.OutputTokens.Value != nil {
		t.Fatal("a request with no usage was omitted from the aggregate")
	}
}

func TestDSHPricingUsesIndependentRequestStart(t *testing.T) {
	// Completion crosses the peak boundary, but official price follows request start.
	start := time.Date(2026, 10, 8, 0, 59, 59, 0, time.UTC).UnixMilli()
	end := start + 2000
	model := "deepseek-flash"
	event := store.DSHUsageEvent{ModelProvider: "deepseek-account", ModelKey: &model, StartedAtMS: &start, OccurredAtMS: end, InputTokens: 1_000_000, CacheReadKnown: true, CacheWriteKnown: true}
	if cost, ok := estimateEvent(event); !ok || cost != 150_000 {
		t.Fatal("completion time changed price", cost, ok)
	}
	event.StartedAtMS = nil
	if _, ok := estimateEvent(event); ok {
		t.Fatal("missing retry start fabricated peak price")
	}
	parsed, err := parseSession(t.Context(), strings.NewReader(fixture(false)), 4, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.usage[0].StartedAtMS == nil || parsed.usage[1].StartedAtMS != nil {
		t.Fatal("retry reused first attempt start")
	}
}

func TestDSHInvalidConfigurationBackendIsUnavailable(t *testing.T) {
	service, err := NewDisabledQueryService()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.snapshot(t.Context()); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatal("disabled backend fabricated empty data", err)
	}
	if _, err := service.RefreshIfDue(t.Context()); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatal("disabled backend scanned files", err)
	}
}

func TestDSHProjectRangeAndFilterKeepSelectedFacts(t *testing.T) {
	service, err := NewDisabledQueryService()
	if err != nil {
		t.Fatal(err)
	}
	service.disabled, service.localRefreshing = false, true
	service.snapshotCache = &store.DSHSnapshot{
		Generation: 1,
		Sessions: []store.DSHSession{
			{ExternalSessionID: "a", ProjectKey: "first", CreatedAtMS: 1000, LastActivityAtMS: 3000, RequestCount: 2, CoverageState: "exact"},
			{ExternalSessionID: "b", ProjectKey: "second", CreatedAtMS: 1000, LastActivityAtMS: 3000, RequestCount: 1, CoverageState: "exact"},
		},
		UsageEvents: []store.DSHUsageEvent{
			{ExternalSessionID: "a", OccurredAtMS: 1500, InputTokens: 10, TotalTokens: 10, TotalKnown: true, CacheReadKnown: true, CacheWriteKnown: true},
			{ExternalSessionID: "a", OccurredAtMS: 2500, InputTokens: 20, TotalTokens: 20, TotalKnown: true, CacheReadKnown: true, CacheWriteKnown: true},
			{ExternalSessionID: "b", OccurredAtMS: 2500, InputTokens: 30, TotalTokens: 30, TotalKnown: true, CacheReadKnown: true, CacheWriteKnown: true},
		},
	}
	rangeValue := &basequery.UTCTimeRange{StartAtMS: 2000, EndAtMS: 4000, TimeZone: "UTC"}
	list, err := service.ListProjects(t.Context(), basequery.Request{ExactTimeRange: rangeValue,
		Filters: []basequery.FilterTerm{{Field: "projectId", Operator: basequery.FilterEqual, Values: []string{"first"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || *list.GlobalTotals.TotalTokens.Value != 50 || *list.MatchedTotals.TotalTokens.Value != 20 || *list.Items[0].Totals.TurnCount.Value != 1 {
		t.Fatal("project filter mixed global facts or lifetime request count")
	}
	detail, err := service.ProjectDetail(t.Context(), usagecost.ProjectDetailRequest{DimensionKey: "first", ExactRange: rangeValue})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Sessions) != 1 || detail.Sessions[0].Totals.TotalTokens.Value == nil || *detail.Sessions[0].Totals.TotalTokens.Value != 20 || *detail.Sessions[0].Totals.TurnCount.Value != 1 {
		t.Fatal("range session facts became unknown because of lifetime requests")
	}
}

func TestDSHCodexPricingMetadataMixedModelsAndUnknownRetry(t *testing.T) {
	at := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC).UnixMilli()
	codex := store.DSHUsageEvent{EventID: "codex", ExternalSessionID: "a", ModelProvider: "openai-codex", ModelKey: new("gpt-6.1-sol"), StartedAtMS: new(at - 100), OccurredAtMS: at, InputTokens: 100, CachedReadTokens: 300, OutputTokens: 200, TotalTokens: 600, TotalKnown: true, CacheReadKnown: true, CacheWriteKnown: true, ReasoningKnown: true, ReasoningTokens: 100}
	cost, rate, ok := priceEvent(codex)
	if !ok || cost != 2230 || rate.PricingVersion() != "openai-api-2026-09-29" {
		t.Fatal("reasoning or cached input counted twice", cost, rate, ok)
	}
	service, err := NewDisabledQueryService()
	if err != nil {
		t.Fatal(err)
	}
	service.disabled, service.localRefreshing = false, true
	service.snapshotCache = &store.DSHSnapshot{Generation: 1, Sessions: []store.DSHSession{{ExternalSessionID: "a", ProjectKey: "project", CreatedAtMS: at - 1000, LastActivityAtMS: at + 1000, RequestCount: 1, CoverageState: "exact"}}, UsageEvents: []store.DSHUsageEvent{codex}}
	request := usagecost.UsageCostRequest{ExactRange: &basequery.UTCTimeRange{StartAtMS: at - 1000, EndAtMS: at + 2000, TimeZone: "UTC"}, Granularity: usagecost.TrendDay}
	usage, err := service.UsageCost(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if usage.PricingSource == nil || *usage.PricingSource != "https://developers.openai.com/api/docs/pricing" || len(usage.PricingVersions) != 1 || usage.PricingVersions[0] != rate.PricingVersion() || usage.Totals.EstimatedUSDMicros.Value == nil || *usage.Totals.EstimatedUSDMicros.Value != 2230 {
		t.Fatal("OpenAI metadata lost", usage)
	}
	detail, err := service.SessionDetail(t.Context(), usagecost.SessionDetailRequest{SessionID: "a"})
	if err != nil || len(detail.Turns) != 1 || detail.Turns[0].PricingVersion == nil || *detail.Turns[0].PricingVersion != rate.PricingVersion() {
		t.Fatal("turn price version lost", detail, err)
	}
	project, err := service.ProjectDetail(t.Context(), usagecost.ProjectDetailRequest{DimensionKey: "project", ExactRange: request.ExactRange})
	if err != nil || len(project.PricingVersions) != 1 || project.PricingVersions[0] != rate.PricingVersion() {
		t.Fatal("project price version lost", project, err)
	}
	deepseek := codex
	deepseek.EventID, deepseek.ModelProvider, deepseek.ModelKey = "deepseek", "deepseek-account", new("deepseek-flash")
	service.snapshotCache.UsageEvents = append(service.snapshotCache.UsageEvents, deepseek)
	service.snapshotCache.Sessions[0].RequestCount = 2
	usage, err = service.UsageCost(t.Context(), request)
	if err != nil || usage.PricingSource != nil || len(usage.PricingVersions) != 2 {
		t.Fatal("mixed sources mislabeled", usage, err)
	}
	unknown := codex
	unknown.EventID = "retry"
	unknown.StartedAtMS = nil
	service.snapshotCache.UsageEvents = append(service.snapshotCache.UsageEvents, unknown)
	service.snapshotCache.Sessions[0].RequestCount = 3
	usage, err = service.UsageCost(t.Context(), request)
	if err != nil || usage.Totals.EstimatedUSDMicros.Value != nil || len(usage.PricingVersions) != 2 || *usage.Totals.UnpricedTurnCount.Value != 1 {
		t.Fatal("unknown retry fabricated a complete cost", usage, err)
	}
	codex.CacheWriteKnown = false
	if _, _, ok := priceEvent(codex); ok {
		t.Fatal("missing cache bucket invented")
	}
}
