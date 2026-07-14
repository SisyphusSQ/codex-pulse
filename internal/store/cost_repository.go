package store

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	maxCostInteger     = int64(1<<63 - 1)
	costDimensionMixed = "mixed"
)

type costFactModel struct {
	TurnID            string
	SessionID         string
	ObservedAtMS      int64
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64

	AttributionTurnID *string
	ProjectID         *string
	ProjectDisplay    *string
	ProjectConfidence *string
	ProjectSource     *string
	ProjectReason     *string
	ModelKey          *string
	ModelDisplay      *string
	ModelConfidence   *string
	ModelSource       *string
	ModelReason       *string
}

type nullableSum struct {
	value    int64
	complete bool
}

func newNullableSum() nullableSum { return nullableSum{complete: true} }

func (sum *nullableSum) add(value *int64) error {
	if !sum.complete {
		return nil
	}
	if value == nil {
		sum.complete = false
		return nil
	}
	var err error
	sum.value, err = addCostInteger(sum.value, *value)
	return err
}

func (sum nullableSum) pointer() *int64 {
	if !sum.complete {
		return nil
	}
	value := sum.value
	return &value
}

type aggregateAccumulator struct {
	turnCount, pricedTurnCount, unpricedTurnCount int64
	input, cached, output, reasoning              nullableSum
	estimatedUSDMicros                            int64
	firstActivityAtMS, lastActivityAtMS           int64
}

func newAggregateAccumulator() *aggregateAccumulator {
	return &aggregateAccumulator{
		input: newNullableSum(), cached: newNullableSum(),
		output: newNullableSum(), reasoning: newNullableSum(),
	}
}

func (aggregate *aggregateAccumulator) add(fact costFactModel, cost TurnCost) error {
	if aggregate.turnCount == 0 {
		aggregate.firstActivityAtMS = fact.ObservedAtMS
		aggregate.lastActivityAtMS = fact.ObservedAtMS
	} else {
		if fact.ObservedAtMS < aggregate.firstActivityAtMS {
			aggregate.firstActivityAtMS = fact.ObservedAtMS
		}
		if fact.ObservedAtMS > aggregate.lastActivityAtMS {
			aggregate.lastActivityAtMS = fact.ObservedAtMS
		}
	}
	aggregate.turnCount++
	if err := aggregate.input.add(fact.InputTokens); err != nil {
		return err
	}
	if err := aggregate.cached.add(fact.CachedInputTokens); err != nil {
		return err
	}
	if err := aggregate.output.add(fact.OutputTokens); err != nil {
		return err
	}
	if err := aggregate.reasoning.add(fact.ReasoningTokens); err != nil {
		return err
	}
	if cost.Status == pricing.CostStatusPriced {
		if cost.EstimatedUSDMicros == nil {
			return invalidRecord("priced turn cost is missing its amount")
		}
		aggregate.pricedTurnCount++
		var err error
		aggregate.estimatedUSDMicros, err = addCostInteger(
			aggregate.estimatedUSDMicros, *cost.EstimatedUSDMicros,
		)
		return err
	}
	aggregate.unpricedTurnCount++
	return nil
}

func (aggregate *aggregateAccumulator) totals(updatedAtMS int64) (RollupTotals, error) {
	input := aggregate.input.pointer()
	cached := aggregate.cached.pointer()
	output := aggregate.output.pointer()
	reasoning := aggregate.reasoning.pointer()
	var totalTokens *int64
	if input != nil && cached != nil && output != nil && reasoning != nil {
		total := int64(0)
		var err error
		for _, component := range []int64{*input, *cached, *output, *reasoning} {
			total, err = addCostInteger(total, component)
			if err != nil {
				return RollupTotals{}, err
			}
		}
		totalTokens = &total
	}
	var estimated *int64
	if aggregate.pricedTurnCount > 0 {
		value := aggregate.estimatedUSDMicros
		estimated = &value
	}
	return RollupTotals{
		TurnCount: aggregate.turnCount, InputTokens: input, CachedInputTokens: cached,
		OutputTokens: output, ReasoningTokens: reasoning, TotalTokens: totalTokens,
		EstimatedUSDMicros: estimated, PricedTurnCount: aggregate.pricedTurnCount,
		UnpricedTurnCount: aggregate.unpricedTurnCount,
		FirstActivityAtMS: aggregate.firstActivityAtMS, LastActivityAtMS: aggregate.lastActivityAtMS,
		UpdatedAtMS: updatedAtMS,
	}, nil
}

type safeDimension struct {
	key, confidence, source, reason string
	identity, display               *string
}

type dimensionBucketKey struct {
	bucketStartMS int64
	dimensionKey  string
}

// RebuildCostLedger 在一个低优先级 writer transaction 内重建并原子切换成本 generation。
func (repository *Repository) RebuildCostLedger(
	ctx context.Context,
	request RebuildCostLedgerRequest,
) (RebuildCostLedgerReport, error) {
	if repository == nil || repository.database == nil {
		return RebuildCostLedgerReport{}, ErrInvalidRepository
	}
	location, err := validateCostRebuildRequest(request)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	var report RebuildCostLedgerReport
	err = repository.database.WriteMaintenance(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		var rebuildErr error
		report, rebuildErr = rebuildCostLedgerInTransaction(
			ctx, transaction.WithContext(ctx), request, location,
		)
		return rebuildErr
	})
	return report, err
}

