package usagecost

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const usageMaxRangeDays = 366

type UsageReader interface {
	UsageCostRange(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error)
}

type SessionReader interface {
	ListSessionAnalytics(context.Context, store.SessionAnalyticsFilter) (store.SessionAnalyticsPage, error)
	SessionAnalytics(context.Context, store.SessionAnalyticsDetailFilter) (store.SessionAnalyticsSnapshot, error)
}

type ProjectReader interface {
	ListProjectAnalytics(context.Context, store.ProjectAnalyticsFilter) (store.ProjectAnalyticsPage, error)
	ProjectAnalytics(context.Context, store.ProjectAnalyticsDetailFilter) (store.ProjectAnalyticsSnapshot, error)
}

type Service struct {
	reader                 UsageReader
	sessionReader          SessionReader
	projectReader          ProjectReader
	rangeSpec              basequery.Specification
	sessionSpec            basequery.Specification
	projectSpec            basequery.Specification
	sessionTurnCursorKey   sessionTurnCursorKey
	projectDetailCursorKey projectDetailCursorKey
}

func NewService(reader UsageReader) (*Service, error) {
	if reader == nil {
		return nil, ErrInvalidService
	}
	specification, err := basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 1, MaxLimit: 1, MaxRangeDays: usageMaxRangeDays,
		SortFields:  []string{"bucketStart"},
		DefaultSort: []basequery.SortTerm{{Field: "bucketStart", Direction: basequery.SortAscending}},
		TieBreaker:  basequery.SortTerm{Field: "bucketStart", Direction: basequery.SortAscending},
	})
	if err != nil {
		return nil, ErrInvalidService
	}
	sessionSpecification, err := basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 50, MaxLimit: 100, MaxRangeDays: usageMaxRangeDays,
		SortFields: []string{"lastActivityAt", "totalTokens", "estimatedCost", "sessionId"},
		FilterFields: []basequery.FilterField{
			{Field: "projectId", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "modelKey", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "activity", Operators: []basequery.FilterOperator{basequery.FilterEqual}},
		},
		DefaultSort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
		TieBreaker:  basequery.SortTerm{Field: "sessionId", Direction: basequery.SortDescending},
	})
	if err != nil {
		return nil, ErrInvalidService
	}
	projectSpecification, err := basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 50, MaxLimit: 100, MaxRangeDays: usageMaxRangeDays,
		SortFields: []string{"lastActivityAt", "totalTokens", "estimatedCost", "displayName", "projectKey"},
		FilterFields: []basequery.FilterField{
			{Field: "projectId", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "confidence", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
		},
		DefaultSort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
		TieBreaker:  basequery.SortTerm{Field: "projectKey", Direction: basequery.SortDescending},
	})
	if err != nil {
		return nil, ErrInvalidService
	}
	sessionTurnKey, err := newSessionTurnCursorKey()
	if err != nil {
		return nil, ErrInvalidService
	}
	projectDetailKey, err := newProjectDetailCursorKey()
	if err != nil {
		return nil, ErrInvalidService
	}
	sessionReader, _ := reader.(SessionReader)
	projectReader, _ := reader.(ProjectReader)
	return &Service{
		reader: reader, sessionReader: sessionReader, projectReader: projectReader,
		rangeSpec: specification, sessionSpec: sessionSpecification, projectSpec: projectSpecification,
		sessionTurnCursorKey:   sessionTurnKey,
		projectDetailCursorKey: projectDetailKey,
	}, nil
}

