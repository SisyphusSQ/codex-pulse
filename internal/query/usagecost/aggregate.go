package usagecost

import (
	"errors"
	"math"
	"sort"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type nullableAccumulator struct {
	value    int64
	complete bool
}

type totalsAccumulator struct {
	rows                                  int
	costRows                              int
	turnCount, pricedCount, unpricedCount int64
	input, cached, output, reasoning      nullableAccumulator
	cost                                  nullableAccumulator
	firstActivityAtMS, lastActivityAtMS   int64
}

func newTotalsAccumulator() *totalsAccumulator {
	return &totalsAccumulator{
		input: nullableAccumulator{complete: true}, cached: nullableAccumulator{complete: true},
		output: nullableAccumulator{complete: true}, reasoning: nullableAccumulator{complete: true},
		cost: nullableAccumulator{complete: true},
	}
}

func (accumulator *totalsAccumulator) add(value store.RollupTotals) error {
	return accumulator.addMode(value, store.AnalyticsReadActiveRollup)
}

func (accumulator *totalsAccumulator) addMode(value store.RollupTotals, mode store.AnalyticsReadMode) error {
	if value.TurnCount < 0 || value.PricedTurnCount < 0 || value.UnpricedTurnCount < 0 ||
		value.PricedTurnCount+value.UnpricedTurnCount != value.TurnCount {
		return errors.New("stored rollup counts are invalid")
	}
	if err := validateStoredTokenTotal(value, mode); err != nil {
		return err
	}
	var err error
	accumulator.turnCount, err = checkedAdd(accumulator.turnCount, value.TurnCount)
	if err != nil {
		return err
	}
	accumulator.pricedCount, err = checkedAdd(accumulator.pricedCount, value.PricedTurnCount)
	if err != nil {
		return err
	}
	accumulator.unpricedCount, err = checkedAdd(accumulator.unpricedCount, value.UnpricedTurnCount)
	if err != nil {
		return err
	}
	for _, component := range []struct {
		sum   *nullableAccumulator
		value *int64
	}{
		{sum: &accumulator.input, value: value.InputTokens},
		{sum: &accumulator.cached, value: value.CachedInputTokens},
		{sum: &accumulator.output, value: value.OutputTokens},
		{sum: &accumulator.reasoning, value: value.ReasoningTokens},
	} {
		if err := component.sum.add(component.value); err != nil {
			return err
		}
	}
	if mode == store.AnalyticsReadLightIndex {
		if value.EstimatedUSDMicros != nil {
			if *value.EstimatedUSDMicros < 0 {
				return errors.New("stored light rollup cost is invalid")
			}
			if err := accumulator.cost.add(value.EstimatedUSDMicros); err != nil {
				return err
			}
			accumulator.costRows++
		}
	} else {
		if (value.PricedTurnCount > 0 && value.EstimatedUSDMicros == nil) ||
			(value.PricedTurnCount == 0 && value.EstimatedUSDMicros != nil) {
			return errors.New("stored rollup cost shape is invalid")
		}
		if value.EstimatedUSDMicros != nil {
			if err := accumulator.cost.add(value.EstimatedUSDMicros); err != nil {
				return err
			}
		}
	}
	if value.TurnCount > 0 {
		if value.FirstActivityAtMS < 0 || value.LastActivityAtMS < value.FirstActivityAtMS {
			return errors.New("stored rollup activity range is invalid")
		}
		if accumulator.rows == 0 || value.FirstActivityAtMS < accumulator.firstActivityAtMS {
			accumulator.firstActivityAtMS = value.FirstActivityAtMS
		}
		if accumulator.rows == 0 || value.LastActivityAtMS > accumulator.lastActivityAtMS {
			accumulator.lastActivityAtMS = value.LastActivityAtMS
		}
	} else if value.FirstActivityAtMS != 0 || value.LastActivityAtMS != 0 {
		return errors.New("stored empty rollup activity range is invalid")
	}
	accumulator.rows++
	return nil
}

func validateStoredTokenTotal(value store.RollupTotals, mode store.AnalyticsReadMode) error {
	components := []*int64{value.InputTokens, value.OutputTokens, value.ReasoningTokens}
	if mode != store.AnalyticsReadLightIndex {
		components = []*int64{value.InputTokens, value.CachedInputTokens, value.OutputTokens, value.ReasoningTokens}
	} else if value.CachedInputTokens == nil {
		if value.TotalTokens != nil {
			return errors.New("stored token total shape is invalid")
		}
		return nil
	}
	for _, component := range components {
		if component == nil {
			if value.TotalTokens != nil {
				return errors.New("stored token total shape is invalid")
			}
			return nil
		}
	}
	total := int64(0)
	for _, component := range components {
		var err error
		total, err = checkedAdd(total, *component)
		if err != nil {
			return err
		}
	}
	if value.TotalTokens == nil || *value.TotalTokens != total {
		return errors.New("stored token total is inconsistent")
	}
	return nil
}

func (accumulator *totalsAccumulator) totals() (store.RollupTotals, error) {
	return accumulator.totalsMode(store.AnalyticsReadActiveRollup)
}

func (accumulator *totalsAccumulator) totalsMode(mode store.AnalyticsReadMode) (store.RollupTotals, error) {
	if accumulator.rows == 0 {
		zero := int64(0)
		var cost *int64
		if mode != store.AnalyticsReadLightIndex {
			cost = cloneInt64(&zero)
		}
		return store.RollupTotals{
			InputTokens: &zero, CachedInputTokens: cloneInt64(&zero),
			OutputTokens: cloneInt64(&zero), ReasoningTokens: cloneInt64(&zero),
			TotalTokens: cloneInt64(&zero), EstimatedUSDMicros: cost,
		}, nil
	}
	input := accumulator.input.pointer()
	cached := accumulator.cached.pointer()
	output := accumulator.output.pointer()
	reasoning := accumulator.reasoning.pointer()
	var total *int64
	if input != nil && cached != nil && output != nil && reasoning != nil {
		value := int64(0)
		var err error
		components := []int64{*input, *output, *reasoning}
		if mode != store.AnalyticsReadLightIndex {
			components = []int64{*input, *cached, *output, *reasoning}
		}
		for _, component := range components {
			value, err = checkedAdd(value, component)
			if err != nil {
				return store.RollupTotals{}, err
			}
		}
		total = &value
	}
	var cost *int64
	if accumulator.pricedCount > 0 || mode == store.AnalyticsReadLightIndex && accumulator.costRows > 0 {
		cost = accumulator.cost.pointer()
		if cost == nil {
			return store.RollupTotals{}, errors.New("aggregated priced cost is unavailable")
		}
	}
	return store.RollupTotals{
		TurnCount: accumulator.turnCount, InputTokens: input, CachedInputTokens: cached,
		OutputTokens: output, ReasoningTokens: reasoning, TotalTokens: total,
		EstimatedUSDMicros: cost, PricedTurnCount: accumulator.pricedCount,
		UnpricedTurnCount: accumulator.unpricedCount, FirstActivityAtMS: accumulator.firstActivityAtMS,
		LastActivityAtMS: accumulator.lastActivityAtMS,
	}, nil
}

func (sum *nullableAccumulator) add(value *int64) error {
	if !sum.complete {
		return nil
	}
	if value == nil {
		sum.complete = false
		return nil
	}
	var err error
	sum.value, err = checkedAdd(sum.value, *value)
	return err
}

func (sum nullableAccumulator) pointer() *int64 {
	if !sum.complete {
		return nil
	}
	value := sum.value
	return &value
}

func checkedAdd(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, errors.New("analytics integer overflow")
	}
	return left + right, nil
}

