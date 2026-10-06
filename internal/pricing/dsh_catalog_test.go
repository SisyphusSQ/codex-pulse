package pricing

import (
	"testing"
	"time"
)

func TestDSHCalendarAndDollarPrices(t *testing.T) {
	for _, tc := range []struct {
		at, period string
		input      int64
	}{
		{"2026-09-28T08:59:59+08:00", "off_peak", 150000}, {"2026-09-28T09:00:00+08:00", "peak", 300000},
		{"2026-09-28T12:00:00+08:00", "off_peak", 150000}, {"2026-09-28T14:00:00+08:00", "peak", 300000},
		{"2026-09-28T18:00:00+08:00", "off_peak", 150000}, {"2026-09-26T10:00:00+08:00", "off_peak", 150000},
		{"2026-10-06T10:00:00+08:00", "off_peak", 150000},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		rate, ok := DSHRateAt("deepseek-account", "deepseek-flash", at.UnixMilli())
		if !ok || rate.Period != tc.period || rate.InputMicros != tc.input {
			t.Fatalf("%s: %+v %v", tc.at, rate, ok)
		}
		cost, ok := EstimateDSHCost(rate, 1_000_000, 1_000_000, 0, 1_000_000)
		if !ok || cost != rate.InputMicros+rate.CachedMicros+rate.OutputMicros {
			t.Fatalf("cost %d %v", cost, ok)
		}
	}
}
func TestDSHPriceDoesNotGuessHistoryRoutesOrCacheWrites(t *testing.T) {
	at := time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		provider, model string
		at              int64
	}{
		{"third-party", "deepseek-flash", at}, {"deepseek-official", "other", at},
		{"deepseek-official", "deepseek-flash", dshFlashEffective - 1},
		{"deepseek-account", "deepseek-flash", time.Date(2027, 1, 4, 2, 0, 0, 0, time.UTC).UnixMilli()},
	} {
		if _, ok := DSHRateAt(tc.provider, tc.model, tc.at); ok {
			t.Fatalf("unexpected price %+v", tc)
		}
	}
	rate, _ := DSHRateAt("deepseek-official", "deepseek-flash", at)
	if _, ok := EstimateDSHCost(rate, 1, 1, 1, 1); ok {
		t.Fatal("invented cache write price")
	}
}

func TestDSHCodexUsesHistoricalOpenAIPriceAndExclusiveTokenBuckets(t *testing.T) {
	at := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC).UnixMilli()
	rate, ok := DSHRateAt("openai-codex", "gpt-6.1-sol", at)
	if !ok || rate.PricingVersion() != "openai-api-2026-09-29" || rate.SourceURL != builtinPricingURL || rate.Period != "" {
		t.Fatalf("OpenAI price evidence lost: %+v, %v", rate, ok)
	}
	// input=227 是未缓存输入，不能再减去 16256 缓存读取；output 已含推理。
	if cost, ok := EstimateDSHCost(rate, 227, 16256, 0, 63); !ok || cost != 2710 {
		t.Fatal("exclusive buckets were mispriced", cost, ok)
	}
	if cost, ok := EstimateDSHCost(rate, 0, 0, 0, 0); !ok || cost != 0 {
		t.Fatal("known zero lost", cost, ok)
	}
	if _, ok := EstimateDSHCost(rate, 100, 300, 1, 200); ok {
		t.Fatal("cache write price was invented")
	}
	for _, tc := range []struct {
		provider, model string
		at              int64
	}{
		{"openai-codex", "gpt-6.1-sol", builtinPricing20260929EffectiveAtMS - 1},
		{"openai-codex", "unknown-model", at},
		{"openai-codex", "gpt-6.1-sol-fast", at},
		{"openai-codex", "GPT-6.1-SOL", at},
		{"third-party", "gpt-6.1-sol", at},
		{"deepseek-account", "gpt-6.1-sol", at},
		{"openai-codex", "gpt-6.1-sol", -1},
	} {
		if _, ok := DSHRateAt(tc.provider, tc.model, tc.at); ok {
			t.Fatalf("unexpected price: %+v", tc)
		}
	}
	before, _ := DSHRateAt("openai-codex", "gpt-5.6-sol", builtinPricing20260905VerifiedAtMS-1)
	after, _ := DSHRateAt("openai-codex", "gpt-5.6-sol", builtinPricing20260905VerifiedAtMS)
	if before.InputMicros != 5_000_000 || after.InputMicros != 4_000_000 || before.PricingVersion() == after.PricingVersion() {
		t.Fatal("current price rewrote historical price", before, after)
	}
	peak, ok := DSHRateAt("openai-codex", "gpt-6.1-sol", time.Date(2027, 1, 4, 2, 0, 0, 0, time.UTC).UnixMilli())
	if !ok || peak != rate {
		t.Fatal("OpenAI used DeepSeek calendar", peak, ok)
	}
}
