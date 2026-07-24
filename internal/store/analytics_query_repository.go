package store

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func checkedAdd(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, invalidRecord("light analytics token overflow")
	}
	return left + right, nil
}

const analyticsHardMaxRangeDays = 366

type analyticsCostReasonCountModel struct {
	Reason string `gorm:"column:reason"`
	Count  int64  `gorm:"column:count"`
}

// UsageCostRange 返回指定 IANA 本地日范围的 active rollup；没有 active generation
// 时只读 final usage 生成 token-only fallback，不在 query path 重建成本账本。
func (repository *Repository) UsageCostRange(
	ctx context.Context,
	filter AnalyticsRange,
) (UsageCostRangeSnapshot, error) {
	if repository == nil || repository.database == nil {
		return UsageCostRangeSnapshot{}, ErrInvalidRepository
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return UsageCostRangeSnapshot{}, err
	}
	location, err := validateAnalyticsRange(filter)
	if err != nil {
		return UsageCostRangeSnapshot{}, err
	}

	var snapshot UsageCostRangeSnapshot
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		handled, err := loadLightUsageCostRange(ctx, database, filter, location, &snapshot)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		var generation costRollupGenerationModel
		err = database.Where(
			"reporting_timezone = ? AND state = ?",
			filter.ReportingTimezone, CostRollupGenerationActive,
		).Take(&generation).Error
		switch {
		case err == nil:
			return loadActiveUsageCostRange(ctx, database, filter, generation, &snapshot)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		default:
			return loadUsageCostDetailFallback(ctx, database, filter, location, &snapshot)
		}
	})
	return snapshot, err
}

type lightTimedRangeProjection struct {
	ObservedAtMS      int64   `gorm:"column:observed_at_ms"`
	ModelKey          *string `gorm:"column:model_key"`
	ModelSource       string  `gorm:"column:model_source"`
	InputTokens       int64   `gorm:"column:input_tokens"`
	CachedInputTokens int64   `gorm:"column:cached_input_tokens"`
	OutputTokens      int64   `gorm:"column:output_tokens"`
	ReasoningTokens   int64   `gorm:"column:reasoning_tokens"`
}

type lightPricingCatalog struct {
	version pricingVersionModel
	models  map[string]modelPriceModel
}

type lightCostGroupKey struct {
	bucketStartMS  int64
	dimensionKey   string
	pricingVersion string
}

type lightCostGroup struct {
	dimension                        safeDimension
	input, cached, output, reasoning int64
}

type lightRollupAccumulator struct {
	input, cached, output, reasoning, total int64
	estimatedUSDMicros                      int64
	hasPricedCost                           bool
}