func rebuildCostLedgerInTransaction(
	ctx context.Context,
	database *gorm.DB,
	request RebuildCostLedgerRequest,
	location *time.Location,
) (RebuildCostLedgerReport, error) {
	var existing costRollupGenerationModel
	existingQuery := database.Where("generation_id = ?", request.GenerationID).Limit(1).Find(&existing)
	if existingQuery.Error != nil {
		return RebuildCostLedgerReport{}, existingQuery.Error
	}
	if existingQuery.RowsAffected > 0 {
		if !generationMatchesRequest(existing, request) || existing.State == string(CostRollupGenerationBuilding) {
			return RebuildCostLedgerReport{}, invalidRecord("cost generation conflicts with immutable history")
		}
		report, err := costGenerationReport(ctx, database, request.GenerationID)
		report.Replayed = err == nil
		return report, err
	}

	generation := costRollupGenerationModel{
		GenerationID: request.GenerationID, ReportingTimezone: request.ReportingTimezone,
		PricingSource: request.PricingSource, Currency: request.Currency,
		RollupVersion: request.RollupVersion, State: string(CostRollupGenerationBuilding),
		CreatedAtMS: request.CalculatedAtMS, UpdatedAtMS: request.CalculatedAtMS,
	}
	createdGeneration := database.Create(&generation)
	if createdGeneration.Error != nil {
		return RebuildCostLedgerReport{}, createdGeneration.Error
	}
	if createdGeneration.RowsAffected != 1 {
		return RebuildCostLedgerReport{}, invalidRecord("cost generation insert affected an unexpected row count")
	}

	facts, err := loadFinalCostFacts(database)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	overall := newAggregateAccumulator()
	sessions := make(map[string]*aggregateAccumulator)
	days := make(map[int64]*aggregateAccumulator)
	projects := make(map[dimensionBucketKey]*aggregateAccumulator)
	models := make(map[dimensionBucketKey]*aggregateAccumulator)
	projectDimensions := make(map[dimensionBucketKey]safeDimension)
	modelDimensions := make(map[dimensionBucketKey]safeDimension)
	costModels := make([]turnCostModel, 0, len(facts))

	for _, fact := range facts {
		if err := ctx.Err(); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		if fact.ObservedAtMS > request.CalculatedAtMS {
			return RebuildCostLedgerReport{}, invalidRecord("calculation time predates final usage")
		}
		cost, err := turnCostForFact(ctx, database, request, fact)
		if err != nil {
			return RebuildCostLedgerReport{}, err
		}
		costModels = append(costModels, turnCostModelFromRecord(cost))
		if err := overall.add(fact, cost); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		if err := accumulatorFor(sessions, fact.SessionID).add(fact, cost); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		bucketStartMS := localDayBucketStart(fact.ObservedAtMS, location)
		if err := accumulatorFor(days, bucketStartMS).add(fact, cost); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		project := costDimension(
			fact.ProjectID, fact.ProjectDisplay,
			fact.ProjectConfidence, fact.ProjectSource, fact.ProjectReason,
		)
		projectKey := dimensionBucketKey{bucketStartMS: bucketStartMS, dimensionKey: project.key}
		if err := recordCostDimension(projectDimensions, projectKey, project); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		if err := accumulatorFor(projects, projectKey).add(fact, cost); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		model := costDimension(
			fact.ModelKey, fact.ModelDisplay,
			fact.ModelConfidence, fact.ModelSource, fact.ModelReason,
		)
		modelKey := dimensionBucketKey{bucketStartMS: bucketStartMS, dimensionKey: model.key}
		if err := recordCostDimension(modelDimensions, modelKey, model); err != nil {
			return RebuildCostLedgerReport{}, err
		}
		if err := accumulatorFor(models, modelKey).add(fact, cost); err != nil {
			return RebuildCostLedgerReport{}, err
		}
	}

	expected, err := overall.totals(request.CalculatedAtMS)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	for _, groups := range []map[dimensionBucketKey]*aggregateAccumulator{projects, models} {
		if err := reconcileAggregateGroups(expected, groups, request.CalculatedAtMS); err != nil {
			return RebuildCostLedgerReport{}, err
		}
	}
	if err := reconcileAggregateGroups(expected, sessions, request.CalculatedAtMS); err != nil {
		return RebuildCostLedgerReport{}, err
	}
	if err := reconcileAggregateGroups(expected, days, request.CalculatedAtMS); err != nil {
		return RebuildCostLedgerReport{}, err
	}

	sessionModels, err := sessionRollupModels(request, sessions)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	dailyModels, err := dailyRollupModels(request, days)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	projectModels, err := projectRollupModels(request, projects, projectDimensions)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	modelModels, err := modelRollupModels(request, models, modelDimensions)
	if err != nil {
		return RebuildCostLedgerReport{}, err
	}
	for _, create := range []func() error{
		func() error { return createCostRows(database, costModels, "turn costs") },
		func() error { return createCostRows(database, sessionModels, "session rollups") },
		func() error { return createCostRows(database, dailyModels, "daily rollups") },
		func() error { return createCostRows(database, projectModels, "project rollups") },
		func() error { return createCostRows(database, modelModels, "model rollups") },
	} {
		if err := create(); err != nil {
			return RebuildCostLedgerReport{}, err
		}
	}
	if err := reconcilePersistedCostGeneration(
		ctx, database, request.GenerationID, expected,
		costModels, sessionModels, dailyModels, projectModels, modelModels,
	); err != nil {
		return RebuildCostLedgerReport{}, err
	}

	var active costRollupGenerationModel
	activeQuery := database.Where(
		"reporting_timezone = ? AND state = ?", request.ReportingTimezone, CostRollupGenerationActive,
	).Limit(1).Find(&active)
	if activeQuery.Error != nil {
		return RebuildCostLedgerReport{}, activeQuery.Error
	}
	if activeQuery.RowsAffected > 0 {
		if active.UpdatedAtMS > request.CalculatedAtMS {
			return RebuildCostLedgerReport{}, invalidRecord("calculation time predates active generation")
		}
		superseded := database.Model(&costRollupGenerationModel{}).
			Where("generation_id = ? AND state = ?", active.GenerationID, CostRollupGenerationActive).
			Updates(map[string]any{
				"state": string(CostRollupGenerationSuperseded), "updated_at_ms": request.CalculatedAtMS,
			})
		if superseded.Error != nil {
			return RebuildCostLedgerReport{}, superseded.Error
		}
		if superseded.RowsAffected != 1 {
			return RebuildCostLedgerReport{}, invalidRecord("active cost generation supersede affected an unexpected row count")
		}
	}
	activated := database.Model(&costRollupGenerationModel{}).
		Where("generation_id = ? AND state = ?", request.GenerationID, CostRollupGenerationBuilding).
		Updates(map[string]any{
			"state": string(CostRollupGenerationActive), "completed_at_ms": request.CalculatedAtMS,
			"updated_at_ms": request.CalculatedAtMS,
		})
	if activated.Error != nil {
		return RebuildCostLedgerReport{}, activated.Error
	}
	if activated.RowsAffected != 1 {
		return RebuildCostLedgerReport{}, invalidRecord("cost generation activation affected an unexpected row count")
	}
	return RebuildCostLedgerReport{
		GenerationID: request.GenerationID, FinalTurns: expected.TurnCount,
		PricedTurns: expected.PricedTurnCount, UnpricedTurns: expected.UnpricedTurnCount,
		EstimatedUSDMicros: expected.EstimatedUSDMicros,
	}, nil
}

