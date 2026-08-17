package cursorprovider

import (
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestCursorStaticPricingEstimatesDocumentedModelTokenClasses(t *testing.T) {
	model := "gpt-5.6-sol"
	events := []store.CursorDashboardUsageEvent{{
		OccurrenceCount: 1, OccurredAtMS: 1_787_000_000_000, ModelKey: &model, TokenBased: true,
		InputTokens: 10, CacheWriteTokens: 30, CacheReadTokens: 40, OutputTokens: 20,
	}}
	estimated, version, ok := estimateCursorDashboardCost(events)
	if !ok || estimated != 858 || version != cursorPricingVersion {
		t.Fatalf("estimate = %d, %q, %t; want 858, %q, true", estimated, version, ok, cursorPricingVersion)
	}
}

func TestCursorStaticPricingDoesNotPresentUnknownModelAsPriced(t *testing.T) {
	model := "future-unlisted-model"
	events := []store.CursorDashboardUsageEvent{{
		OccurrenceCount: 1, OccurredAtMS: 1_787_000_000_000, ModelKey: &model, TokenBased: true,
		InputTokens: 10, OutputTokens: 20,
	}}
	if estimated, version, ok := estimateCursorDashboardCost(events); ok || estimated != 0 || version != "" {
		t.Fatalf("unknown model estimate = %d, %q, %t", estimated, version, ok)
	}
}

func TestCursorStaticPricingAppliesGrok46LaunchDiscountByEventTime(t *testing.T) {
	model := "grok-4.6"
	events := []store.CursorDashboardUsageEvent{{
		OccurrenceCount: 1,
		OccurredAtMS:    time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).UnixMilli(),
		ModelKey:        &model, TokenBased: true,
		InputTokens: 1_000_000,
	}}
	estimated, _, ok := estimateCursorDashboardCost(events)
	if !ok || estimated != 1_000_000 {
		t.Fatalf("discounted Grok 4.6 estimate = %d, %t; want 1000000, true", estimated, ok)
	}
}
