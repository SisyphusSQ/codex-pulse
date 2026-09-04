package dashboardsummary

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

func TestDashboardSummaryReconcilesCompleteProviders(t *testing.T) {
	t.Parallel()

	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
		agentprovider.Cursor: completeUsage("cursor", "cursor-grok-4.6", 40, 4, "2026-07-01"),
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.grok_bot"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponseComplete ||
		response.Coverage.OverallState != CoverageComplete ||
		response.Coverage.KnownProviderCount != 3 ||
		numericValue(response.Totals.TotalTokens) != 160 ||
		numericValue(response.Totals.EstimatedUSDMicros) != 16 ||
		numericValue(response.Totals.ActiveProviderCount) != 3 {
		t.Fatalf("complete summary = %#v", response)
	}
	if len(response.Providers) != 3 || response.Providers[0].Provider != agentprovider.Codex ||
		response.Providers[1].Provider != agentprovider.Cursor ||
		response.Providers[2].Provider != agentprovider.Grok {
		t.Fatalf("providers = %#v", response.Providers)
	}
	if response.Providers[0].ModelCount != 1 || response.Providers[1].ModelCount != 1 ||
		response.Providers[2].ModelCount != 1 {
		t.Fatalf("provider model counts = %#v", response.Providers)
	}
	if len(response.Models) != 3 ||
		response.Models[0].DimensionKey != "codex:gpt-5" ||
		response.Models[1].DimensionKey != "cursor:cursor-grok-4.6" ||
		response.Models[2].DimensionKey != "grok:grok-4" {
		t.Fatalf("namespaced models = %#v", response.Models)
	}
	if len(response.Trend) != 1 || len(response.Trend[0].Shares) != 3 ||
		numericValue(response.Trend[0].TotalTokens) != 160 {
		t.Fatalf("trend = %#v", response.Trend)
	}
	if response.ActivityCoverageState != CoverageComplete ||
		response.ActivityRange.TimeZone != "Asia/Shanghai" ||
		len(response.Activity) != 1 || len(response.Activity[0].Shares) != 3 ||
		numericValue(response.Activity[0].TotalTokens) != 160 ||
		len(response.ActivityProviderCoverage) != 3 {
		t.Fatalf("activity = range=%#v state=%s points=%#v providers=%#v",
			response.ActivityRange,
			response.ActivityCoverageState,
			response.Activity,
			response.ActivityProviderCoverage,
		)
	}
	if len(response.Quotas) != 3 ||
		response.Quotas[1].Current.Windows[0].LimitID != "cursor.grok_bot" ||
		response.Quotas[2].Current.Windows[0].LimitID != "grok.credits" {
		t.Fatalf("quota identities = %#v", response.Quotas)
	}
	for _, slice := range response.Providers {
		if slice.ReportedUSDMicros != nil {
			t.Fatalf("reported cost leaked into comparable slice pointer for %s", slice.Provider)
		}
	}
}

func TestDashboardSummaryKeepsKnownSubtotalWhenOneProviderUnavailable(t *testing.T) {
	t.Parallel()

	usage := &usageStub{
		responses: map[string]usagecost.UsageCostResponse{
			agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
			agentprovider.Cursor: completeUsage("cursor", "composer", 40, 4, "2026-07-01"),
		},
		errors: map[string]error{agentprovider.Grok: basequery.NewUnavailableFailure(errors.New("grok down"))},
	}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
	}, errors: map[string]error{agentprovider.Grok: basequery.NewUnavailableFailure(errors.New("grok quota"))}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial ||
		response.Coverage.OverallState != CoveragePartial ||
		response.Coverage.KnownProviderCount != 2 ||
		numericValue(response.Totals.TotalTokens) != 140 ||
		response.Providers[2].CoverageState != CoverageUnavailable ||
		response.Providers[2].Totals.TotalTokens.Value != nil ||
		numericValue(response.Providers[0].Totals.TotalTokens) != 100 {
		t.Fatalf("partial summary = %#v", response)
	}
}