func validateCostRebuildRequest(request RebuildCostLedgerRequest) (*time.Location, error) {
	if request.GenerationID == "" || request.ReportingTimezone == "" ||
		request.PricingSource == "" || request.Currency == "" {
		return nil, invalidRecord("cost rebuild identity is incomplete")
	}
	if request.RollupVersion <= 0 || request.CalculatedAtMS < 0 {
		return nil, invalidRecord("cost rebuild version or timestamp is invalid")
	}
	location, err := time.LoadLocation(request.ReportingTimezone)
	if err != nil {
		return nil, invalidRecord("cost rebuild reporting timezone is invalid")
	}
	return location, nil
}

func generationMatchesRequest(model costRollupGenerationModel, request RebuildCostLedgerRequest) bool {
	return model.GenerationID == request.GenerationID &&
		model.ReportingTimezone == request.ReportingTimezone &&
		model.PricingSource == request.PricingSource && model.Currency == request.Currency &&
		model.RollupVersion == request.RollupVersion && model.CreatedAtMS == request.CalculatedAtMS
}

func loadFinalCostFacts(database *gorm.DB) ([]costFactModel, error) {
	var facts []costFactModel
	err := database.Table("turn_usage AS usage").
		Select(`usage.turn_id, turns.session_id, usage.observed_at_ms,
			usage.input_tokens, usage.cached_input_tokens, usage.output_tokens, usage.reasoning_tokens,
			attribution.turn_id AS attribution_turn_id,
			attribution.project_id, attribution.project_display_name AS project_display,
			attribution.project_confidence, attribution.project_source, attribution.project_reason,
			attribution.model_key, attribution.model_display_name AS model_display,
			attribution.model_confidence, attribution.model_source, attribution.model_reason`).
		Joins("JOIN turns ON turns.turn_id = usage.turn_id").
		Joins("LEFT JOIN turn_attributions AS attribution ON attribution.turn_id = usage.turn_id").
		Where("usage.is_final = ?", true).
		Order("usage.turn_id").Scan(&facts).Error
	return facts, err
}