func loadLightUsageCostRange(
	ctx context.Context,
	database *gorm.DB,
	filter AnalyticsRange,
	location *time.Location,
	snapshot *UsageCostRangeSnapshot,
) (bool, error) {
	var sessionCount int64
	if err := database.Model(&lightSessionModel{}).Count(&sessionCount).Error; err != nil {
		return false, err
	}
	if sessionCount == 0 {
		return false, nil
	}
	var rows []lightTimedRangeProjection
	if err := database.Table("light_token_timed AS timed").
		Select("timed.observed_at_ms, timed.model_key, timed.model_source, timed.input_tokens, timed.cached_input_tokens, timed.output_tokens, timed.reasoning_tokens").
		Joins("JOIN light_sessions AS session ON session.session_id = timed.session_id AND session.active_token_generation = timed.generation").
		Where("timed.observed_at_ms >= ? AND timed.observed_at_ms < ?", filter.StartAtMS, filter.EndAtMS).
		Order("timed.observed_at_ms, timed.session_id, timed.source_offset").Find(&rows).Error; err != nil {
		return false, err
	}
	catalogs, err := loadLightPricingCatalogs(database, filter.EndAtMS)
	if err != nil {
		return false, err
	}
	catalogByVersion := make(map[string]lightPricingCatalog, len(catalogs))
	for _, catalog := range catalogs {
		catalogByVersion[catalog.version.PricingVersion] = catalog
	}
	groups := make(map[lightCostGroupKey]*lightCostGroup)
	usedVersions := make(map[string]struct{})
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		bucket := analyticsBucketStart(row.ObservedAtMS, filter, location)
		dimension := lightModelDimension(row.ModelKey, row.ModelSource)
		catalog := effectiveLightPricingCatalog(catalogs, row.ObservedAtMS)
		version := ""
		if catalog != nil {
			version = catalog.version.PricingVersion
			usedVersions[version] = struct{}{}
		}
		key := lightCostGroupKey{bucketStartMS: bucket, dimensionKey: dimension.key, pricingVersion: version}
		group := groups[key]
		if group == nil {
			group = &lightCostGroup{dimension: dimension}
			groups[key] = group
		}
		if err := addLightTokens(group, row.InputTokens, row.CachedInputTokens, row.OutputTokens, row.ReasoningTokens); err != nil {
			return false, err
		}
	}
	byDay := make(map[int64]*lightRollupAccumulator)
	byModel := make(map[dimensionBucketKey]*lightRollupAccumulator)
	modelDimensions := make(map[dimensionBucketKey]safeDimension)
	for key, group := range groups {
		estimated, err := calculateLightGroupCost(group, catalogByVersion, key.pricingVersion)
		if err != nil {
			return false, err
		}
		if err := accumulatorForLight(byDay, key.bucketStartMS).add(group, estimated); err != nil {
			return false, err
		}
		modelKey := dimensionBucketKey{bucketStartMS: key.bucketStartMS, dimensionKey: key.dimensionKey}
		if err := recordCostDimension(modelDimensions, modelKey, group.dimension); err != nil {
			return false, err
		}
		if err := accumulatorForLight(byModel, modelKey).add(group, estimated); err != nil {
			return false, err
		}
	}
	keys := make([]int64, 0, len(byDay))
	for key := range byDay {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	snapshot.Mode = AnalyticsReadLightIndex
	snapshot.Daily = make([]UsageDaily, 0, len(keys))
	snapshot.Models = make([]ModelUsageDaily, 0, len(byModel))
	snapshot.PricingVersions = make([]string, 0, len(usedVersions))
	snapshot.UnpricedReasons = make([]CostReasonCount, 0)
	if len(rows) > 0 && len(catalogs) > 0 {
		snapshot.PricingSource = "openai-api"
		snapshot.Currency = "USD"
	}
	for version := range usedVersions {
		snapshot.PricingVersions = append(snapshot.PricingVersions, version)
	}
	sort.Strings(snapshot.PricingVersions)
	for _, key := range keys {
		snapshot.Daily = append(snapshot.Daily, UsageDaily{
			BucketStartMS: key, ReportingTimezone: filter.ReportingTimezone,
			RollupTotals: byDay[key].totals(),
		})
	}
	modelKeys := make([]dimensionBucketKey, 0, len(byModel))
	for key := range byModel {
		modelKeys = append(modelKeys, key)
	}
	sort.Slice(modelKeys, func(left, right int) bool {
		if modelKeys[left].bucketStartMS != modelKeys[right].bucketStartMS {
			return modelKeys[left].bucketStartMS < modelKeys[right].bucketStartMS
		}
		return modelKeys[left].dimensionKey < modelKeys[right].dimensionKey
	})
	for _, key := range modelKeys {
		dimension := modelDimensions[key]
		snapshot.Models = append(snapshot.Models, ModelUsageDaily{
			BucketStartMS: key.bucketStartMS, ReportingTimezone: filter.ReportingTimezone,
			DimensionKey: key.dimensionKey, ModelKey: cloneLightString(dimension.identity),
			ModelDisplayName:      cloneLightString(dimension.display),
			AttributionConfidence: dimension.confidence, AttributionSource: dimension.source,
			AttributionReason: dimension.reason, RollupTotals: byModel[key].totals(),
		})
	}
	return true, nil
}

func loadLightPricingCatalogs(database *gorm.DB, endAtMS int64) ([]lightPricingCatalog, error) {
	var versions []pricingVersionModel
	if err := database.Where("source = ? AND currency = ? AND effective_from_ms < ?", "openai-api", "USD", endAtMS).
		Order("effective_from_ms, pricing_version").Find(&versions).Error; err != nil {
		return nil, err
	}
	output := make([]lightPricingCatalog, 0, len(versions))
	for _, version := range versions {
		var models []modelPriceModel
		if err := database.Where("pricing_version = ? AND match_kind = ?", version.PricingVersion, ModelMatchExact).
			Order("model_pattern").Find(&models).Error; err != nil {
			return nil, err
		}
		catalog := lightPricingCatalog{version: version, models: make(map[string]modelPriceModel, len(models))}
		for _, model := range models {
			catalog.models[model.ModelPattern] = model
		}
		output = append(output, catalog)
	}
	return output, nil
}