func TestDashboardSummaryKeepsCurrentSummaryWhenAnnualActivityIsPartial(t *testing.T) {
	t.Parallel()

	usage := &usageStub{
		responses: map[string]usagecost.UsageCostResponse{
			agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
			agentprovider.Cursor: completeUsage("cursor", "composer", 40, 4, "2026-07-01"),
			agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
		},
		activityErrors: map[string]error{
			agentprovider.Cursor: basequery.NewUnavailableFailure(errors.New("cursor annual index unavailable")),
		},
	}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}

	response, err := newSummaryService(t, usage, quota).DashboardSummary(
		context.Background(), summaryRequest(),
	)
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Coverage.OverallState != CoverageComplete ||
		numericValue(response.Totals.TotalTokens) != 160 ||
		response.ActivityCoverageState != CoveragePartial ||
		response.Meta.Status != basequery.ResponsePartial ||
		len(response.Activity) != 1 ||
		numericValue(response.Activity[0].TotalTokens) != 120 ||
		response.ActivityProviderCoverage[1].CoverageState != CoverageUnavailable {
		t.Fatalf("partial activity summary = %#v", response)
	}
}

func TestDashboardSummaryKnownEmptyDoesNotLookUnavailable(t *testing.T) {
	t.Parallel()

	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "gpt-5", 0, 0, "2026-07-01"),
		agentprovider.Cursor: completeUsage("cursor", "composer", 0, 0, "2026-07-01"),
		agentprovider.Grok:   completeUsage("grok", "grok-4", 0, 0, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Coverage.OverallState != CoverageEmpty ||
		response.Meta.Status != basequery.ResponseComplete ||
		numericValue(response.Totals.TotalTokens) != 0 ||
		numericValue(response.Totals.ActiveProviderCount) != 0 {
		t.Fatalf("empty summary = %#v", response)
	}
}

func TestDashboardSummaryKeepsUnknownCostOutOfCompleteDistribution(t *testing.T) {
	t.Parallel()

	codex := completeUsage("codex", "gpt-5", 100, 10, "2026-07-01")
	cursor := completeUsage("cursor", "composer", 40, 0, "2026-07-01")
	unknownCost, err := basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	cursor.Totals.EstimatedUSDMicros = unknownCost
	cursor.Trend[0].Totals.EstimatedUSDMicros = unknownCost
	grok := completeUsage("grok", "grok-4", 20, 2, "2026-07-01")
	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex: codex, agentprovider.Cursor: cursor, agentprovider.Grok: grok,
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Coverage.CostState != CoveragePartial ||
		response.Coverage.TokenState != CoverageComplete ||
		response.Coverage.OverallState != CoveragePartial ||
		response.Coverage.KnownProviderCount != 3 ||
		response.Coverage.KnownCostProviderCount != 2 ||
		response.Meta.Status != basequery.ResponsePartial ||
		numericValue(response.Totals.EstimatedUSDMicros) != 12 ||
		response.Distribution[1].EstimatedUSDMicros.Value != nil {
		t.Fatalf("unknown cost summary = %#v", response)
	}
	if response.Providers[1].ReportedUSDMicros != nil {
		t.Fatalf("estimated and reported cost were mixed")
	}
}