func aggregateDaily(rows []store.UsageDaily, mode store.AnalyticsReadMode) (store.RollupTotals, error) {
	accumulator := newTotalsAccumulator()
	for _, row := range rows {
		if err := accumulator.addMode(row.RollupTotals, mode); err != nil {
			return store.RollupTotals{}, err
		}
	}
	return accumulator.totalsMode(mode)
}

func mapUsageTotals(value store.RollupTotals, mode store.AnalyticsReadMode) (UsageTotals, error) {
	if mode == store.AnalyticsReadLightIndex {
		return mapLightIndexSessionTotals(&value)
	}
	turnCount, err := basequery.KnownNumeric(value.TurnCount, basequery.NumericCount)
	if err != nil {
		return UsageTotals{}, err
	}
	priced, err := basequery.KnownNumeric(value.PricedTurnCount, basequery.NumericCount)
	if err != nil {
		return UsageTotals{}, err
	}
	unpriced, err := basequery.KnownNumeric(value.UnpricedTurnCount, basequery.NumericCount)
	if err != nil {
		return UsageTotals{}, err
	}
	input, err := numericOrUnknown(value.InputTokens, basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return UsageTotals{}, err
	}
	cached, err := numericOrUnknown(value.CachedInputTokens, basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return UsageTotals{}, err
	}
	output, err := numericOrUnknown(value.OutputTokens, basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return UsageTotals{}, err
	}
	reasoning, err := numericOrUnknown(value.ReasoningTokens, basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return UsageTotals{}, err
	}
	total, err := numericOrUnknown(value.TotalTokens, basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return UsageTotals{}, err
	}
	costReason := basequery.UnknownNotComputed
	var cost basequery.NumericValue
	if mode == store.AnalyticsReadDetailFallback {
		cost, err = basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	} else {
		cost, err = numericOrUnknown(value.EstimatedUSDMicros, basequery.NumericMicroUSD, costReason)
	}
	if err != nil {
		return UsageTotals{}, err
	}
	first, last := basequery.NumericValue{}, basequery.NumericValue{}
	if value.TurnCount == 0 {
		first, _ = basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
		last, _ = basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	} else {
		first, err = basequery.KnownNumeric(value.FirstActivityAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return UsageTotals{}, err
		}
		last, err = basequery.KnownNumeric(value.LastActivityAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return UsageTotals{}, err
		}
	}
	return UsageTotals{
		TurnCount: turnCount, InputTokens: input, CachedInputTokens: cached,
		OutputTokens: output, ReasoningTokens: reasoning, TotalTokens: total,
		EstimatedUSDMicros: cost, PricedTurnCount: priced, UnpricedTurnCount: unpriced,
		FirstActivityAtMS: first, LastActivityAtMS: last,
	}, nil
}