func effectiveLightPricingCatalog(catalogs []lightPricingCatalog, observedAtMS int64) *lightPricingCatalog {
	for index := len(catalogs) - 1; index >= 0; index-- {
		if catalogs[index].version.EffectiveFromMS <= observedAtMS {
			return &catalogs[index]
		}
	}
	return nil
}

func lightModelDimension(modelKey *string, source string) safeDimension {
	decision := attribution.NormalizeModel("")
	if modelKey != nil {
		decision = attribution.NormalizeModel(*modelKey)
	}
	if decision.Key != "" && (source == string(attribution.SourceModelCanonical) || source == string(attribution.SourceModelAlias)) {
		identity, display := decision.Key, decision.DisplayName
		return safeDimension{
			key: identity, identity: &identity, display: &display,
			confidence: string(attribution.ConfidenceHigh), source: source, reason: string(attribution.ReasonObserved),
		}
	}
	unknownSource := string(attribution.SourceMissing)
	unknownReason := string(attribution.ReasonMissing)
	if source == string(attribution.SourceInvalidModel) {
		unknownSource = source
		unknownReason = string(attribution.ReasonInvalid)
	}
	return safeDimension{
		key:        "unknown|" + string(attribution.ConfidenceUnknown) + "|" + unknownSource + "|" + unknownReason,
		confidence: string(attribution.ConfidenceUnknown), source: unknownSource, reason: unknownReason,
	}
}

func addLightTokens(group *lightCostGroup, input, cached, output, reasoning int64) error {
	var err error
	if group.input, err = checkedAdd(group.input, input); err != nil {
		return err
	}
	if group.cached, err = checkedAdd(group.cached, cached); err != nil {
		return err
	}
	if group.output, err = checkedAdd(group.output, output); err != nil {
		return err
	}
	group.reasoning, err = checkedAdd(group.reasoning, reasoning)
	return err
}

func calculateLightGroupCost(
	group *lightCostGroup,
	catalogs map[string]lightPricingCatalog,
	pricingVersion string,
) (*int64, error) {
	if group == nil || pricingVersion == "" || group.dimension.identity == nil {
		return nil, nil
	}
	catalog, found := catalogs[pricingVersion]
	if !found {
		return nil, invalidRecord("light pricing version is missing")
	}
	model, found := catalog.models[*group.dimension.identity]
	if !found {
		return nil, nil
	}
	calculation, err := pricing.Calculate(pricing.Usage{
		InputTokens: &group.input, CachedInputTokens: &group.cached,
		OutputTokens: &group.output, ReasoningTokens: &group.reasoning,
	}, pricing.Rates{
		InputMicrosPerMillion:       model.InputMicrosPerMillion,
		CachedInputMicrosPerMillion: model.CachedInputMicrosPerMillion,
		OutputMicrosPerMillion:      model.OutputMicrosPerMillion,
	})
	if err != nil {
		return nil, err
	}
	return calculation.EstimatedUSDMicros, nil
}

func accumulatorForLight[K comparable](groups map[K]*lightRollupAccumulator, key K) *lightRollupAccumulator {
	if groups[key] == nil {
		groups[key] = &lightRollupAccumulator{}
	}
	return groups[key]
}

func (aggregate *lightRollupAccumulator) add(group *lightCostGroup, estimated *int64) error {
	var err error
	if aggregate.input, err = checkedAdd(aggregate.input, group.input); err != nil {
		return err
	}
	if aggregate.cached, err = checkedAdd(aggregate.cached, group.cached); err != nil {
		return err
	}
	if aggregate.output, err = checkedAdd(aggregate.output, group.output); err != nil {
		return err
	}
	if aggregate.reasoning, err = checkedAdd(aggregate.reasoning, group.reasoning); err != nil {
		return err
	}
	groupTotal, err := checkedAdd(group.input, group.output)
	if err == nil {
		groupTotal, err = checkedAdd(groupTotal, group.reasoning)
	}
	if err != nil {
		return err
	}
	if aggregate.total, err = checkedAdd(aggregate.total, groupTotal); err != nil {
		return err
	}
	if estimated != nil {
		aggregate.estimatedUSDMicros, err = checkedAdd(aggregate.estimatedUSDMicros, *estimated)
		aggregate.hasPricedCost = err == nil
	}
	return err
}