func TestDashboardSummaryKeepsPartialEstimateSeparateFromReportedCost(t *testing.T) {
	t.Parallel()

	cursor := completeUsage("cursor", "composer", 40, 4, "2026-07-01")
	unpriced, err := basequery.KnownNumeric(2, basequery.NumericCount)
	if err != nil {
		t.Fatal(err)
	}
	reported, err := basequery.KnownNumeric(27, basequery.NumericMicroUSD)
	if err != nil {
		t.Fatal(err)
	}
	reportedSource := "cursor.dashboard"
	cursor.Totals.UnpricedTurnCount = unpriced
	cursor.ReportedUSDMicros = &reported
	cursor.ReportedCostSource = &reportedSource
	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
		agentprovider.Cursor: cursor,
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Coverage.CostState != CoveragePartial ||
		response.Coverage.KnownCostProviderCount != 3 ||
		numericValue(response.Totals.EstimatedUSDMicros) != 16 ||
		response.Providers[1].CostState != CoveragePartial ||
		numericValue(response.Providers[1].Totals.EstimatedUSDMicros) != 4 ||
		numericValue(response.Distribution[1].EstimatedUSDMicros) != 4 {
		t.Fatalf("partial estimated cost summary = %#v", response)
	}
	if response.Providers[1].ReportedUSDMicros == nil ||
		numericValue(*response.Providers[1].ReportedUSDMicros) != 27 ||
		response.Providers[1].ReportedCostSource == nil ||
		*response.Providers[1].ReportedCostSource != reportedSource {
		t.Fatalf("Cursor reported cost identity = %#v", response.Providers[1])
	}
}

func TestDashboardSummaryKeepsMissingPartialTrendShareUnknown(t *testing.T) {
	t.Parallel()

	cursor := completeUsage("cursor", "composer", 40, 4, "2026-07-01")
	partial, err := basequery.NewResponseMeta(
		basequery.ResponsePartial, nil, []basequery.ErrorCode{basequery.ErrorPartial},
	)
	if err != nil {
		t.Fatal(err)
	}
	cursor.Meta = partial
	cursor.Trend = []usagecost.TrendPoint{}
	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
		agentprovider.Cursor: cursor,
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	response, err := newSummaryService(t, usage, quota).DashboardSummary(
		context.Background(), summaryRequest(),
	)
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if len(response.Trend) != 1 || len(response.Trend[0].Shares) != 3 ||
		response.Trend[0].Shares[1].TotalTokens.Value != nil ||
		response.Trend[0].Shares[1].EstimatedUSDMicros.Value != nil {
		t.Fatalf("partial missing trend share = %#v", response.Trend)
	}
}

func TestDashboardSummaryRejectsProviderRangeDriftWithoutDroppingKnownProviders(t *testing.T) {
	t.Parallel()

	cursor := completeUsage("cursor", "composer", 40, 4, "2026-07-01")
	cursor.Range.TimeZone = "UTC"
	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "gpt-5", 100, 10, "2026-07-01"),
		agentprovider.Cursor: cursor,
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	response, err := newSummaryService(t, usage, quota).DashboardSummary(
		context.Background(), summaryRequest(),
	)
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Coverage.KnownProviderCount != 2 ||
		response.Providers[1].CoverageState != CoverageUnavailable ||
		numericValue(response.Totals.TotalTokens) != 120 {
		t.Fatalf("range drift response = %#v", response)
	}
}

func TestDashboardSummaryFailsClosedWhenCompleteTrendDoesNotMatchTotals(t *testing.T) {
	t.Parallel()

	codex := completeUsage("codex", "gpt-5", 100, 10, "2026-07-01")
	codex.Trend[0].Totals.TotalTokens, _ = basequery.KnownNumeric(99, basequery.NumericTokens)
	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  codex,
		agentprovider.Cursor: completeUsage("cursor", "composer", 40, 4, "2026-07-01"),
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	response, err := newSummaryService(t, usage, quota).DashboardSummary(
		context.Background(), summaryRequest(),
	)
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponseUnavailable ||
		response.Coverage.OverallState != CoverageUnavailable ||
		response.Totals.TotalTokens.Value != nil {
		t.Fatalf("mismatched complete trend did not fail closed: %#v", response)
	}
}