func turnCostForFact(
	ctx context.Context,
	database *gorm.DB,
	request RebuildCostLedgerRequest,
	fact costFactModel,
) (TurnCost, error) {
	base := TurnCost{
		GenerationID: request.GenerationID, TurnID: fact.TurnID,
		CalculatedAtMS: request.CalculatedAtMS,
	}
	if fact.AttributionTurnID == nil {
		base.Status = pricing.CostStatusUnpriced
		base.Reason = pricing.CostReasonMissingAttribution
		return base, nil
	}
	if fact.ModelKey == nil {
		base.Status = pricing.CostStatusUnpriced
		base.Reason = modelAttributionCostReason(fact.ModelReason)
		return base, nil
	}

	var version pricingVersionModel
	err := database.WithContext(ctx).
		Where("source = ? AND currency = ? AND effective_from_ms <= ?",
			request.PricingSource, request.Currency, fact.ObservedAtMS).
		Order("effective_from_ms DESC").Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		base.Status = pricing.CostStatusUnpriced
		base.Reason = pricing.CostReasonCatalogNotEffective
		return base, nil
	}
	if err != nil {
		return TurnCost{}, err
	}
	base.PricingVersion = pointerToString(version.PricingVersion)
	var model modelPriceModel
	err = database.WithContext(ctx).
		Where("pricing_version = ? AND match_kind = ? AND model_pattern = ?",
			version.PricingVersion, ModelMatchExact, *fact.ModelKey).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		base.Status = pricing.CostStatusUnpriced
		base.Reason = pricing.CostReasonModelNotListed
		return base, nil
	}
	if err != nil {
		return TurnCost{}, err
	}
	calculation, err := pricing.Calculate(pricing.Usage{
		InputTokens: fact.InputTokens, CachedInputTokens: fact.CachedInputTokens,
		OutputTokens: fact.OutputTokens, ReasoningTokens: fact.ReasoningTokens,
	}, pricing.Rates{
		InputMicrosPerMillion:       model.InputMicrosPerMillion,
		CachedInputMicrosPerMillion: model.CachedInputMicrosPerMillion,
		OutputMicrosPerMillion:      model.OutputMicrosPerMillion,
	})
	if err != nil {
		return TurnCost{}, err
	}
	base.Status = calculation.Status
	base.Reason = calculation.Reason
	base.EstimatedUSDMicros = calculation.EstimatedUSDMicros
	return base, nil
}

func modelAttributionCostReason(reason *string) pricing.CostReason {
	if reason == nil {
		return pricing.CostReasonMissingAttribution
	}
	switch AttributionReason(*reason) {
	case AttributionReasonConflict:
		return pricing.CostReasonConflictModel
	case AttributionReasonInvalid:
		return pricing.CostReasonInvalidModel
	case AttributionReasonMissing:
		return pricing.CostReasonMissingModel
	default:
		return pricing.CostReasonMissingAttribution
	}
}

func localDayBucketStart(atMS int64, location *time.Location) int64 {
	local := time.UnixMilli(atMS).In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC().UnixMilli()
}

func costDimension(
	identity *string,
	display *string,
	confidence *string,
	source *string,
	reason *string,
) safeDimension {
	safeConfidence := optionalCostDimensionValue(confidence, string(AttributionConfidenceUnknown))
	safeSource := optionalCostDimensionValue(source, string(AttributionSourceMissing))
	safeReason := optionalCostDimensionValue(reason, string(AttributionReasonMissing))
	if identity != nil && *identity != "" && display != nil && *display != "" {
		return safeDimension{
			key: *identity, identity: identity, display: display,
			confidence: safeConfidence, source: safeSource, reason: safeReason,
		}
	}
	return safeDimension{
		key:        "unknown|" + strings.Join([]string{safeConfidence, safeSource, safeReason}, "|"),
		confidence: safeConfidence, source: safeSource, reason: safeReason,
	}
}