func (aggregate *lightRollupAccumulator) totals() RollupTotals {
	input, cached, output, reasoning := aggregate.input, aggregate.cached, aggregate.output, aggregate.reasoning
	total := aggregate.total
	var estimated *int64
	if aggregate.hasPricedCost {
		value := aggregate.estimatedUSDMicros
		estimated = &value
	}
	return RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total, EstimatedUSDMicros: estimated,
	}
}

func validateAnalyticsRange(filter AnalyticsRange) (*time.Location, error) {
	if filter.ReportingTimezone == "" || filter.ReportingTimezone == "Local" ||
		filter.StartAtMS < 0 || filter.EndAtMS <= filter.StartAtMS {
		return nil, invalidRecord("analytics range is invalid")
	}
	location, err := time.LoadLocation(filter.ReportingTimezone)
	if err != nil {
		return nil, invalidRecord("analytics timezone is invalid")
	}
	if filter.Granularity == "" {
		filter.Granularity = AnalyticsGranularityDay
	}
	if filter.Granularity != AnalyticsGranularityDay && filter.Granularity != AnalyticsGranularityHour {
		return nil, invalidRecord("analytics granularity is invalid")
	}
	if !filter.Exact && (localDayBucketStart(filter.StartAtMS, location) != filter.StartAtMS ||
		localDayBucketStart(filter.EndAtMS, location) != filter.EndAtMS) {
		return nil, invalidRecord("analytics range must use local day boundaries")
	}
	start := time.UnixMilli(filter.StartAtMS).In(location)
	end := time.UnixMilli(filter.EndAtMS).In(location)
	calendarStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	calendarEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	days := int(calendarEnd.Sub(calendarStart) / (24 * time.Hour))
	if days > analyticsHardMaxRangeDays || (!filter.Exact && days < 1) {
		return nil, invalidRecord("analytics range exceeds calendar day limit")
	}
	return location, nil
}

func analyticsBucketStart(observedAtMS int64, filter AnalyticsRange, location *time.Location) int64 {
	bucket := localDayBucketStart(observedAtMS, location)
	if filter.Granularity == AnalyticsGranularityHour {
		local := time.UnixMilli(observedAtMS).In(location)
		bucket = time.Date(
			local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, location,
		).UTC().UnixMilli()
	}
	if filter.Exact && bucket < filter.StartAtMS {
		return filter.StartAtMS
	}
	return bucket
}