func TestDashboardSummaryDoesNotMergeSameRawModelAcrossProviders(t *testing.T) {
	t.Parallel()

	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsage("codex", "grok-4", 10, 1, "2026-07-01"),
		agentprovider.Cursor: completeUsage("cursor", "grok-4", 30, 3, "2026-07-01"),
		agentprovider.Grok:   completeUsage("grok", "grok-4", 20, 2, "2026-07-01"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	response, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if len(response.Models) != 3 {
		t.Fatalf("models = %#v, want 3 namespaced rows", response.Models)
	}
	seen := map[string]bool{}
	for _, item := range response.Models {
		if item.DimensionKey != item.Provider+":grok-4" {
			t.Fatalf("dimension = %s, want provider-namespaced grok-4", item.DimensionKey)
		}
		if seen[item.DimensionKey] {
			t.Fatalf("duplicate model key %s", item.DimensionKey)
		}
		seen[item.DimensionKey] = true
	}
}

func TestDashboardSummaryAlignsTimezoneDateBoundaries(t *testing.T) {
	t.Parallel()

	usage := &usageStub{responses: map[string]usagecost.UsageCostResponse{
		agentprovider.Codex:  completeUsageInZone("codex", "gpt-5", 5, 1, "2026-03-08", "America/New_York"),
		agentprovider.Cursor: completeUsageInZone("cursor", "composer", 7, 1, "2026-03-08", "America/New_York"),
		agentprovider.Grok:   completeUsageInZone("grok", "grok-4", 9, 1, "2026-03-08", "America/New_York"),
	}}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	request := Request{
		Range: basequery.LocalDateRange{
			StartDate: "2026-03-08", EndDateExclusive: "2026-03-09", TimeZone: "America/New_York",
		},
		ActivityRange: basequery.LocalDateRange{
			StartDate: "2025-03-09", EndDateExclusive: "2026-03-09", TimeZone: "America/New_York",
		},
		Granularity: usagecost.TrendHour,
	}
	response, err := service.DashboardSummary(context.Background(), request)
	if err != nil {
		t.Fatalf("DashboardSummary() error = %v", err)
	}
	if response.Range.TimeZone != "America/New_York" ||
		response.Range.StartAtMS >= response.Range.EndAtMS ||
		response.ReportingTimeZone != "America/New_York" ||
		numericValue(response.Totals.TotalTokens) != 21 {
		t.Fatalf("timezone summary = %#v", response.Range)
	}
}

func TestDashboardSummaryRejectsStaleGenerationWrites(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	usage := &usageStub{
		responses: map[string]usagecost.UsageCostResponse{
			agentprovider.Codex:  completeUsage("codex", "gpt-5", 1, 1, "2026-07-01"),
			agentprovider.Cursor: completeUsage("cursor", "composer", 2, 1, "2026-07-01"),
			agentprovider.Grok:   completeUsage("grok", "grok-4", 3, 1, "2026-07-01"),
		},
		fresh: map[string]usagecost.UsageCostResponse{
			agentprovider.Codex: completeUsage("codex", "gpt-5", 99, 1, "2026-07-01"),
		},
		blocks: map[string]<-chan struct{}{agentprovider.Codex: block},
	}
	quota := &quotaStub{responses: map[string]runtimeinfo.QuotaCurrentResponse{
		agentprovider.Codex:  completeQuota("codex", "codex"),
		agentprovider.Cursor: completeQuota("cursor", "cursor.models"),
		agentprovider.Grok:   completeQuota("grok", "grok.credits"),
	}}
	service := newSummaryService(t, usage, quota)
	started := make(chan struct{})
	var first Response
	var firstErr error
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		close(started)
		first, firstErr = service.DashboardSummary(context.Background(), summaryRequest())
	}()
	<-started
	waitUntil(t, func() bool { return usage.active.Load() > 0 })
	service.InvalidateUsage()
	second, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("fresh DashboardSummary() error = %v", err)
	}
	close(block)
	wait.Wait()
	if firstErr != nil {
		t.Fatalf("stale DashboardSummary() error = %v", firstErr)
	}
	if numericValue(first.Totals.TotalTokens) != 6 {
		t.Fatalf("stale totals = %d, want 6", numericValue(first.Totals.TotalTokens))
	}
	if numericValue(second.Totals.TotalTokens) != 104 {
		t.Fatalf("fresh totals = %d, want 104", numericValue(second.Totals.TotalTokens))
	}
	cached, err := service.DashboardSummary(context.Background(), summaryRequest())
	if err != nil {
		t.Fatalf("cached DashboardSummary() error = %v", err)
	}
	if numericValue(cached.Totals.TotalTokens) != 104 || cached.UsageGeneration != second.UsageGeneration {
		t.Fatalf("stale generation overwrote fresh cache: first=%#v cached=%#v", first.Totals, cached.Totals)
	}
	if numericValue(cached.Providers[1].Totals.TotalTokens) != 2 ||
		numericValue(cached.Providers[2].Totals.TotalTokens) != 3 {
		t.Fatalf("single-provider refresh zeroed other clients: %#v", cached.Providers)
	}
}