func optionalCostDimensionValue(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func recordCostDimension(
	dimensions map[dimensionBucketKey]safeDimension,
	key dimensionBucketKey,
	dimension safeDimension,
) error {
	previous, exists := dimensions[key]
	if !exists {
		dimensions[key] = dimension
		return nil
	}
	if previous.key != dimension.key ||
		!equalStringPointer(previous.identity, dimension.identity) {
		return invalidRecord("cost dimension identity conflicts within one daily bucket")
	}
	if previous.identity == nil {
		if !equalStringPointer(previous.display, dimension.display) ||
			previous.confidence != dimension.confidence || previous.source != dimension.source ||
			previous.reason != dimension.reason {
			return invalidRecord("unknown cost dimension metadata conflicts with its dimension key")
		}
		return nil
	}
	merged := previous
	if !equalStringPointer(previous.display, dimension.display) {
		merged.display = pointerToString(previous.key)
	}
	merged.confidence = conservativeCostDimensionConfidence(previous.confidence, dimension.confidence)
	merged.source = mergeCostDimensionValue(previous.source, dimension.source)
	merged.reason = mergeCostDimensionValue(previous.reason, dimension.reason)
	dimensions[key] = merged
	return nil
}

func conservativeCostDimensionConfidence(left, right string) string {
	if costDimensionConfidenceRank(left) <= costDimensionConfidenceRank(right) {
		return left
	}
	return right
}

func costDimensionConfidenceRank(value string) int {
	switch AttributionConfidence(value) {
	case AttributionConfidenceHigh:
		return 4
	case AttributionConfidenceMedium:
		return 3
	case AttributionConfidenceLow:
		return 2
	case AttributionConfidenceUnknown:
		return 1
	default:
		return 0
	}
}

func mergeCostDimensionValue(left, right string) string {
	if left == right {
		return left
	}
	return costDimensionMixed
}

func accumulatorFor[K comparable](
	groups map[K]*aggregateAccumulator,
	key K,
) *aggregateAccumulator {
	if groups[key] == nil {
		groups[key] = newAggregateAccumulator()
	}
	return groups[key]
}

func reconcileAggregateGroups[K comparable](
	expected RollupTotals,
	groups map[K]*aggregateAccumulator,
	updatedAtMS int64,
) error {
	totals := make([]RollupTotals, 0, len(groups))
	for _, aggregate := range groups {
		value, err := aggregate.totals(updatedAtMS)
		if err != nil {
			return err
		}
		totals = append(totals, value)
	}
	return reconcileRollupTotals(expected, totals)
}

func reconcileRollupTotals(expected RollupTotals, totals []RollupTotals) error {
	turnCount, pricedCount, unpricedCount, estimated := int64(0), int64(0), int64(0), int64(0)
	input, cached, output, reasoning := newNullableSum(), newNullableSum(), newNullableSum(), newNullableSum()
	for _, value := range totals {
		var err error
		turnCount, err = addCostInteger(turnCount, value.TurnCount)
		if err != nil {
			return err
		}
		pricedCount, err = addCostInteger(pricedCount, value.PricedTurnCount)
		if err != nil {
			return err
		}
		unpricedCount, err = addCostInteger(unpricedCount, value.UnpricedTurnCount)
		if err != nil {
			return err
		}
		if value.EstimatedUSDMicros != nil {
			estimated, err = addCostInteger(estimated, *value.EstimatedUSDMicros)
			if err != nil {
				return err
			}
		}
		for sum, component := range map[*nullableSum]*int64{
			&input: value.InputTokens, &cached: value.CachedInputTokens,
			&output: value.OutputTokens, &reasoning: value.ReasoningTokens,
		} {
			if err := sum.add(component); err != nil {
				return err
			}
		}
	}
	if turnCount != expected.TurnCount || pricedCount != expected.PricedTurnCount ||
		unpricedCount != expected.UnpricedTurnCount ||
		!equalInt64Pointer(input.pointer(), expected.InputTokens) ||
		!equalInt64Pointer(cached.pointer(), expected.CachedInputTokens) ||
		!equalInt64Pointer(output.pointer(), expected.OutputTokens) ||
		!equalInt64Pointer(reasoning.pointer(), expected.ReasoningTokens) {
		return invalidRecord("cost rollup token/count reconciliation failed")
	}
	if expected.EstimatedUSDMicros == nil {
		if pricedCount != 0 || estimated != 0 {
			return invalidRecord("cost rollup amount reconciliation failed")
		}
	} else if estimated != *expected.EstimatedUSDMicros {
		return invalidRecord("cost rollup amount reconciliation failed")
	}
	return nil
}

func createCostRows[T any](database *gorm.DB, rows []T, name string) error {
	if len(rows) == 0 {
		return nil
	}
	created := database.CreateInBatches(&rows, 100)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected != int64(len(rows)) {
		return invalidRecord(name + " insert affected an unexpected row count")
	}
	return nil
}

func reconcilePersistedCostGeneration(
	ctx context.Context,
	database *gorm.DB,
	generationID string,
	expectedTotals RollupTotals,
	expectedCosts []turnCostModel,
	expectedSessions []sessionUsageRollupModel,
	expectedDaily []usageDailyModel,
	expectedProjects []projectUsageDailyModel,
	expectedModels []modelUsageDailyModel,
) error {
	storedCosts, err := loadPersistedCostRows[turnCostModel](ctx, database, generationID, "turn_id")
	if err != nil {
		return err
	}
	storedSessions, err := loadPersistedCostRows[sessionUsageRollupModel](ctx, database, generationID, "session_id")
	if err != nil {
		return err
	}
	storedDaily, err := loadPersistedCostRows[usageDailyModel](ctx, database, generationID, "bucket_start_ms")
	if err != nil {
		return err
	}
	storedProjects, err := loadPersistedCostRows[projectUsageDailyModel](
		ctx, database, generationID, "bucket_start_ms, dimension_key",
	)
	if err != nil {
		return err
	}
	storedModels, err := loadPersistedCostRows[modelUsageDailyModel](
		ctx, database, generationID, "bucket_start_ms, dimension_key",
	)
	if err != nil {
		return err
	}
	for name, equal := range map[string]bool{
		"turn costs":      equalPersistedCostRows(expectedCosts, storedCosts),
		"session rollups": equalPersistedCostRows(expectedSessions, storedSessions),
		"daily rollups":   equalPersistedCostRows(expectedDaily, storedDaily),
		"project rollups": equalPersistedCostRows(expectedProjects, storedProjects),
		"model rollups":   equalPersistedCostRows(expectedModels, storedModels),
	} {
		if !equal {
			return invalidRecord(name + " persisted readback differs from shadow generation")
		}
	}
	report, err := costGenerationReport(ctx, database, generationID)
	if err != nil {
		return err
	}
	if report.FinalTurns != expectedTotals.TurnCount ||
		report.PricedTurns != expectedTotals.PricedTurnCount ||
		report.UnpricedTurns != expectedTotals.UnpricedTurnCount ||
		!equalInt64Pointer(report.EstimatedUSDMicros, expectedTotals.EstimatedUSDMicros) {
		return invalidRecord("turn cost persisted reconciliation failed")
	}
	for _, totals := range [][]RollupTotals{
		rollupTotalsFromSessionModels(storedSessions),
		rollupTotalsFromDailyModels(storedDaily),
		rollupTotalsFromProjectModels(storedProjects),
		rollupTotalsFromModelModels(storedModels),
	} {
		if err := reconcileRollupTotals(expectedTotals, totals); err != nil {
			return err
		}
	}
	return nil
}

func loadPersistedCostRows[T any](
	ctx context.Context,
	database *gorm.DB,
	generationID string,
	order string,
) ([]T, error) {
	rows := make([]T, 0)
	err := database.WithContext(ctx).Where("generation_id = ?", generationID).Order(order).Find(&rows).Error
	return rows, err
}

func equalPersistedCostRows[T any](expected, stored []T) bool {
	if len(expected) != len(stored) {
		return false
	}
	for index := range expected {
		if !reflect.DeepEqual(expected[index], stored[index]) {
			return false
		}
	}
	return true
}

func rollupTotalsFromSessionModels(models []sessionUsageRollupModel) []RollupTotals {
	totals := make([]RollupTotals, 0, len(models))
	for _, model := range models {
		totals = append(totals, totalsFromModel(model.Totals))
	}
	return totals
}

func rollupTotalsFromDailyModels(models []usageDailyModel) []RollupTotals {
	totals := make([]RollupTotals, 0, len(models))
	for _, model := range models {
		totals = append(totals, totalsFromModel(model.Totals))
	}
	return totals
}

func rollupTotalsFromProjectModels(models []projectUsageDailyModel) []RollupTotals {
	totals := make([]RollupTotals, 0, len(models))
	for _, model := range models {
		totals = append(totals, totalsFromModel(model.Totals))
	}
	return totals
}

func rollupTotalsFromModelModels(models []modelUsageDailyModel) []RollupTotals {
	totals := make([]RollupTotals, 0, len(models))
	for _, model := range models {
		totals = append(totals, totalsFromModel(model.Totals))
	}
	return totals
}

func sessionRollupModels(
	request RebuildCostLedgerRequest,
	groups map[string]*aggregateAccumulator,
) ([]sessionUsageRollupModel, error) {
	keys := sortedStringKeys(groups)
	models := make([]sessionUsageRollupModel, 0, len(keys))
	for _, key := range keys {
		totals, err := groups[key].totals(request.CalculatedAtMS)
		if err != nil {
			return nil, err
		}
		models = append(models, sessionUsageRollupModel{
			GenerationID: request.GenerationID, SessionID: key, Totals: totalsModel(totals),
		})
	}
	return models, nil
}

func dailyRollupModels(
	request RebuildCostLedgerRequest,
	groups map[int64]*aggregateAccumulator,
) ([]usageDailyModel, error) {
	keys := make([]int64, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	models := make([]usageDailyModel, 0, len(keys))
	for _, key := range keys {
		totals, err := groups[key].totals(request.CalculatedAtMS)
		if err != nil {
			return nil, err
		}
		models = append(models, usageDailyModel{
			GenerationID: request.GenerationID, BucketStartMS: key,
			ReportingTimezone: request.ReportingTimezone, Totals: totalsModel(totals),
		})
	}
	return models, nil
}

func projectRollupModels(
	request RebuildCostLedgerRequest,
	groups map[dimensionBucketKey]*aggregateAccumulator,
	dimensions map[dimensionBucketKey]safeDimension,
) ([]projectUsageDailyModel, error) {
	keys := sortedDimensionKeys(groups)
	models := make([]projectUsageDailyModel, 0, len(keys))
	for _, key := range keys {
		totals, err := groups[key].totals(request.CalculatedAtMS)
		if err != nil {
			return nil, err
		}
		dimension := dimensions[key]
		models = append(models, projectUsageDailyModel{
			GenerationID: request.GenerationID, BucketStartMS: key.bucketStartMS,
			ReportingTimezone: request.ReportingTimezone, DimensionKey: dimension.key,
			ProjectID: dimension.identity, ProjectDisplayName: dimension.display,
			AttributionConfidence: dimension.confidence, AttributionSource: dimension.source,
			AttributionReason: dimension.reason, Totals: totalsModel(totals),
		})
	}
	return models, nil
}

func modelRollupModels(
	request RebuildCostLedgerRequest,
	groups map[dimensionBucketKey]*aggregateAccumulator,
	dimensions map[dimensionBucketKey]safeDimension,
) ([]modelUsageDailyModel, error) {
	keys := sortedDimensionKeys(groups)
	models := make([]modelUsageDailyModel, 0, len(keys))
	for _, key := range keys {
		totals, err := groups[key].totals(request.CalculatedAtMS)
		if err != nil {
			return nil, err
		}
		dimension := dimensions[key]
		models = append(models, modelUsageDailyModel{
			GenerationID: request.GenerationID, BucketStartMS: key.bucketStartMS,
			ReportingTimezone: request.ReportingTimezone, DimensionKey: dimension.key,
			ModelKey: dimension.identity, ModelDisplayName: dimension.display,
			AttributionConfidence: dimension.confidence, AttributionSource: dimension.source,
			AttributionReason: dimension.reason, Totals: totalsModel(totals),
		})
	}
	return models, nil
}

func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedDimensionKeys(values map[dimensionBucketKey]*aggregateAccumulator) []dimensionBucketKey {
	keys := make([]dimensionBucketKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].bucketStartMS != keys[right].bucketStartMS {
			return keys[left].bucketStartMS < keys[right].bucketStartMS
		}
		return keys[left].dimensionKey < keys[right].dimensionKey
	})
	return keys
}