func loadActiveUsageCostRange(
	ctx context.Context,
	database *gorm.DB,
	filter AnalyticsRange,
	generation costRollupGenerationModel,
	snapshot *UsageCostRangeSnapshot,
) error {
	record := generationFromModel(generation)
	snapshot.Mode = AnalyticsReadActiveRollup
	snapshot.Generation = &record
	snapshot.PricingSource = record.PricingSource
	snapshot.Currency = record.Currency
	snapshot.Daily = make([]UsageDaily, 0)
	snapshot.Models = make([]ModelUsageDaily, 0)
	snapshot.PricingVersions = make([]string, 0)
	snapshot.UnpricedReasons = make([]CostReasonCount, 0)

	if filter.Exact || filter.Granularity == AnalyticsGranularityHour {
		if err := loadActiveGranularUsageRows(ctx, database, filter, generation, snapshot); err != nil {
			return err
		}
	} else {
		var days []usageDailyModel
		if err := database.Where(
			"generation_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?",
			generation.GenerationID, filter.StartAtMS, filter.EndAtMS,
		).Order("bucket_start_ms").Find(&days).Error; err != nil {
			return err
		}
		for _, model := range days {
			if model.ReportingTimezone != filter.ReportingTimezone {
				return invalidRecord("stored daily timezone is inconsistent")
			}
			snapshot.Daily = append(snapshot.Daily, UsageDaily{
				GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
				ReportingTimezone: model.ReportingTimezone, RollupTotals: totalsFromModel(model.Totals),
			})
		}
	}
	if !filter.Exact {
		var models []modelUsageDailyModel
		if err := database.Where(
			"generation_id = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?",
			generation.GenerationID, filter.StartAtMS, filter.EndAtMS,
		).Order("bucket_start_ms, dimension_key").Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			snapshot.Models = append(snapshot.Models, ModelUsageDaily{
				GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
				ReportingTimezone: model.ReportingTimezone, DimensionKey: model.DimensionKey,
				ModelKey: model.ModelKey, ModelDisplayName: model.ModelDisplayName,
				AttributionConfidence: model.AttributionConfidence, AttributionSource: model.AttributionSource,
				AttributionReason: model.AttributionReason, RollupTotals: totalsFromModel(model.Totals),
			})
		}
	}

	if err := database.Table("turn_costs AS cost").
		Joins("JOIN turn_usage AS usage ON usage.turn_id = cost.turn_id").
		Where(
			"cost.generation_id = ? AND usage.is_final = ? AND usage.observed_at_ms >= ? AND usage.observed_at_ms < ? AND cost.pricing_version IS NOT NULL",
			generation.GenerationID, true, filter.StartAtMS, filter.EndAtMS,
		).
		Distinct("cost.pricing_version").Order("cost.pricing_version").
		Pluck("cost.pricing_version", &snapshot.PricingVersions).Error; err != nil {
		return err
	}

	var reasons []analyticsCostReasonCountModel
	if err := database.Table("turn_costs AS cost").
		Select("cost.pricing_reason AS reason, COUNT(*) AS count").
		Joins("JOIN turn_usage AS usage ON usage.turn_id = cost.turn_id").
		Where(
			"cost.generation_id = ? AND cost.pricing_status = ? AND usage.is_final = ? AND usage.observed_at_ms >= ? AND usage.observed_at_ms < ?",
			generation.GenerationID, pricing.CostStatusUnpriced, true,
			filter.StartAtMS, filter.EndAtMS,
		).Group("cost.pricing_reason").Order("cost.pricing_reason").Scan(&reasons).Error; err != nil {
		return err
	}
	for _, reason := range reasons {
		if reason.Count <= 0 || !validStoredCostReason(pricing.CostReason(reason.Reason)) {
			return invalidRecord("stored unpriced reason summary is invalid")
		}
		snapshot.UnpricedReasons = append(snapshot.UnpricedReasons, CostReasonCount{
			Reason: pricing.CostReason(reason.Reason), Count: reason.Count,
		})
	}
	return nil
}

func loadActiveGranularUsageRows(
	ctx context.Context,
	database *gorm.DB,
	filter AnalyticsRange,
	generation costRollupGenerationModel,
	snapshot *UsageCostRangeSnapshot,
) error {
	facts, err := loadFinalCostFactsInRange(database, filter.StartAtMS, filter.EndAtMS)
	if err != nil {
		return err
	}
	var models []turnCostModel
	if err := database.Table("turn_costs AS cost").
		Select("cost.*").
		Joins("JOIN turn_usage AS usage ON usage.turn_id = cost.turn_id").
		Where(
			"cost.generation_id = ? AND usage.is_final = ? AND usage.observed_at_ms >= ? AND usage.observed_at_ms < ?",
			generation.GenerationID, true, filter.StartAtMS, filter.EndAtMS,
		).
		Order("cost.turn_id").Scan(&models).Error; err != nil {
		return err
	}
	costs := make(map[string]TurnCost, len(models))
	for _, model := range models {
		if _, duplicated := costs[model.TurnID]; duplicated {
			return invalidRecord("stored hourly turn cost is duplicated")
		}
		status := pricing.CostStatus(model.PricingStatus)
		reason := pricing.CostReason(model.PricingReason)
		if status != pricing.CostStatusPriced && status != pricing.CostStatusUnpriced {
			return invalidRecord("stored hourly turn cost status is invalid")
		}
		if status == pricing.CostStatusPriced && (reason != pricing.CostReasonPriced || model.EstimatedUSDMicros == nil) {
			return invalidRecord("stored hourly priced turn is invalid")
		}
		if status == pricing.CostStatusUnpriced && !validStoredCostReason(reason) {
			return invalidRecord("stored hourly unpriced turn is invalid")
		}
		costs[model.TurnID] = TurnCost{
			GenerationID: model.GenerationID, TurnID: model.TurnID,
			PricingVersion: model.PricingVersion, EstimatedUSDMicros: model.EstimatedUSDMicros,
			Status: status, Reason: reason, CalculatedAtMS: model.CalculatedAtMS,
		}
	}
	location, err := time.LoadLocation(filter.ReportingTimezone)
	if err != nil {
		return invalidRecord("analytics timezone is invalid")
	}
	groups := make(map[int64]*aggregateAccumulator)
	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return err
		}
		cost, ok := costs[fact.TurnID]
		if !ok {
			return invalidRecord("stored hourly turn cost is missing")
		}
		bucket := analyticsBucketStart(fact.ObservedAtMS, filter, location)
		if err := accumulatorFor(groups, bucket).add(fact, cost); err != nil {
			return err
		}
	}
	keys := make([]int64, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	for _, key := range keys {
		totals, err := groups[key].totals(generation.UpdatedAtMS)
		if err != nil {
			return err
		}
		snapshot.Daily = append(snapshot.Daily, UsageDaily{
			GenerationID: generation.GenerationID, BucketStartMS: key,
			ReportingTimezone: filter.ReportingTimezone, RollupTotals: totals,
		})
	}
	return nil
}

