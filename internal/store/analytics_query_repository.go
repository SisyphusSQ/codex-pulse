package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

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
		var generation costRollupGenerationModel
		err := database.Where(
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

func validateAnalyticsRange(filter AnalyticsRange) (*time.Location, error) {
	if filter.ReportingTimezone == "" || filter.ReportingTimezone == "Local" ||
		filter.StartAtMS < 0 || filter.EndAtMS <= filter.StartAtMS {
		return nil, invalidRecord("analytics range is invalid")
	}
	location, err := time.LoadLocation(filter.ReportingTimezone)
	if err != nil {
		return nil, invalidRecord("analytics timezone is invalid")
	}
	if localDayBucketStart(filter.StartAtMS, location) != filter.StartAtMS ||
		localDayBucketStart(filter.EndAtMS, location) != filter.EndAtMS {
		return nil, invalidRecord("analytics range must use local day boundaries")
	}
	start := time.UnixMilli(filter.StartAtMS).In(location)
	end := time.UnixMilli(filter.EndAtMS).In(location)
	calendarStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	calendarEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	days := int(calendarEnd.Sub(calendarStart) / (24 * time.Hour))
	if days < 1 || days > analyticsHardMaxRangeDays {
		return nil, invalidRecord("analytics range exceeds calendar day limit")
	}
	return location, nil
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
	snapshot.Daily = make([]UsageDaily, 0)
	snapshot.PricingVersions = make([]string, 0)
	snapshot.UnpricedReasons = make([]CostReasonCount, 0)

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
	snapshot.PricingVersions = make([]string, 0)
	snapshot.UnpricedReasons = make([]CostReasonCount, 0)
	byDay := make(map[int64]*aggregateAccumulator)
	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return err
		}
		bucket := localDayBucketStart(fact.ObservedAtMS, location)
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
