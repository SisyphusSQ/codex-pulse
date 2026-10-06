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