func loadUsageCostDetailFallback(
	ctx context.Context,
	database *gorm.DB,
	filter AnalyticsRange,
	location *time.Location,
	snapshot *UsageCostRangeSnapshot,
) error {
	facts, err := loadFinalCostFactsInRange(database, filter.StartAtMS, filter.EndAtMS)
	if err != nil {
		return err
	}
	snapshot.Mode = AnalyticsReadDetailFallback
	snapshot.Daily = make([]UsageDaily, 0)
	snapshot.Models = make([]ModelUsageDaily, 0)
	snapshot.PricingVersions = make([]string, 0)
	snapshot.UnpricedReasons = make([]CostReasonCount, 0)
	byDay := make(map[int64]*aggregateAccumulator)
	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return err
		}
		bucket := analyticsBucketStart(fact.ObservedAtMS, filter, location)
		fallbackCost := TurnCost{
			Status: pricing.CostStatusUnpriced, Reason: pricing.CostReasonCatalogNotEffective,
		}
		if err := accumulatorFor(byDay, bucket).add(fact, fallbackCost); err != nil {
			return err
		}
	}
	keys := make([]int64, 0, len(byDay))
	for key := range byDay {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	for _, key := range keys {
		totals, err := byDay[key].totals(byDay[key].lastActivityAtMS)
		if err != nil {
			return err
		}
		snapshot.Daily = append(snapshot.Daily, UsageDaily{
			BucketStartMS: key, ReportingTimezone: filter.ReportingTimezone, RollupTotals: totals,
		})
	}
	return nil
}

func loadFinalCostFactsInRange(database *gorm.DB, startAtMS, endAtMS int64) ([]costFactModel, error) {
	var facts []costFactModel
	err := database.Table("turn_usage AS usage").
		Select(`usage.turn_id, turns.session_id, usage.observed_at_ms,
			usage.input_tokens, usage.cached_input_tokens, usage.output_tokens, usage.reasoning_tokens`).
		Joins("JOIN turns ON turns.turn_id = usage.turn_id AND turns.source_generation = usage.source_generation").
		Where(
			"usage.is_final = ? AND usage.observed_at_ms >= ? AND usage.observed_at_ms < ?",
			true, startAtMS, endAtMS,
		).Order("usage.turn_id").Scan(&facts).Error
	return facts, err
}

func validStoredCostReason(reason pricing.CostReason) bool {
	switch reason {
	case pricing.CostReasonMissingAttribution, pricing.CostReasonMissingModel,
		pricing.CostReasonConflictModel, pricing.CostReasonInvalidModel,
		pricing.CostReasonCatalogNotEffective, pricing.CostReasonModelNotListed,
		pricing.CostReasonMissingToken, pricing.CostReasonMissingPriceComponent:
		return true
	default:
		return false
	}
}
