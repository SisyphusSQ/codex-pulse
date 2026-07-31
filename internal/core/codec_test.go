package core

import (
	"errors"
	"strings"
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

// 测试 encodeResponse 在 partial 响应中保留真实零和 unknown presence。
func TestEncodeResponsePreservesNumericPresenceAndPartialStatus(t *testing.T) {
	zero, err := basequery.KnownNumeric(0, basequery.NumericTokens)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponsePartial, nil, []basequery.ErrorCode{basequery.ErrorPartial})
	if err != nil {
		t.Fatal(err)
	}

	got := &corev1.UsageCostResponse{}
	err = EncodeResponse(usagecost.UsageCostResponse{
		Meta:   meta,
		Totals: withUsageTotals(t, zero, unknown),
	}, got)
	if err != nil {
		t.Fatalf("encodeResponse() error = %v", err)
	}
	if got.Meta == nil || got.Meta.Status != "partial" || len(got.Meta.Issues) != 1 ||
		got.Meta.Issues[0].Code != "partial" {
		t.Fatalf("meta = %#v, want partial with one partial issue", got.Meta)
	}
	if got.Totals == nil || got.Totals.TotalTokens == nil || got.Totals.TotalTokens.Value == nil ||
		*got.Totals.TotalTokens.Value != 0 || got.Totals.TotalTokens.UnknownReason != nil {
		t.Fatalf("total_tokens = %#v, want known present zero", got.GetTotals().GetTotalTokens())
	}
	if got.Totals.EstimatedUsdMicros == nil || got.Totals.EstimatedUsdMicros.Value != nil ||
		got.Totals.EstimatedUsdMicros.UnknownReason == nil ||
		*got.Totals.EstimatedUsdMicros.UnknownReason != "not_computed" {
		t.Fatalf("estimated_usd_micros = %#v, want unknown not_computed", got.Totals.EstimatedUsdMicros)
	}
}

// 测试 encodeResponse 拒绝 value 与 unknown_reason 同时出现的非法数值。
func TestEncodeResponseRejectsInvalidNumericPresence(t *testing.T) {
	zero := int64(0)
	reason := basequery.UnknownUnavailable
	response := usagecost.UsageCostResponse{
		Totals: usagecost.UsageTotals{
			TotalTokens: basequery.NumericValue{
				Value: &zero, Unit: basequery.NumericTokens, UnknownReason: &reason,
			},
		},
	}
	err := EncodeResponse(response, &corev1.UsageCostResponse{})
	if !errors.Is(err, ErrProtoMapping) {
		t.Fatalf("encodeResponse() error = %v, want ErrProtoMapping", err)
	}
}

