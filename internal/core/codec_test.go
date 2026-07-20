package core

import (
	"errors"
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
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