func TestInvalidateUsageAndQuotaAdvancesOneAtomicGenerationSnapshot(t *testing.T) {
	t.Parallel()

	service := newSummaryService(t, &usageStub{}, &quotaStub{})
	service.usageCache[usageCacheKey{provider: agentprovider.Codex}] = cachedUsage{}
	service.quotaCache[agentprovider.Codex] = cachedQuota{}

	service.InvalidateUsageAndQuota()
	usageGeneration, quotaGeneration := service.generations()
	if usageGeneration != 1 || quotaGeneration != 1 ||
		len(service.usageCache) != 0 || len(service.quotaCache) != 0 {
		t.Fatalf(
			"combined invalidation = generations(%d, %d), cache sizes(%d, %d)",
			usageGeneration, quotaGeneration, len(service.usageCache), len(service.quotaCache),
		)
	}
}

func TestReconcileCompleteFailsClosedOnMismatchedTotals(t *testing.T) {
	t.Parallel()

	tokens, err := basequery.KnownNumeric(10, basequery.NumericTokens)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := basequery.KnownNumeric(99, basequery.NumericTokens)
	if err != nil {
		t.Fatal(err)
	}
	slices := []ProviderSlice{
		{Provider: "codex", CoverageState: CoverageComplete, Totals: usagecost.UsageTotals{TotalTokens: tokens}},
		{Provider: "cursor", CoverageState: CoverageComplete, Totals: usagecost.UsageTotals{TotalTokens: tokens}},
		{Provider: "grok", CoverageState: CoverageComplete, Totals: usagecost.UsageTotals{TotalTokens: tokens}},
	}
	if err := reconcileComplete(
		slices, Totals{TotalTokens: wrong}, nil, CoverageComplete, CoverageUnknown,
	); err == nil {
		t.Fatal("reconcileComplete() error = nil, want mismatch")
	}
}

func TestDashboardSummaryValidatesRangeAndGranularity(t *testing.T) {
	t.Parallel()

	service := newSummaryService(t, &usageStub{}, &quotaStub{})
	_, err := service.DashboardSummary(context.Background(), Request{
		Range: basequery.LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "UTC"},
	})
	if !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("missing granularity error = %v", err)
	}
	_, err = service.DashboardSummary(context.Background(), Request{
		Range:       basequery.LocalDateRange{StartDate: "2026-07-01", EndDateExclusive: "2026-07-02"},
		Granularity: usagecost.TrendDay,
	})
	if !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("missing timezone error = %v", err)
	}
	_, err = service.DashboardSummary(context.Background(), Request{
		Range: basequery.LocalDateRange{
			StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "UTC",
		},
		Granularity: usagecost.TrendWeek,
	})
	if !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("unsupported dashboard granularity error = %v", err)
	}
	_, err = service.DashboardSummary(context.Background(), Request{
		Range: basequery.LocalDateRange{
			StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "UTC",
		},
		ActivityRange: basequery.LocalDateRange{
			StartDate: "2025-07-02", EndDateExclusive: "2026-07-02", TimeZone: "Asia/Shanghai",
		},
		Granularity: usagecost.TrendDay,
	})
	if !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("activity timezone drift error = %v", err)
	}
}