func TestEncodeResponsePreservesQuotaPacePresenceAndHistory(t *testing.T) {
	t.Parallel()

	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	windowStartAtMS := int64(0)
	resetsAtMS := int64(6_000_000)
	windowMinutes := int64(100)
	windowGeneration := resetsAtMS
	usedPercent := 0.0
	remainingPercent := 100.0
	elapsedPercent := 42.0
	paceDeltaPP := -42.0
	previousRemaining := 80.0
	historyRemaining := 75.0
	exhaustAtMS := int64(5_000_000)
	leadBeforeResetMS := resetsAtMS - exhaustAtMS
	target := &corev1.QuotaPaceResponse{}
	if err := EncodeResponse(runtimeinfo.QuotaPaceResponse{
		Meta: meta,
		Pace: quotaquery.PaceResponse{
			Version:       quotaquery.PaceContractVersion,
			AccountScope:  store.QuotaAccountScopeDefault,
			EvaluatedAtMS: 2_520_000,
			Windows: []quotaquery.PaceWindow{{
				WindowKind:       store.QuotaWindowPrimary,
				LimitID:          "codex",
				WindowStartAtMS:  &windowStartAtMS,
				ResetsAtMS:       &resetsAtMS,
				WindowMinutes:    &windowMinutes,
				WindowGeneration: &windowGeneration,
				UsedPercent:      &usedPercent,
				RemainingPercent: &remainingPercent,
				ElapsedPercent:   &elapsedPercent,
				PaceDeltaPP:      &paceDeltaPP,
				Forecast: quotaquery.PaceForecast{
					State:             quotaquery.PaceForecastAtRisk,
					Method:            quotaquery.PaceForecastMethodRecentTheilSen,
					ExhaustAtMS:       &exhaustAtMS,
					LeadBeforeResetMS: &leadBeforeResetMS,
					EvidenceCount:     4,
					EvidenceSpanMS:    1_800_000,
				},
				CurrentPoints: []quotaquery.PacePoint{{
					ObservedAtMS: 2_520_000, ElapsedPercent: 42,
					UsedPercent: 0, RemainingPercent: 100,
				}},
				HistoricalCycles: []quotaquery.PaceCycle{{
					WindowGeneration: 1, Complete: true,
					Points: []quotaquery.PacePoint{{
						ObservedAtMS: 1, ElapsedPercent: 42,
						UsedPercent: 20, RemainingPercent: 80,
					}},
				}},
				HistoryBand: []quotaquery.PaceHistoryBandPoint{{
					ElapsedPercent: 42, MedianRemaining: 75,
					MinimumRemaining: 70, MaximumRemaining: 80, CycleCount: 4,
				}},
				HistoryCycleCount:               4,
				PreviousRemainingAtElapsed:      &previousRemaining,
				HistoryMedianRemainingAtElapsed: &historyRemaining,
			}},
		},
	}, target); err != nil {
		t.Fatalf("EncodeResponse(QuotaPaceResponse) error = %v", err)
	}
	if target.Meta == nil || target.Pace == nil || len(target.Pace.Windows) != 1 {
		t.Fatalf("QuotaPaceResponse envelope = %#v", target)
	}
	window := target.Pace.Windows[0]
	if window.UsedPercent == nil || window.GetUsedPercent() != 0 ||
		window.RemainingPercent == nil || window.GetRemainingPercent() != 100 ||
		window.Forecast == nil || window.Forecast.ExhaustAtMs == nil ||
		window.Forecast.GetLeadBeforeResetMs() != leadBeforeResetMS ||
		len(window.CurrentPoints) != 1 || len(window.HistoricalCycles) != 1 ||
		len(window.HistoryBand) != 1 ||
		window.PreviousRemainingAtElapsed == nil ||
		window.HistoryMedianRemainingAtElapsed == nil {
		t.Fatalf("QuotaPaceResponse window = %#v", window)
	}
}

