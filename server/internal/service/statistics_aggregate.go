package service

import (
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
)

// 以下类型仅为一次查询的私有计算状态，不跨层传输、不持久化或序列化。
type decimalSum struct {
	value   big.Int
	seen    bool
	missing bool
}

func (s *decimalSum) add(v *int64) {
	if v == nil {
		s.missing = true
		return
	}
	s.value.Add(&s.value, big.NewInt(*v))
	s.seen = true
}
func (s *decimalSum) text(knownEmpty bool) *string {
	if !s.seen && !knownEmpty {
		return nil
	}
	return new(s.value.String())
}
func (s *decimalSum) exact() *big.Int {
	if !s.seen || s.missing {
		return nil
	}
	return &s.value
}

type statisticsPriceGroup struct {
	input, cached, output, reasoning decimalSum
	rates                            pricing.Rates
	facts                            int64
	unknown                          bool
}
type statisticsAggregate struct {
	codexRange                                                bool
	input, cached, write, output, reasoning, tokens, reported decimalSum
	eventCost                                                 big.Int
	eventKnown                                                bool
	cursor                                                    map[string]*decimalSum
	prices                                                    map[string]*statisticsPriceGroup
	sessions                                                  map[string]bool
	invocations, unpriced, unknown, untimed                   int64
	versions                                                  map[string]bool
}