func totalsModel(totals RollupTotals) rollupTotalsModel {
	return rollupTotalsModel{
		TurnCount: totals.TurnCount, InputTokens: totals.InputTokens,
		CachedInputTokens: totals.CachedInputTokens, OutputTokens: totals.OutputTokens,
		ReasoningTokens: totals.ReasoningTokens, TotalTokens: totals.TotalTokens,
		EstimatedUSDMicros: totals.EstimatedUSDMicros,
		PricedTurnCount:    totals.PricedTurnCount, UnpricedTurnCount: totals.UnpricedTurnCount,
		FirstActivityAtMS: totals.FirstActivityAtMS, LastActivityAtMS: totals.LastActivityAtMS,
		UpdatedAtMS: totals.UpdatedAtMS,
	}
}

func totalsFromModel(model rollupTotalsModel) RollupTotals {
	return RollupTotals{
		TurnCount: model.TurnCount, InputTokens: model.InputTokens,
		CachedInputTokens: model.CachedInputTokens, OutputTokens: model.OutputTokens,
		ReasoningTokens: model.ReasoningTokens, TotalTokens: model.TotalTokens,
		EstimatedUSDMicros: model.EstimatedUSDMicros,
		PricedTurnCount:    model.PricedTurnCount, UnpricedTurnCount: model.UnpricedTurnCount,
		FirstActivityAtMS: model.FirstActivityAtMS, LastActivityAtMS: model.LastActivityAtMS,
		UpdatedAtMS: model.UpdatedAtMS,
	}
}