// 测试 EncodeResponse 保留活动分布中的 Token、会话数和星期小时语义。
func TestEncodeResponseMapsActivityDistribution(t *testing.T) {
	t.Parallel()

	tokens, err := basequery.KnownNumeric(60, basequery.NumericTokens)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := basequery.KnownNumeric(2, basequery.NumericCount)
	if err != nil {
		t.Fatal(err)
	}
	start, err := basequery.KnownNumeric(100, basequery.NumericMilliseconds)
	if err != nil {
		t.Fatal(err)
	}
	end, err := basequery.KnownNumeric(200, basequery.NumericMilliseconds)
	if err != nil {
		t.Fatal(err)
	}
	unknownCost, err := basequery.UnknownNumeric(
		basequery.NumericMicroUSD,
		basequery.UnknownNotComputed,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := &corev1.UsageCostResponse{}
	if err := EncodeResponse(usagecost.UsageCostResponse{
		Totals: withUsageTotals(t, tokens, unknownCost),
		ActivityDistribution: &usagecost.ActivityDistribution{
			TimelineGranularity:   usagecost.TrendHour,
			TimelineBucketMinutes: 180,
			Timeline: []usagecost.ActivityTimelinePoint{{
				StartAtMS: start, EndAtMS: end,
				Metrics: usagecost.ActivityMetrics{TotalTokens: tokens, SessionCount: sessions},
			}},
			WeekdayHours: []usagecost.ActivityWeekdayHourPoint{{
				Weekday: 1, Hour: 9,
				Metrics: usagecost.ActivityMetrics{TotalTokens: tokens, SessionCount: sessions},
			}},
		},
	}, target); err != nil {
		t.Fatalf("EncodeResponse(activity) error = %v", err)
	}
	if target.ActivityDistribution == nil {
		t.Fatal("activity distribution presence was lost")
	}
	activity := target.GetActivityDistribution()
	if activity.TimelineGranularity != "hour" || activity.TimelineBucketMinutes != 180 ||
		len(activity.Timeline) != 1 ||
		len(activity.WeekdayHours) != 1 || activity.WeekdayHours[0].Weekday != 1 ||
		activity.WeekdayHours[0].Hour != 9 ||
		activity.Timeline[0].Metrics.GetTotalTokens().GetValue() != 60 ||
		activity.Timeline[0].Metrics.GetSessionCount().GetValue() != 2 {
		t.Fatalf("activity distribution = %#v", activity)
	}
}

// 测试 EncodeResponse 把 Session 自适应趋势粒度和点映射到 Protobuf contract。
func TestEncodeResponseMapsAdaptiveSessionTrend(t *testing.T) {
	t.Parallel()

	tokens, err := basequery.KnownNumeric(10, basequery.NumericTokens)
	if err != nil {
		t.Fatal(err)
	}
	unknownCost, err := basequery.UnknownNumeric(
		basequery.NumericMicroUSD,
		basequery.UnknownNotComputed,
	)
	if err != nil {
		t.Fatal(err)
	}
	timestamp, err := basequery.KnownNumeric(100, basequery.NumericMilliseconds)
	if err != nil {
		t.Fatal(err)
	}
	response := usagecost.SessionDetailResponse{
		Item: usagecost.SessionItem{
			LastActivityAt: timestamp,
			Totals:         withUsageTotals(t, tokens, unknownCost),
		},
		TrendGranularity: usagecost.TrendHour,
		Trend: []usagecost.TrendPoint{{
			Key: "1970-01-01T00:00", StartAtMS: timestamp, EndAtMS: timestamp,
			Totals: withUsageTotals(t, tokens, unknownCost),
		}},
	}
	target := &corev1.SessionDetailResponse{}
	if err := EncodeResponse(response, target); err != nil {
		t.Fatalf("EncodeResponse(SessionDetailResponse) error = %v", err)
	}
	encoded, err := protojson.Marshal(target)
	if err != nil {
		t.Fatalf("protojson.Marshal(SessionDetailResponse) error = %v", err)
	}
	if !strings.Contains(string(encoded), `"trendGranularity":"hour"`) ||
		!strings.Contains(string(encoded), `"trend":[`) {
		t.Fatalf("SessionDetailResponse contract = %s", encoded)
	}
}

// 测试 Session generation fallback 经 Proto 保持空趋势与明确降级原因，不伪造小时口径。
func TestEncodeResponsePreservesUnavailableFallbackSessionTrend(t *testing.T) {
	t.Parallel()

	reason := usagecost.DegradedRollupMissing
	unknownTokens, err := basequery.UnknownNumeric(
		basequery.NumericTokens,
		basequery.UnknownUnavailable,
	)
	if err != nil {
		t.Fatal(err)
	}
	unknownCost, err := basequery.UnknownNumeric(
		basequery.NumericMicroUSD,
		basequery.UnknownUnavailable,
	)
	if err != nil {
		t.Fatal(err)
	}
	unknownTime, err := basequery.UnknownNumeric(
		basequery.NumericMilliseconds,
		basequery.UnknownUnavailable,
	)
	if err != nil {
		t.Fatal(err)
	}
	target := &corev1.SessionDetailResponse{}
	if err := EncodeResponse(usagecost.SessionDetailResponse{
		TrendGranularity: "",
		Trend:            make([]usagecost.TrendPoint, 0),
		DegradedReason:   &reason,
		Item: usagecost.SessionItem{
			LastActivityAt: unknownTime,
			Totals:         withUsageTotals(t, unknownTokens, unknownCost),
		},
	}, target); err != nil {
		t.Fatalf("EncodeResponse(fallback SessionDetailResponse) error = %v", err)
	}
	if len(target.Trend) != 0 || target.TrendGranularity != "" ||
		target.DegradedReason == nil ||
		target.GetDegradedReason() != string(usagecost.DegradedRollupMissing) {
		t.Fatalf("fallback SessionDetailResponse = %#v", target)
	}
	encoded, err := protojson.Marshal(target)
	if err != nil {
		t.Fatalf("protojson.Marshal(fallback SessionDetailResponse) error = %v", err)
	}
	if strings.Contains(string(encoded), `"trend"`) ||
		strings.Contains(string(encoded), `"trendGranularity"`) {
		t.Fatalf("fallback SessionDetailResponse exposed trend fields: %s", encoded)
	}
}

func withUsageTotals(
	t testing.TB,
	totalTokens basequery.NumericValue,
	estimatedUSD basequery.NumericValue,
) usagecost.UsageTotals {
	t.Helper()
	unknownTokens, err := basequery.UnknownNumeric(basequery.NumericTokens, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	unknownCount, err := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	unknownTime, err := basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	return usagecost.UsageTotals{
		TurnCount: unknownCount, InputTokens: unknownTokens, CachedInputTokens: unknownTokens,
		OutputTokens: unknownTokens, ReasoningTokens: unknownTokens, TotalTokens: totalTokens,
		EstimatedUSDMicros: estimatedUSD, PricedTurnCount: unknownCount, UnpricedTurnCount: unknownCount,
		FirstActivityAtMS: unknownTime, LastActivityAtMS: unknownTime,
	}
}