func (service *Service) UsageCost(
	ctx context.Context,
	request UsageCostRequest,
) (UsageCostResponse, error) {
	if service == nil || service.reader == nil {
		return UsageCostResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return UsageCostResponse{}, err
	}
	if !validGranularity(request.Granularity) {
		return UsageCostResponse{}, fmt.Errorf("%w: usage granularity", basequery.ErrValidation)
	}
	validatedRange, exact, err := service.validateUsageRange(ctx, request)
	if err != nil {
		return UsageCostResponse{}, err
	}
	rangeFilter := store.AnalyticsRange{
		ReportingTimezone: validatedRange.TimeZone,
		StartAtMS:         validatedRange.StartAtMS,
		EndAtMS:           validatedRange.EndAtMS,
		Exact:             exact,
		Granularity:       store.AnalyticsGranularityDay,
	}
	if request.Granularity == TrendHour {
		rangeFilter.Granularity = store.AnalyticsGranularityHour
	}
	snapshot, err := service.reader.UsageCostRange(ctx, rangeFilter)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return UsageCostResponse{}, err
		}
		return UsageCostResponse{}, basequery.NewUnavailableFailure(err)
	}
	response, err := mapUsageCostResponse(request.Granularity, *validatedRange, snapshot)
	if err != nil {
		return UsageCostResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func (service *Service) validateUsageRange(
	ctx context.Context,
	request UsageCostRequest,
) (*basequery.UTCTimeRange, bool, error) {
	if request.ExactRange == nil {
		validated, err := service.rangeSpec.Validate(ctx, basequery.Request{TimeRange: &request.Range})
		if err != nil {
			return nil, false, err
		}
		if validated.TimeRange == nil {
			return nil, false, fmt.Errorf("%w: usage range", basequery.ErrValidation)
		}
		return validated.TimeRange, false, nil
	}
	if request.Range != (basequery.LocalDateRange{}) {
		return nil, false, fmt.Errorf("%w: usage range", basequery.ErrValidation)
	}
	rangeValue, err := validateExactUsageRange(*request.ExactRange)
	if err != nil {
		return nil, false, err
	}
	return rangeValue, true, nil
}

func validateExactUsageRange(input basequery.UTCTimeRange) (*basequery.UTCTimeRange, error) {
	if input.TimeZone == "" || input.TimeZone == "Local" || input.StartAtMS < 0 ||
		input.EndAtMS <= input.StartAtMS || input.EndAtMS > basequery.JavaScriptMaxSafeInteger {
		return nil, fmt.Errorf("%w: usage exact range", basequery.ErrValidation)
	}
	location, err := time.LoadLocation(input.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("%w: usage exact range timezone", basequery.ErrValidation)
	}
	start := time.UnixMilli(input.StartAtMS).In(location)
	end := time.UnixMilli(input.EndAtMS).In(location)
	calendarStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	calendarEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	days := int(calendarEnd.Sub(calendarStart) / (24 * time.Hour))
	if days < 0 || days > usageMaxRangeDays {
		return nil, fmt.Errorf("%w: usage exact range", basequery.ErrValidation)
	}
	result := input
	return &result, nil
}

func mapUsageCostResponse(
	granularity TrendGranularity,
	rangeValue basequery.UTCTimeRange,
	snapshot store.UsageCostRangeSnapshot,
) (UsageCostResponse, error) {
	mode := snapshot.Mode
	if mode != store.AnalyticsReadActiveRollup && mode != store.AnalyticsReadDetailFallback &&
		mode != store.AnalyticsReadLightIndex {
		return UsageCostResponse{}, errors.New("stored analytics mode is invalid")
	}
	if (mode == store.AnalyticsReadActiveRollup && snapshot.Generation == nil) ||
		(mode != store.AnalyticsReadActiveRollup && snapshot.Generation != nil) {
		return UsageCostResponse{}, errors.New("stored analytics generation shape is invalid")
	}
	if snapshot.Generation != nil {
		if snapshot.Generation.GenerationID == "" || snapshot.Generation.PricingSource == "" ||
			snapshot.Generation.Currency == "" || snapshot.Generation.RollupVersion <= 0 {
			return UsageCostResponse{}, errors.New("stored analytics pricing evidence is invalid")
		}
		if snapshot.Generation.ReportingTimezone != rangeValue.TimeZone {
			return UsageCostResponse{}, errors.New("stored analytics timezone is inconsistent")
		}
	}
	pricingSource, currency, err := usagePricingEvidence(snapshot)
	if err != nil {
		return UsageCostResponse{}, err
	}
	rows := append([]store.UsageDaily(nil), snapshot.Daily...)
	if err := validateAndSortDaily(rows, rangeValue, snapshot.Generation); err != nil {
		return UsageCostResponse{}, err
	}
	overall, err := aggregateDaily(rows, mode)
	if err != nil {
		return UsageCostResponse{}, err
	}
	if err := validatePricingEvidence(
		mode, overall, pricingSource, currency, snapshot.PricingVersions, snapshot.UnpricedReasons,
	); err != nil {
		return UsageCostResponse{}, err
	}
	totals, err := mapUsageTotals(overall, mode)
	if err != nil {
		return UsageCostResponse{}, err
	}
	trend, err := groupTrend(rows, granularity, rangeValue, mode)
	if err != nil {
		return UsageCostResponse{}, err
	}
	versions, err := normalizedPricingVersions(snapshot.PricingVersions)
	if err != nil {
		return UsageCostResponse{}, err
	}
	reasons, err := normalizedReasonCounts(snapshot.UnpricedReasons)
	if err != nil {
		return UsageCostResponse{}, err
	}
	models, err := mapUsageModels(
		snapshot.Models, mode, rangeValue, snapshot.Generation, granularity,
	)
	if err != nil {
		return UsageCostResponse{}, err
	}
	partial := mode != store.AnalyticsReadActiveRollup || overall.UnpricedTurnCount > 0 ||
		overall.InputTokens == nil || overall.CachedInputTokens == nil ||
		overall.OutputTokens == nil || overall.ReasoningTokens == nil
	status := basequery.ResponseComplete
	var issueCodes []basequery.ErrorCode
	if partial {
		status = basequery.ResponsePartial
		issueCodes = []basequery.ErrorCode{basequery.ErrorPartial}
	}
	meta, err := basequery.NewResponseMeta(status, nil, issueCodes)
	if err != nil {
		return UsageCostResponse{}, err
	}
	response := UsageCostResponse{
		Meta: meta, Range: rangeValue, ReportingTimeZone: rangeValue.TimeZone,
		PricingVersions: versions, Totals: totals, Trend: trend, UnpricedReasons: reasons,
		Models: models,
	}
	if pricingSource != "" {
		response.PricingSource = cloneString(pricingSource)
		response.Currency = cloneString(currency)
	}
	if snapshot.Generation == nil {
		reason := DegradedRollupMissing
		response.DegradedReason = &reason
	}
	return response, nil
}

func validGranularity(value TrendGranularity) bool {
	return value == TrendHour || value == TrendDay || value == TrendWeek || value == TrendMonth
}

func validateAndSortDaily(
	rows []store.UsageDaily,
	rangeValue basequery.UTCTimeRange,
	generation *store.CostRollupGeneration,
) error {
	for _, row := range rows {
		if row.ReportingTimezone != rangeValue.TimeZone || row.BucketStartMS < rangeValue.StartAtMS ||
			row.BucketStartMS >= rangeValue.EndAtMS {
			return errors.New("stored daily range is inconsistent")
		}
		if (generation != nil && row.GenerationID != generation.GenerationID) ||
			(generation == nil && row.GenerationID != "") {
			return errors.New("stored daily generation is inconsistent")
		}
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].BucketStartMS < rows[right].BucketStartMS })
	for index := 1; index < len(rows); index++ {
		if rows[index-1].BucketStartMS == rows[index].BucketStartMS {
			return errors.New("stored daily bucket is duplicated")
		}
	}
	return nil
}