type usageStub struct {
	mu                sync.Mutex
	responses         map[string]usagecost.UsageCostResponse
	fresh             map[string]usagecost.UsageCostResponse
	errors            map[string]error
	activityResponses map[string]usagecost.UsageCostResponse
	activityErrors    map[string]error
	blocks            map[string]<-chan struct{}
	calls             map[string]int
	active            atomic.Int32
}

func (stub *usageStub) UsageCost(
	_ context.Context,
	request usagecost.UsageCostRequest,
) (usagecost.UsageCostResponse, error) {
	stub.active.Add(1)
	defer stub.active.Add(-1)
	provider := request.Provider.Provider
	callKey := provider
	if request.TokenTotalsOnly {
		callKey += ":activity"
	}
	stub.mu.Lock()
	if stub.calls == nil {
		stub.calls = map[string]int{}
	}
	stub.calls[callKey]++
	call := stub.calls[callKey]
	block := stub.blocks[callKey]
	if block == nil && !request.TokenTotalsOnly {
		block = stub.blocks[provider]
	}
	stub.mu.Unlock()
	if call == 1 && block != nil {
		<-block
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if request.TokenTotalsOnly {
		if err := stub.activityErrors[provider]; err != nil {
			return usagecost.UsageCostResponse{}, err
		}
		if response, ok := stub.activityResponses[provider]; ok {
			return response, nil
		}
	}
	if err := stub.errors[provider]; err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	if call > 1 {
		if response, ok := stub.fresh[provider]; ok {
			return response, nil
		}
	}
	response, ok := stub.responses[provider]
	if !ok {
		return usagecost.UsageCostResponse{}, basequery.NewUnavailableFailure(errors.New("missing provider"))
	}
	if request.TokenTotalsOnly {
		rangeValue, err := basequery.NormalizeLocalDateRange(request.Range, maxRangeDays)
		if err != nil {
			return usagecost.UsageCostResponse{}, err
		}
		response.Range = *rangeValue
		response.ReportingTimeZone = rangeValue.TimeZone
	}
	return response, nil
}

func (stub *usageStub) setResponse(provider string, response usagecost.UsageCostResponse) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.responses == nil {
		stub.responses = map[string]usagecost.UsageCostResponse{}
	}
	stub.responses[provider] = response
}

func (stub *usageStub) clearBlock(provider string) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	delete(stub.blocks, provider)
}

type quotaStub struct {
	responses map[string]runtimeinfo.QuotaCurrentResponse
	errors    map[string]error
}

func (stub *quotaStub) QuotaCurrent(
	_ context.Context,
	scope agentprovider.Scope,
	_ int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	if err := stub.errors[scope.Provider]; err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	response, ok := stub.responses[scope.Provider]
	if !ok {
		return runtimeinfo.QuotaCurrentResponse{}, basequery.NewUnavailableFailure(errors.New("missing quota"))
	}
	return response, nil
}

