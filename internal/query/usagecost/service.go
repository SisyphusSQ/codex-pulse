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
	validated, err := service.rangeSpec.Validate(ctx, basequery.Request{TimeRange: &request.Range})
	if err != nil {
		return UsageCostResponse{}, err
	}
	if validated.TimeRange == nil {
		return UsageCostResponse{}, fmt.Errorf("%w: usage range", basequery.ErrValidation)
	}
	rangeFilter := store.AnalyticsRange{
		ReportingTimezone: validated.TimeRange.TimeZone,
		StartAtMS:         validated.TimeRange.StartAtMS,
		EndAtMS:           validated.TimeRange.EndAtMS,
	}
	snapshot, err := service.reader.UsageCostRange(ctx, rangeFilter)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return UsageCostResponse{}, err
		}
		return UsageCostResponse{}, basequery.NewUnavailableFailure(err)
	}
	response, err := mapUsageCostResponse(request.Granularity, *validated.TimeRange, snapshot)
	if err != nil {
		return UsageCostResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func mapUsageCostResponse(
	granularity TrendGranularity,
	rangeValue basequery.UTCTimeRange,
	snapshot store.UsageCostRangeSnapshot,
) (UsageCostResponse, error) {
	mode := snapshot.Mode
	if mode != store.AnalyticsReadActiveRollup && mode != store.AnalyticsReadDetailFallback {
		return UsageCostResponse{}, errors.New("stored analytics mode is invalid")
	}
	if (mode == store.AnalyticsReadActiveRollup && snapshot.Generation == nil) ||
		(mode == store.AnalyticsReadDetailFallback && snapshot.Generation != nil) {
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
	rows := append([]store.UsageDaily(nil), snapshot.Daily...)
	if err := validateAndSortDaily(rows, rangeValue, snapshot.Generation); err != nil {
		return UsageCostResponse{}, err
	}
	overall, err := aggregateDaily(rows)
	if err != nil {
		return UsageCostResponse{}, err
	}
	if err := validatePricingEvidence(
		mode, overall, snapshot.PricingVersions, snapshot.UnpricedReasons,
	); err != nil {
		return UsageCostResponse{}, err
	}
	totals, err := mapUsageTotals(overall, mode)
	if err != nil {
		return UsageCostResponse{}, err
	}
	trend, err := groupTrend(rows, granularity, rangeValue.TimeZone, mode)
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
	partial := mode == store.AnalyticsReadDetailFallback || overall.UnpricedTurnCount > 0 ||
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
	}
	if snapshot.Generation != nil {
		response.PricingSource = cloneString(snapshot.Generation.PricingSource)
		response.Currency = cloneString(snapshot.Generation.Currency)
	} else {
		reason := DegradedRollupMissing
		response.DegradedReason = &reason
	}
	return response, nil
}

func validGranularity(value TrendGranularity) bool {
	return value == TrendDay || value == TrendWeek || value == TrendMonth
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
	versions []string,
	reasons []store.CostReasonCount,
) error {
	if mode != store.AnalyticsReadActiveRollup {
		if len(versions) != 0 || len(reasons) != 0 {
			return errors.New("fallback pricing evidence must be absent")
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
	case TrendDay:
		return value.Format("2006-01-02")
	case TrendWeek:
		year, week := value.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	default:
		return value.Format("2006-01")
	}
}
