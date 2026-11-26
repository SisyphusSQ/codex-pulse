package agyprovider

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQueryServiceUsageTokensPricingTimezoneSessionsAndProjects(t *testing.T) {
	t.Parallel()

	repo := openPulseRepo(t)
	now := time.Date(2026, 11, 25, 18, 0, 0, 0, time.UTC)

	// 2026-11-25T17:00:00Z = Asia/Shanghai 2026-11-26 01:00:00（验证跨日时区分桶）
	ev1Time := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC).UnixMilli()
	ev2Time := time.Date(2026, 11, 25, 17, 0, 0, 0, time.UTC).UnixMilli()

	mCarbon := "gemini-4-carbon-high"
	mUnknown := "future-unknown-model"
	projKey := digestString("agy:/work/pulse")
	sess1 := "11111111-1111-4111-8111-111111111111"
	sess2 := "22222222-2222-4222-8222-222222222222"

	err := repo.ReplaceAGYSnapshot(context.Background(), store.AGYSnapshot{
		Generation:    now.UnixMilli(),
		CollectedAtMS: now.UnixMilli(),
		Sources: []store.CursorSourceStatus{{
			Provider: "agy", SourceKey: SourceTrajectories, SourceType: "sqlite",
			State: "available", CoverageState: "exact", CheckpointKind: "snapshot",
			RowCount: 2, LastAttemptAtMS: now.UnixMilli(), LastSuccessAtMS: new(now.UnixMilli()), UpdatedAtMS: now.UnixMilli(),
		}},
		Sessions: []store.AGYSession{
			{
				ExternalSessionID: sess1, DisplayTitle: "Session One", TitleSource: "agy_annotation",
				SessionRole: "main", ProjectKey: projKey, ProjectDisplayName: "pulse", ProjectSource: "workspace",
				CreatedAtMS: ev1Time, LastActivityAtMS: ev1Time, ModelKey: &mCarbon, RequestCount: 1,
				CoverageState: "exact", UpdatedAtMS: now.UnixMilli(),
			},
			{
				ExternalSessionID: sess2, DisplayTitle: "Session Two", TitleSource: "agy_summary",
				SessionRole: "subagent", ParentExternalSessionID: &sess1,
				ProjectKey: projKey, ProjectDisplayName: "pulse", ProjectSource: "inherited_parent",
				CreatedAtMS: ev2Time, LastActivityAtMS: ev2Time, ModelKey: &mUnknown, RequestCount: 1,
				CoverageState: "exact", UpdatedAtMS: now.UnixMilli(),
			},
		},
		UsageEvents: []store.AGYUsageEvent{
			{
				EventID: digestString("e1"), ExternalSessionID: sess1, StepIndex: 1, OccurredAtMS: ev1Time,
				ModelKey: &mCarbon, UncachedInputTokens: 1_000_000, CacheReadTokens: 4_000_000,
				OutputTokens: 500_000, ReasoningTokens: 100_000, TotalTokens: 5_500_000,
				CacheReadKnown: true, ReasoningKnown: true, TotalKnown: true,
				StartedAtMS: &ev1Time, EndedAtMS: new(ev1Time + 2000),
				Provenance: "agy_step_metadata", UpdatedAtMS: now.UnixMilli(),
			},
			{
				EventID: digestString("e2"), ExternalSessionID: sess2, StepIndex: 1, OccurredAtMS: ev2Time,
				ModelKey: &mUnknown, UncachedInputTokens: 100, CacheReadTokens: 0,
				OutputTokens: 50, ReasoningTokens: 0, TotalTokens: 150,
				CacheReadKnown: true, ReasoningKnown: true, TotalKnown: true,
				StartedAtMS: &ev2Time, EndedAtMS: new(ev2Time + 1000),
				Provenance: "agy_step_metadata", UpdatedAtMS: now.UnixMilli(),
			},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceAGYSnapshot: %v", err)
	}

	collector, err := NewCollector(repo, Config{
		BaseRoot:       filepath.Join(t.TempDir(), "empty"),
		MinimumRefresh: time.Hour,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	service, err := NewQueryService(collector, repo)
	if err != nil {
		t.Fatalf("NewQueryService: %v", err)
	}

	// 1. Asia/Shanghai 跨日分桶 + Uncached Input 映射 + 未知模型保持 unpriced
	resp, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
		Range: basequery.LocalDateRange{
			StartDate:        "2026-11-25",
			EndDateExclusive: "2026-11-27",
			TimeZone:         "Asia/Shanghai",
		},
		Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost: %v", err)
	}
	if resp.ProviderContext.EffectiveProvider != agentprovider.AGY {
		t.Fatalf("provider = %s", resp.ProviderContext.EffectiveProvider)
	}
	if len(resp.Trend) != 2 || resp.Trend[0].Key != "2026-11-25" || resp.Trend[1].Key != "2026-11-26" {
		t.Fatalf("expected 2 Asia/Shanghai daily buckets, got %+v", resp.Trend)
	}
	// Day 1 (sess1): input = 1M + 4M = 5M, cached = 4M, output = 0.5M, total = 5.5M
	// Cost = 1M*$2 + 4M*$0.10 + 0.5M*$10 = $2 + $0.40 + $5 = $7.40 = 7_400_000 micros
	d1 := resp.Trend[0].Totals
	if *d1.InputTokens.Value != 5_000_000 || *d1.CachedInputTokens.Value != 4_000_000 ||
		*d1.OutputTokens.Value != 500_000 || *d1.ReasoningTokens.Value != 100_000 ||
		*d1.TotalTokens.Value != 5_500_000 || *d1.EstimatedUSDMicros.Value != 7_400_000 {
		t.Fatalf("unexpected day1 totals: %+v", d1)
	}
	// Overall includes 1 unpriced event -> EstimatedUSDMicros is unknown, UnpricedTurnCount = 1
	if resp.Totals.EstimatedUSDMicros.Value != nil || *resp.Totals.UnpricedTurnCount.Value != 1 ||
		len(resp.UnpricedReasons) != 1 || resp.UnpricedReasons[0].Reason != pricing.CostReasonMissingPriceComponent {
		t.Fatalf("unexpected overall unpriced semantics: totals=%+v reasons=%+v", resp.Totals, resp.UnpricedReasons)
	}

	// 2. ListSessions + CacheHitRate (4M / 5M = 8000 bps = 80%)
	sessList, err := service.ListSessions(context.Background(), basequery.Request{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessList.Items) != 2 {
		t.Fatalf("sessions len = %d, want 2", len(sessList.Items))
	}
	var s1Item usagecost.SessionItem
	for _, it := range sessList.Items {
		if it.SessionID == sess1 {
			s1Item = it
		}
	}
	if s1Item.CacheHitRate == nil || s1Item.CacheHitRate.Value == nil || *s1Item.CacheHitRate.Value != 8000 {
		t.Fatalf("expected 8000 bps cache hit rate, got %+v", s1Item.CacheHitRate)
	}

	// 3. SessionDetail
	detail, err := service.SessionDetail(context.Background(), usagecost.SessionDetailRequest{
		Provider:  agentprovider.Scope{Provider: agentprovider.AGY},
		SessionID: sess1,
	})
	if err != nil {
		t.Fatalf("SessionDetail: %v", err)
	}
	if len(detail.Turns) != 1 || detail.Turns[0].PricingStatus != usagecost.SessionTurnPricingPriced {
		t.Fatalf("unexpected session detail turns: %+v", detail.Turns)
	}

	// 4. ListProjects & ProjectDetail
	projList, err := service.ListProjects(context.Background(), basequery.Request{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
		TimeRange: &basequery.LocalDateRange{
			StartDate:        "2026-11-25",
			EndDateExclusive: "2026-11-27",
			TimeZone:         "Asia/Shanghai",
		},
	})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projList.Items) != 1 || *projList.Items[0].SessionCount.Value != 2 {
		t.Fatalf("unexpected projects: %+v", projList.Items)
	}
	projDetail, err := service.ProjectDetail(context.Background(), usagecost.ProjectDetailRequest{
		Provider:     agentprovider.Scope{Provider: agentprovider.AGY},
		DimensionKey: projKey,
		Range: basequery.LocalDateRange{
			StartDate:        "2026-11-25",
			EndDateExclusive: "2026-11-27",
			TimeZone:         "Asia/Shanghai",
		},
	})
	if err != nil {
		t.Fatalf("ProjectDetail: %v", err)
	}
	if len(projDetail.Sessions) != 2 || len(projDetail.Models) != 2 {
		t.Fatalf("unexpected project detail: sessions=%d models=%d", len(projDetail.Sessions), len(projDetail.Models))
	}
}
