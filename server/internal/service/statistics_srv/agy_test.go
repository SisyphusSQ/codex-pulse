package statistics_srv

import (
	"net/url"
	"strconv"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestAGYReportingUsageCacheThroughputAnnualAndProviderIsolation(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	stats.now = func() time.Time { return now }

	evAt := now.Add(-2 * time.Hour).UnixMilli()
	turnStart, turnEnd := evAt-2000, evAt
	snapshot := reportingv1.SessionSnapshot{
		Provider:       "agy",
		HomeID:         "home-agy",
		SessionID:      "agy-session-stats-1",
		Revision:       1,
		CollectedAtMS:  evAt + 5000,
		Title:          "AGY 统计会话",
		ProjectID:      reportingv1.Key("agy", "workspace"),
		ProjectName:    "workspace",
		CreatedAtMS:    &turnStart,
		LastActiveAtMS: &evAt,
		SourceKind:     "agy_local",
		SessionKind:    "session",
		SessionRole:    "subagent",
		Complete:       true,
		Contributions: []reportingv1.Contribution{
			{
				ObservedAtMS:    &evAt,
				Model:           new("gemini-4-carbon"),
				InputTokens:     new(int64(1000)),
				CachedTokens:    new(int64(750)),
				OutputTokens:    new(int64(200)),
				ReasoningTokens: new(int64(80)),
				TotalTokens:     new(int64(1200)),
				CostMicroUSD:    new(int64(2575)),
				PricingVersion:  new("agy-api-2026-11-25:phase1"),
				CostStatus:      "known",
				PricingMode:     "event_cost",
			},
		},
		CacheUsage: &reportingv1.CacheUsageCapsule{
			Version:           1,
			Basis:             "lifetime_cached_input",
			InputTokens:       new(int64(1000)),
			CachedInputTokens: new(int64(750)),
		},
		Throughput: &reportingv1.ThroughputCapsule{
			Version: 1,
			Basis:   "closed_turn_lifetime_output",
			Measures: reportingv1.ThroughputMeasures{
				OutputTokens:     new(int64(200)),
				ActiveDurationMS: new(int64(2000)),
				IncludedTurns:    1,
				CoverageKnown:    true,
				Status:           "complete",
				DurationSource:   "step_metadata",
			},
			TurnsTotal: 1,
			RecentTurns: []reportingv1.ThroughputTurn{{
				Key:         reportingv1.Key("agy", "agy-session-stats-1", "turn", "0"),
				StartedAtMS: &turnStart,
				EndedAtMS:   &turnEnd,
				Measures: reportingv1.ThroughputMeasures{
					OutputTokens:     new(int64(200)),
					ActiveDurationMS: new(int64(2000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
			}},
		},
	}
	fixStatisticsIDs(&snapshot)

	// Upload from all 3 devices to confirm multi-device deduplication.
	for _, client := range clients {
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
	}

	rangeStart := strconv.FormatInt(now.Add(-24*time.Hour).UnixMilli(), 10)
	rangeEnd := strconv.FormatInt(now.Add(24*time.Hour).UnixMilli(), 10)
	q := statisticsTestQuery(t, url.Values{
		"provider":    {"agy"},
		"start_at_ms": {rangeStart},
		"end_at_ms":   {rangeEnd},
	})
	usage, err := stats.Usage(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, usage.Totals.TotalTokens, "1200")
	decimal(t, usage.Totals.CostMicroUSD, "2575")
	decimal(t, usage.CacheHitRate.BasisPoints, "7500")
	if len(usage.Models) != 1 || usage.Models[0].Provider != "agy" || len(usage.Totals.PricingVersions) != 1 || usage.Totals.PricingVersions[0] != "agy-api-2026-11-25:phase1" {
		t.Fatalf("unexpected AGY usage models/versions: %+v", usage)
	}

	// Single-provider empty filter also computes range cache hit rate for AGY.
	unfiltered := statisticsTestQuery(t, url.Values{"start_at_ms": {rangeStart}, "end_at_ms": {rangeEnd}})
	usageAll, err := stats.Usage(t.Context(), admin, unfiltered)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, usageAll.CacheHitRate.BasisPoints, "7500")

	// Sessions list and detail preserve cache hit rate and step_metadata TPS.
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("expected 1 deduplicated AGY session, got %d err=%v", len(list.Items), err)
	}
	decimal(t, list.Items[0].CacheHitRate.BasisPoints, "7500")
	decimal(t, list.Items[0].Throughput.AverageOutputMilliTPS, "100000")
	if list.Items[0].Throughput.DurationSource != "step_metadata" {
		t.Fatalf("expected step_metadata duration source, got %+v", list.Items[0].Throughput)
	}
	if list.Items[0].SessionRole == nil || *list.Items[0].SessionRole != "subagent" {
		t.Fatalf("expected AGY session_role=subagent in list, got %v", list.Items[0].SessionRole)
	}

	detail, err := stats.Session(t.Context(), admin, q, list.Items[0].ID)
	if err != nil || len(detail.ThroughputTurns.Items) != 1 {
		t.Fatalf("AGY session detail failed: turns=%d err=%v", len(detail.ThroughputTurns.Items), err)
	}
	if detail.Session.SessionRole == nil || *detail.Session.SessionRole != "subagent" {
		t.Fatalf("expected AGY session_role=subagent in detail, got %v", detail.Session.SessionRole)
	}

	// Annual heatmap and activity include AGY.
	summary, err := stats.Summary(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, summary.HeatmapTotals.TotalTokens, "1200")
	if summary.HeatmapActivity.ActiveDays == nil || *summary.HeatmapActivity.ActiveDays != "1" {
		t.Fatalf("unexpected AGY annual activity: %+v", summary.HeatmapActivity)
	}

	// Provider isolation: Codex filter must not return AGY data.
	q.Provider = "codex"
	other, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(other.Items) != 0 {
		t.Fatalf("AGY leaked into Codex scope: %+v err=%v", other.Items, err)
	}
}