func newStatisticsAggregate() *statisticsAggregate {
	return &statisticsAggregate{cursor: make(map[string]*decimalSum), prices: make(map[string]*statisticsPriceGroup), sessions: make(map[string]bool), versions: make(map[string]bool)}
}
func pricePart(v *int64) string {
	if v == nil {
		return "null"
	}
	return strconv.FormatInt(*v, 10)
}
func valueString(v *string, empty string) string {
	if v == nil || *v == "" {
		return empty
	}
	return *v
}
func statisticsDay(at int64, location *time.Location) time.Time {
	t := time.UnixMilli(at).In(location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, location)
}
func (g *statisticsAggregate) usage(row do.Usage, location *time.Location, provider string) {
	g.sessions[row.SessionKey] = true
	g.input.add(row.InputTokens)
	g.cached.add(row.CachedTokens)
	g.write.add(row.CacheWriteTokens)
	g.output.add(row.OutputTokens)
	g.reasoning.add(row.ReasoningTokens)
	g.tokens.add(row.TotalTokens)
	g.reported.add(row.ReportedChargeMicroUSD)
	if row.TotalTokens == nil || row.InputTokens == nil || row.OutputTokens == nil || (row.PricingMode == "codex_model_sum" && (row.CachedTokens == nil || row.ReasoningTokens == nil)) || (row.PricingMode == "cursor_range_sum" && (row.CachedTokens == nil || row.CacheWriteTokens == nil)) {
		g.unknown++
	}
	if row.PricingVersion != nil {
		g.versions[*row.PricingVersion] = true
	}
	switch row.PricingMode {
	case "codex_model_sum":
		bucket := statisticsDay(*row.ObservedAtMS, location).Format(time.DateOnly)
		if g.codexRange {
			bucket = "range"
		}
		key := strings.Join([]string{provider, bucket, valueString(row.Model, "unknown"), valueString(row.PricingVersion, ""), pricePart(row.InputPrice), pricePart(row.CachedPrice), pricePart(row.OutputPrice), row.CostStatus}, "\x00")
		p := g.prices[key]
		if p == nil {
			p = &statisticsPriceGroup{rates: pricing.Rates{InputMicrosPerMillion: row.InputPrice, CachedInputMicrosPerMillion: row.CachedPrice, OutputMicrosPerMillion: row.OutputPrice}}
			g.prices[key] = p
		}
		p.input.add(row.InputTokens)
		p.cached.add(row.CachedTokens)
		p.output.add(row.OutputTokens)
		p.reasoning.add(row.ReasoningTokens)
		p.facts++
		p.unknown = p.unknown || row.CostStatus != "known" || row.PricingVersion == nil
	case "cursor_range_sum":
		group := g.cursor[provider]
		if group == nil {
			group = &decimalSum{}
			g.cursor[provider] = group
		}
		if row.CostStatus != "known" || row.PricingVersion == nil || !cursorNumerator(&group.value, row) {
			g.unpriced++
		} else {
			group.seen = true
		}
	default:
		if row.CostMicroUSD != nil && (row.CostStatus == "known" || row.CostStatus == "partial") {
			g.eventCost.Add(&g.eventCost, big.NewInt(*row.CostMicroUSD))
			g.eventKnown = true
		}
		if row.CostMicroUSD == nil || row.CostStatus != "known" {
			g.unpriced++
		}
	}
}
func cursorNumerator(total *big.Int, row do.Usage) bool {
	values := []*int64{row.InputTokens, row.CachedTokens, row.CacheWriteTokens, row.OutputTokens}
	rates := []*int64{row.InputPrice, row.CachedPrice, row.CacheWritePrice, row.OutputPrice}
	for i, v := range values {
		if v == nil || (*v > 0 && rates[i] == nil) {
			return false
		}
	}
	for i, v := range values {
		if *v != 0 && rates[i] != nil {
			total.Add(total, new(big.Int).Mul(big.NewInt(*v), big.NewInt(*rates[i])))
		}
	}
	return true
}
func (g *statisticsAggregate) call(row do.Invocation) {
	g.invocations++
	g.sessions[row.SessionKey] = true
}
func (g *statisticsAggregate) finish(knownEmpty bool) (vo.StatisticsTotals, int64) {
	cost := new(big.Int).Set(&g.eventCost)
	known := g.eventKnown
	unpriced := g.unpriced
	for _, group := range g.cursor {
		if group.seen {
			cost.Add(cost, pricing.RoundMicroUSD(&group.value))
			known = true
		}
	}
	for _, p := range g.prices {
		if p.unknown {
			unpriced += p.facts
			continue
		}
		result, err := pricing.CalculateExact(pricing.ExactUsage{InputTokens: p.input.exact(), CachedInputTokens: p.cached.exact(), OutputTokens: p.output.exact(), ReasoningTokens: p.reasoning.exact()}, p.rates)
		if err != nil || result.EstimatedUSDMicros == nil {
			unpriced += p.facts
			continue
		}
		cost.Add(cost, result.EstimatedUSDMicros)
		known = true
	}
	var amount *string
	if known || (knownEmpty && len(g.sessions) == 0) {
		amount = new(cost.String())
	}
	versions := make([]string, 0, len(g.versions))
	for version := range g.versions {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	status := "unknown"
	if amount != nil {
		status = "known"
		if unpriced > 0 {
			status = "partial"
		}
	}
	basis := "codex_day_model_version;cursor_range_sum;event_cost_sum"
	if g.codexRange {
		basis = "codex_model_version;cursor_range_sum;event_cost_sum"
	}
	return vo.StatisticsTotals{CostBasis: basis, PricingVersions: versions, CostStatus: status, InputTokens: g.input.text(knownEmpty && len(g.sessions) == 0), CachedTokens: g.cached.text(knownEmpty && len(g.sessions) == 0), CacheWriteTokens: g.write.text(knownEmpty && len(g.sessions) == 0), OutputTokens: g.output.text(knownEmpty && len(g.sessions) == 0), ReasoningTokens: g.reasoning.text(knownEmpty && len(g.sessions) == 0), TotalTokens: g.tokens.text(knownEmpty && len(g.sessions) == 0), CostMicroUSD: amount, ReportedChargeMicroUSD: g.reported.text(false), Sessions: int64(len(g.sessions)), Invocations: g.invocations}, unpriced
}
func statisticsSlices(groups map[string]*statisticsAggregate, known bool) []vo.StatisticsSlice {
	out := make([]vo.StatisticsSlice, 0, len(groups))
	for key, g := range groups {
		totals, _ := g.finish(known)
		out = append(out, vo.StatisticsSlice{Key: key, Name: key, Totals: totals})
	}
	slices.SortFunc(out, func(a, b vo.StatisticsSlice) int { return strings.Compare(a.Key, b.Key) })
	return out
}
func aggregateFor(groups map[string]*statisticsAggregate, key string) *statisticsAggregate {
	g := groups[key]
	if g == nil {
		g = newStatisticsAggregate()
		groups[key] = g
	}
	return g
}
func decimalDifference(a *string, values []vo.StatisticsDay) *string {
	if a == nil {
		return nil
	}
	result, _ := new(big.Int).SetString(*a, 10)
	for _, v := range values {
		if v.Totals.CostMicroUSD != nil {
			n, _ := new(big.Int).SetString(*v.Totals.CostMicroUSD, 10)
			result.Sub(result, n)
		}
	}
	return new(result.String())
}

func sessionAggregate(groups map[string]*statisticsAggregate, key string) *statisticsAggregate {
	g := aggregateFor(groups, key)
	g.codexRange = true
	return g
}