func turnCostModelFromRecord(cost TurnCost) turnCostModel {
	return turnCostModel{
		GenerationID: cost.GenerationID, TurnID: cost.TurnID,
		PricingVersion: cost.PricingVersion, EstimatedUSDMicros: cost.EstimatedUSDMicros,
		PricingStatus: string(cost.Status), PricingReason: string(cost.Reason),
		CalculatedAtMS: cost.CalculatedAtMS,
	}
}

func addCostInteger(left, right int64) (int64, error) {
	if left < 0 || right < 0 {
		return 0, pricing.ErrInvalidCalculation
	}
	if right > maxCostInteger-left {
		return 0, pricing.ErrCostOverflow
	}
	return left + right, nil
}

func pointerToString(value string) *string { return &value }

func costGenerationReport(
	ctx context.Context,
	database *gorm.DB,
	generationID string,
) (RebuildCostLedgerReport, error) {
	var models []turnCostModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).Find(&models).Error; err != nil {
		return RebuildCostLedgerReport{}, err
	}
	report := RebuildCostLedgerReport{GenerationID: generationID, FinalTurns: int64(len(models))}
	estimated := int64(0)
	for _, model := range models {
		if pricing.CostStatus(model.PricingStatus) == pricing.CostStatusPriced {
			report.PricedTurns++
			if model.EstimatedUSDMicros == nil {
				return RebuildCostLedgerReport{}, invalidRecord("stored priced turn is missing amount")
			}
			var err error
			estimated, err = addCostInteger(estimated, *model.EstimatedUSDMicros)
			if err != nil {
				return RebuildCostLedgerReport{}, err
			}
		} else {
			report.UnpricedTurns++
		}
	}
	if report.PricedTurns > 0 {
		report.EstimatedUSDMicros = &estimated
	}
	return report, nil
}