func numericOrUnknown(
	value *int64,
	unit basequery.NumericUnit,
	reason basequery.UnknownReason,
) (basequery.NumericValue, error) {
	if value == nil {
		return basequery.UnknownNumeric(unit, reason)
	}
	return basequery.KnownNumeric(*value, unit)
}

type trendGroup struct {
	key                string
	startAtMS, endAtMS int64
	accumulator        *totalsAccumulator
}

func groupTrend(
	rows []store.UsageDaily,
	granularity TrendGranularity,
	timezone string,
	mode store.AnalyticsReadMode,
) ([]TrendPoint, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]*trendGroup)
	for _, row := range rows {
		local := time.UnixMilli(row.BucketStartMS).In(location)
		key := trendKey(local, granularity)
		group, found := groups[key]
		if !found {
			group = &trendGroup{key: key, startAtMS: row.BucketStartMS, accumulator: newTotalsAccumulator()}
			groups[key] = group
		}
		if row.BucketStartMS < group.startAtMS {
			group.startAtMS = row.BucketStartMS
		}
		nextDay := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location).UTC().UnixMilli()
		if nextDay > group.endAtMS {
			group.endAtMS = nextDay
		}
		if err := group.accumulator.addMode(row.RollupTotals, mode); err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]TrendPoint, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		storedTotals, err := group.accumulator.totalsMode(mode)
		if err != nil {
			return nil, err
		}
		totals, err := mapUsageTotals(storedTotals, mode)
		if err != nil {
			return nil, err
		}
		start, err := basequery.KnownNumeric(group.startAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return nil, err
		}
		end, err := basequery.KnownNumeric(group.endAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return nil, err
		}
		result = append(result, TrendPoint{Key: key, StartAtMS: start, EndAtMS: end, Totals: totals})
	}
	return result, nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
