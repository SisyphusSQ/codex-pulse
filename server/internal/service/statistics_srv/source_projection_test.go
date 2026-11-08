package statistics_srv

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
)

func sourceProjectionRead(q statistics_dto.StatisticsQuery, legacy bool) *statisticsRead {
	r := &statisticsRead{q: q, metadata: map[string]reporting_do.Session{"session": {ID: "session", Provider: "codex", Complete: true, CollectedAtMS: q.EndAtMS}}, total: newStatisticsAggregate(), providerSeen: map[string]bool{}, projection: "source-usage"}
	if legacy {
		r.heatmapOnly = true
		r.days = map[string]*statisticsAggregate{}
	}
	return r
}

func TestSourceTotalsKeepDailyPricingAndCoverage(t *testing.T) {
	q := statisticsTestQuery(t, url.Values{"start_date": {"2026-03-08"}, "end_date_exclusive": {"2026-03-10"}, "time_zone": {"America/New_York"}})
	first := q.StartAtMS
	second := time.UnixMilli(first).In(q.Location).AddDate(0, 0, 1).UnixMilli()
	// 两个当地自然日分别舍入；不能把来源总量改成整个范围只舍入一次。
	base := reporting_do.Usage{SessionKey: "session", ObservedAtMS: new(first), Model: new("model"), InputTokens: new(int64(1)), CachedTokens: new(int64(0)), OutputTokens: new(int64(0)), ReasoningTokens: new(int64(0)), TotalTokens: new(int64(1)), PricingVersion: new("historical"), PricingMode: "codex_model_sum", InputPrice: new(int64(600000)), CachedPrice: new(int64(0)), OutputPrice: new(int64(0)), CostStatus: "known"}
	facts := []reporting_do.Usage{base, base}
	facts[1].ObservedAtMS = new(second)
	for _, model := range []string{"", "model", "unknown", "absent"} {
		q.Model = model
		t.Run("model="+model, func(t *testing.T) {
			old, current := sourceProjectionRead(q, true), sourceProjectionRead(q, false)
			for _, row := range facts {
				old.usage(row)
				current.usage(row)
			}
			a, _ := old.total.finish(false)
			b, _ := current.total.finish(false)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(old.coverage(time.UnixMilli(q.EndAtMS)), current.coverage(time.UnixMilli(q.EndAtMS))) {
				t.Fatal("daily rounding or coverage changed")
			}
			if model == "" || model == "model" {
				decimal(t, b.CostMicroUSD, "2")
			}
			// 巨大整数、真零、未知值、无时间事实及范围外事实沿用旧路径的精确语义。
			extra := []reporting_do.Usage{base, base, base, base, base}
			extra[0].InputTokens = new(int64(9007199254740993))
			extra[0].TotalTokens = extra[0].InputTokens
			extra[1].InputTokens = new(int64(0))
			extra[1].TotalTokens = extra[1].InputTokens
			extra[2].InputTokens = nil
			extra[2].TotalTokens = nil
			extra[2].Model = nil
			extra[2].CostStatus = "unknown"
			extra[3].ObservedAtMS = nil
			extra[4].ObservedAtMS = new(q.EndAtMS)
			for _, row := range extra {
				old.usage(row)
				current.usage(row)
			}
			a, _ = old.total.finish(false)
			b, _ = current.total.finish(false)
			if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(old.coverage(time.UnixMilli(q.EndAtMS)), current.coverage(time.UnixMilli(q.EndAtMS))) {
				t.Fatal("totals or coverage changed")
			}
			if len(current.days) != 0 {
				t.Fatal("source totals built unused day buckets")
			}
		})
	}
}

func BenchmarkSourceTotalsProjection(b *testing.B) {
	q := statistics_dto.StatisticsQuery{StartAtMS: 0, EndAtMS: 366 * 86400000, Location: time.UTC}
	rows := make([]reporting_do.Usage, 50000)
	for i := range rows {
		rows[i] = reporting_do.Usage{SessionKey: "session", ObservedAtMS: new(int64(i) * 600000), Model: new("model"), InputTokens: new(int64(i)), CachedTokens: new(int64(0)), OutputTokens: new(int64(0)), ReasoningTokens: new(int64(0)), TotalTokens: new(int64(i)), PricingVersion: new("historical"), PricingMode: "codex_model_sum", InputPrice: new(int64(1500000)), CachedPrice: new(int64(150000)), OutputPrice: new(int64(10000000)), CostStatus: "known"}
	}
	for _, legacy := range []bool{true, false} {
		b.Run(fmt.Sprintf("legacy=%v", legacy), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r := sourceProjectionRead(q, legacy)
				for _, row := range rows {
					r.usage(row)
				}
				totals, _ := r.total.finish(false)
				if totals.TotalTokens == nil || *totals.TotalTokens != "1249975000" {
					b.Fatal("wrong total")
				}
			}
		})
	}
}