// ActiveCostLedger 返回指定时区唯一 active generation 的完整 typed readback。
func (repository *Repository) ActiveCostLedger(
	ctx context.Context,
	reportingTimezone string,
) (CostLedgerSnapshot, error) {
	if repository == nil || repository.database == nil {
		return CostLedgerSnapshot{}, ErrInvalidRepository
	}
	if reportingTimezone == "" {
		return CostLedgerSnapshot{}, invalidRecord("reporting timezone must not be empty")
	}
	if _, err := time.LoadLocation(reportingTimezone); err != nil {
		return CostLedgerSnapshot{}, invalidRecord("reporting timezone is invalid")
	}
	var snapshot CostLedgerSnapshot
	err := repository.database.View(ctx, func(ctx context.Context, database storesqlite.ReadConn) error {
		var generation costRollupGenerationModel
		err := database.WithContext(ctx).Where(
			"reporting_timezone = ? AND state = ?", reportingTimezone, CostRollupGenerationActive,
		).Take(&generation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		snapshot.Generation = generationFromModel(generation)
		return loadCostLedgerSnapshot(ctx, database, &snapshot)
	})
	return snapshot, err
}

func loadCostLedgerSnapshot(
	ctx context.Context,
	database *gorm.DB,
	snapshot *CostLedgerSnapshot,
) error {
	generationID := snapshot.Generation.GenerationID
	var costs []turnCostModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).
		Order("turn_id").Find(&costs).Error; err != nil {
		return err
	}
	snapshot.TurnCosts = make([]TurnCost, 0, len(costs))
	for _, model := range costs {
		snapshot.TurnCosts = append(snapshot.TurnCosts, TurnCost{
			GenerationID: model.GenerationID, TurnID: model.TurnID,
			PricingVersion: model.PricingVersion, EstimatedUSDMicros: model.EstimatedUSDMicros,
			Status: pricing.CostStatus(model.PricingStatus), Reason: pricing.CostReason(model.PricingReason),
			CalculatedAtMS: model.CalculatedAtMS,
		})
	}
	var sessions []sessionUsageRollupModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).
		Order("session_id").Find(&sessions).Error; err != nil {
		return err
	}
	snapshot.SessionRollups = make([]SessionUsageRollup, 0, len(sessions))
	for _, model := range sessions {
		snapshot.SessionRollups = append(snapshot.SessionRollups, SessionUsageRollup{
			GenerationID: model.GenerationID, SessionID: model.SessionID,
			RollupTotals: totalsFromModel(model.Totals),
		})
	}
	var days []usageDailyModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).
		Order("bucket_start_ms").Find(&days).Error; err != nil {
		return err
	}
	snapshot.DailyRollups = make([]UsageDaily, 0, len(days))
	for _, model := range days {
		snapshot.DailyRollups = append(snapshot.DailyRollups, UsageDaily{
			GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
			ReportingTimezone: model.ReportingTimezone, RollupTotals: totalsFromModel(model.Totals),
		})
	}
	var projects []projectUsageDailyModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).
		Order("bucket_start_ms").Order("dimension_key").Find(&projects).Error; err != nil {
		return err
	}
	snapshot.ProjectDaily = make([]ProjectUsageDaily, 0, len(projects))
	for _, model := range projects {
		snapshot.ProjectDaily = append(snapshot.ProjectDaily, ProjectUsageDaily{
			GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
			ReportingTimezone: model.ReportingTimezone, DimensionKey: model.DimensionKey,
			ProjectID: model.ProjectID, ProjectDisplayName: model.ProjectDisplayName,
			AttributionConfidence: model.AttributionConfidence,
			AttributionSource:     model.AttributionSource, AttributionReason: model.AttributionReason,
			RollupTotals: totalsFromModel(model.Totals),
		})
	}
	var models []modelUsageDailyModel
	if err := database.WithContext(ctx).Where("generation_id = ?", generationID).
		Order("bucket_start_ms").Order("dimension_key").Find(&models).Error; err != nil {
		return err
	}
	snapshot.ModelDaily = make([]ModelUsageDaily, 0, len(models))
	for _, model := range models {
		snapshot.ModelDaily = append(snapshot.ModelDaily, ModelUsageDaily{
			GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
			ReportingTimezone: model.ReportingTimezone, DimensionKey: model.DimensionKey,
			ModelKey: model.ModelKey, ModelDisplayName: model.ModelDisplayName,
			AttributionConfidence: model.AttributionConfidence,
			AttributionSource:     model.AttributionSource, AttributionReason: model.AttributionReason,
			RollupTotals: totalsFromModel(model.Totals),
		})
	}
	return nil
}

func generationFromModel(model costRollupGenerationModel) CostRollupGeneration {
	return CostRollupGeneration{
		GenerationID: model.GenerationID, ReportingTimezone: model.ReportingTimezone,
		PricingSource: model.PricingSource, Currency: model.Currency,
		RollupVersion: model.RollupVersion, State: CostRollupGenerationState(model.State),
		CreatedAtMS: model.CreatedAtMS, CompletedAtMS: model.CompletedAtMS, UpdatedAtMS: model.UpdatedAtMS,
	}
}