func normalizedPricingVersions(input []string) ([]string, error) {
	result := append([]string(nil), input...)
	if result == nil {
		result = make([]string, 0)
	}
	sort.Strings(result)
	for index, value := range result {
		if value == "" || len(value) > 256 || (index > 0 && result[index-1] == value) {
			return nil, errors.New("stored pricing version is invalid")
		}
	}
	return result, nil
}

func normalizedReasonCounts(input []store.CostReasonCount) ([]ReasonCount, error) {
	values := append([]store.CostReasonCount(nil), input...)
	sort.Slice(values, func(left, right int) bool { return values[left].Reason < values[right].Reason })
	result := make([]ReasonCount, 0, len(values))
	for index, value := range values {
		if value.Count <= 0 || !validUnpricedReason(value.Reason) ||
			(index > 0 && values[index-1].Reason == value.Reason) {
			return nil, errors.New("stored unpriced reason is invalid")
		}
		count, err := basequery.KnownNumeric(value.Count, basequery.NumericCount)
		if err != nil {
			return nil, err
		}
		result = append(result, ReasonCount{Reason: value.Reason, Count: count})
	}
	return result, nil
}

func validatePricingEvidence(
	mode store.AnalyticsReadMode,
	totals store.RollupTotals,
	pricingSource string,
	currency string,
	versions []string,
	reasons []store.CostReasonCount,
) error {
	if mode == store.AnalyticsReadDetailFallback {
		if len(versions) != 0 || len(reasons) != 0 {
			return errors.New("fallback pricing evidence must be absent")
		}
		return nil
	}
	if mode == store.AnalyticsReadLightIndex {
		if len(reasons) != 0 || (pricingSource == "") != (currency == "") ||
			(totals.EstimatedUSDMicros != nil || len(versions) > 0) && pricingSource == "" {
			return errors.New("light pricing evidence is incomplete")
		}
		return nil
	}
	if totals.PricedTurnCount > 0 && len(versions) == 0 {
		return errors.New("stored pricing version evidence is incomplete")
	}
	reasonTotal := int64(0)
	for _, reason := range reasons {
		if reason.Count <= 0 || reason.Count > math.MaxInt64-reasonTotal {
			return errors.New("stored unpriced reason count is invalid")
		}
		reasonTotal += reason.Count
	}
	if reasonTotal != totals.UnpricedTurnCount {
		return errors.New("stored unpriced reason evidence is inconsistent")
	}
	return nil
}

func usagePricingEvidence(snapshot store.UsageCostRangeSnapshot) (string, string, error) {
	source, currency := snapshot.PricingSource, snapshot.Currency
	if snapshot.Generation != nil {
		if source == "" {
			source = snapshot.Generation.PricingSource
		}
		if currency == "" {
			currency = snapshot.Generation.Currency
		}
		if source != snapshot.Generation.PricingSource || currency != snapshot.Generation.Currency {
			return "", "", errors.New("stored analytics pricing evidence conflicts with generation")
		}
	} else if snapshot.Mode == store.AnalyticsReadDetailFallback && (source != "" || currency != "") {
		return "", "", errors.New("fallback pricing evidence must be absent")
	}
	if (source == "") != (currency == "") {
		return "", "", errors.New("stored analytics pricing evidence is incomplete")
	}
	return source, currency, nil
}