func newSummaryService(t *testing.T, usage UsageQuery, quota QuotaQuery) *Service {
	t.Helper()
	service, err := New(usage, quota, func() time.Time {
		return time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func summaryRequest() Request {
	return Request{
		Range: basequery.LocalDateRange{
			StartDate: "2026-07-01", EndDateExclusive: "2026-07-02", TimeZone: "Asia/Shanghai",
		},
		ActivityRange: basequery.LocalDateRange{
			StartDate: "2025-07-02", EndDateExclusive: "2026-07-02", TimeZone: "Asia/Shanghai",
		},
		Granularity: usagecost.TrendDay,
	}
}

func completeUsage(provider, model string, tokens, cost int64, day string) usagecost.UsageCostResponse {
	return completeUsageInZone(provider, model, tokens, cost, day, "Asia/Shanghai")
}

func completeUsageInZone(
	provider, model string,
	tokens, cost int64,
	day, timeZone string,
) usagecost.UsageCostResponse {
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		panic(err)
	}
	totals := knownTotals(tokens, cost)
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		panic(err)
	}
	startTime, err := time.ParseInLocation("2006-01-02", day, location)
	if err != nil {
		panic(err)
	}
	startMS := startTime.UnixMilli()
	endMS := startTime.AddDate(0, 0, 1).UnixMilli()
	start, err := basequery.KnownNumeric(startMS, basequery.NumericMilliseconds)
	if err != nil {
		panic(err)
	}
	end, err := basequery.KnownNumeric(endMS, basequery.NumericMilliseconds)
	if err != nil {
		panic(err)
	}
	display := model
	return usagecost.UsageCostResponse{
		ProviderContext: agentprovider.Context{EffectiveProvider: provider},
		Meta:            meta,
		Range: basequery.UTCTimeRange{
			StartAtMS: startMS, EndAtMS: endMS, TimeZone: timeZone,
		},
		ReportingTimeZone: timeZone,
		Totals:            totals,
		Trend: []usagecost.TrendPoint{{
			Key: day, StartAtMS: start, EndAtMS: end, Totals: totals,
		}},
		Models: []usagecost.UsageModelItem{{
			DimensionKey: model,
			Model:        usagecost.AttributionValue{DisplayName: &display, Confidence: "high", Source: provider},
			Totals:       totals,
		}},
	}
}

func completeQuota(provider, limitID string) runtimeinfo.QuotaCurrentResponse {
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		panic(err)
	}
	remaining := 40.0
	return runtimeinfo.QuotaCurrentResponse{
		Meta:            meta,
		ProviderContext: agentprovider.Context{EffectiveProvider: provider},
		Current: quotaquery.CurrentResponse{
			Version: "quota-current-v1", AccountScope: provider, EvaluatedAtMS: 1_751_400_000_000,
			Windows: []quotaquery.CurrentWindow{{
				WindowKind: "weekly", LimitID: limitID, RemainingPercent: &remaining, Freshness: "fresh",
			}},
		},
	}
}

func knownTotals(tokens, cost int64) usagecost.UsageTotals {
	must := func(value basequery.NumericValue, err error) basequery.NumericValue {
		if err != nil {
			panic(err)
		}
		return value
	}
	return usagecost.UsageTotals{
		TurnCount:          must(basequery.KnownNumeric(1, basequery.NumericCount)),
		InputTokens:        must(basequery.KnownNumeric(tokens, basequery.NumericTokens)),
		CachedInputTokens:  must(basequery.KnownNumeric(0, basequery.NumericTokens)),
		OutputTokens:       must(basequery.KnownNumeric(0, basequery.NumericTokens)),
		ReasoningTokens:    must(basequery.KnownNumeric(0, basequery.NumericTokens)),
		TotalTokens:        must(basequery.KnownNumeric(tokens, basequery.NumericTokens)),
		EstimatedUSDMicros: must(basequery.KnownNumeric(cost, basequery.NumericMicroUSD)),
		PricedTurnCount:    must(basequery.KnownNumeric(1, basequery.NumericCount)),
		UnpricedTurnCount:  must(basequery.KnownNumeric(0, basequery.NumericCount)),
		FirstActivityAtMS:  must(basequery.KnownNumeric(1_751_328_000_000, basequery.NumericMilliseconds)),
		LastActivityAtMS:   must(basequery.KnownNumeric(1_751_400_000_000, basequery.NumericMilliseconds)),
	}
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