func mapUsageModels(
	rows []store.ModelUsageDaily,
	mode store.AnalyticsReadMode,
	rangeValue basequery.UTCTimeRange,
	generation *store.CostRollupGeneration,
	granularity TrendGranularity,
) ([]UsageModelItem, error) {
	type modelGroup struct {
		record store.ModelUsageDaily
		totals *totalsAccumulator
		daily  []store.UsageDaily
	}
	groups := make(map[string]*modelGroup)
	for _, row := range rows {
		if !validOpaqueIdentity(row.DimensionKey) || row.ReportingTimezone != rangeValue.TimeZone ||
			row.BucketStartMS < rangeValue.StartAtMS || row.BucketStartMS >= rangeValue.EndAtMS ||
			!validAttributionTuple(row.ModelKey, row.ModelDisplayName) ||
			!validProjectAttributionDTO(row.AttributionConfidence, row.AttributionSource, row.AttributionReason) ||
			(row.ModelKey != nil && *row.ModelKey != row.DimensionKey) ||
			(row.ModelKey == nil && row.DimensionKey != "unknown|"+row.AttributionConfidence+"|"+row.AttributionSource+"|"+row.AttributionReason) ||
			(generation != nil && row.GenerationID != generation.GenerationID) ||
			(generation == nil && row.GenerationID != "") {
			return nil, errors.New("stored usage model row is invalid")
		}
		group := groups[row.DimensionKey]
		if group == nil {
			group = &modelGroup{
				record: row, totals: newTotalsAccumulator(), daily: make([]store.UsageDaily, 0),
			}
			groups[row.DimensionKey] = group
		} else if !equalStringPointers(group.record.ModelKey, row.ModelKey) ||
			!equalStringPointers(group.record.ModelDisplayName, row.ModelDisplayName) ||
			group.record.AttributionConfidence != row.AttributionConfidence ||
			group.record.AttributionSource != row.AttributionSource ||
			group.record.AttributionReason != row.AttributionReason {
			return nil, errors.New("stored usage model attribution is inconsistent")
		}
		if err := group.totals.addMode(row.RollupTotals, mode); err != nil {
			return nil, err
		}
		group.daily = append(group.daily, store.UsageDaily{
			GenerationID: row.GenerationID, BucketStartMS: row.BucketStartMS,
			ReportingTimezone: row.ReportingTimezone, RollupTotals: row.RollupTotals,
		})
	}
	type mappedModel struct {
		item  UsageModelItem
		total *int64
	}
	mapped := make([]mappedModel, 0, len(groups))
	for _, group := range groups {
		totals, err := group.totals.totalsMode(mode)
		if err != nil {
			return nil, err
		}
		mappedTotals, err := mapUsageTotals(totals, mode)
		if err != nil {
			return nil, err
		}
		trend, err := groupTrend(group.daily, granularity, rangeValue, mode)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, mappedModel{
			item: UsageModelItem{
				DimensionKey: group.record.DimensionKey,
				Model: AttributionValue{
					ID: cloneStringPointer(group.record.ModelKey), DisplayName: cloneStringPointer(group.record.ModelDisplayName),
					Confidence: group.record.AttributionConfidence, Source: group.record.AttributionSource,
					Reason: group.record.AttributionReason,
				},
				Totals: mappedTotals, Trend: trend,
			},
			total: totals.TotalTokens,
		})
	}
	sort.Slice(mapped, func(left, right int) bool {
		if mapped[left].total != nil && mapped[right].total != nil && *mapped[left].total != *mapped[right].total {
			return *mapped[left].total > *mapped[right].total
		}
		if (mapped[left].total == nil) != (mapped[right].total == nil) {
			return mapped[left].total != nil
		}
		return mapped[left].item.DimensionKey < mapped[right].item.DimensionKey
	})
	result := make([]UsageModelItem, 0, len(mapped))
	for _, value := range mapped {
		result = append(result, value.item)
	}
	return result, nil
}

func validUnpricedReason(reason pricing.CostReason) bool {
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

func cloneString(value string) *string {
	cloned := value
	return &cloned
}

func trendKey(value time.Time, granularity TrendGranularity) string {
	switch granularity {
	case TrendHour:
		return value.Format("2006-01-02T15:00")
	case TrendDay:
		return value.Format("2006-01-02")
	case TrendWeek:
		year, week := value.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	default:
		return value.Format("2006-01")
	}
}
