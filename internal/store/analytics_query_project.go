package store

import (
	"context"
	"errors"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const projectAnalyticsGroupSelect = `
	project.dimension_key AS dimension_key,
	MIN(project.project_id) AS project_id_min,
	MAX(project.project_id) AS project_id_max,
	CASE
		WHEN MIN(project.project_id) IS NULL THEN NULL
		WHEN MIN(project.project_display_name) = MAX(project.project_display_name)
			THEN MIN(project.project_display_name)
		ELSE MIN(project.project_id)
	END AS project_display_name,
	MIN(CASE project.attribution_confidence
		WHEN 'unknown' THEN 0 WHEN 'low' THEN 1 WHEN 'medium' THEN 2 WHEN 'high' THEN 3
		ELSE -1 END) AS confidence_rank,
	MIN(project.attribution_source) AS source_min,
	MAX(project.attribution_source) AS source_max,
	MIN(project.attribution_reason) AS reason_min,
	MAX(project.attribution_reason) AS reason_max,
	COUNT(*) AS rollup_rows,
	COALESCE(SUM(project.turn_count), 0) AS turn_count,
	SUM(project.input_tokens) AS input_tokens,
	SUM(CASE WHEN project.input_tokens IS NULL THEN 1 ELSE 0 END) AS input_nulls,
	SUM(project.cached_input_tokens) AS cached_input_tokens,
	SUM(CASE WHEN project.cached_input_tokens IS NULL THEN 1 ELSE 0 END) AS cached_nulls,
	SUM(project.output_tokens) AS output_tokens,
	SUM(CASE WHEN project.output_tokens IS NULL THEN 1 ELSE 0 END) AS output_nulls,
	SUM(project.reasoning_tokens) AS reasoning_tokens,
	SUM(CASE WHEN project.reasoning_tokens IS NULL THEN 1 ELSE 0 END) AS reasoning_nulls,
	SUM(project.total_tokens) AS total_tokens,
	SUM(CASE WHEN project.total_tokens IS NULL THEN 1 ELSE 0 END) AS total_nulls,
	SUM(project.estimated_usd_micros) AS estimated_usd_micros,
	COALESCE(SUM(project.priced_turn_count), 0) AS priced_turn_count,
	COALESCE(SUM(project.unpriced_turn_count), 0) AS unpriced_turn_count,
	MIN(project.first_activity_at_ms) AS first_activity_at_ms,
	MAX(project.last_activity_at_ms) AS last_activity_at_ms,
	COALESCE(MAX(project.updated_at_ms), 0) AS updated_at_ms`

const analyticsNormalizedAggregateSelect = `
	COUNT(*) AS rollup_rows,
	COALESCE(SUM(rollup.turn_count), 0) AS turn_count,
	SUM(rollup.input_tokens) AS input_tokens,
	COALESCE(SUM(CASE WHEN rollup.input_tokens IS NULL THEN 1 ELSE 0 END), 0) AS input_nulls,
	SUM(rollup.cached_input_tokens) AS cached_input_tokens,
	COALESCE(SUM(CASE WHEN rollup.cached_input_tokens IS NULL THEN 1 ELSE 0 END), 0) AS cached_nulls,
	SUM(rollup.output_tokens) AS output_tokens,
	COALESCE(SUM(CASE WHEN rollup.output_tokens IS NULL THEN 1 ELSE 0 END), 0) AS output_nulls,
	SUM(rollup.reasoning_tokens) AS reasoning_tokens,
	COALESCE(SUM(CASE WHEN rollup.reasoning_tokens IS NULL THEN 1 ELSE 0 END), 0) AS reasoning_nulls,
	SUM(rollup.total_tokens) AS total_tokens,
	COALESCE(SUM(CASE WHEN rollup.total_tokens IS NULL THEN 1 ELSE 0 END), 0) AS total_nulls,
	SUM(rollup.estimated_usd_micros) AS estimated_usd_micros,
	COALESCE(SUM(rollup.priced_turn_count), 0) AS priced_turn_count,
	COALESCE(SUM(rollup.unpriced_turn_count), 0) AS unpriced_turn_count,
	MIN(rollup.first_activity_at_ms) AS first_activity_at_ms,
	MAX(rollup.last_activity_at_ms) AS last_activity_at_ms,
	COALESCE(MAX(rollup.updated_at_ms), 0) AS updated_at_ms`

const projectAnalyticsTotalsColumns = `
	project.turn_count AS turn_count,
	project.input_tokens AS input_tokens,
	project.cached_input_tokens AS cached_input_tokens,
	project.output_tokens AS output_tokens,
	project.reasoning_tokens AS reasoning_tokens,
	project.total_tokens AS total_tokens,
	project.estimated_usd_micros AS estimated_usd_micros,
	project.priced_turn_count AS priced_turn_count,
	project.unpriced_turn_count AS unpriced_turn_count,
	project.first_activity_at_ms AS first_activity_at_ms,
	project.last_activity_at_ms AS last_activity_at_ms,
	project.updated_at_ms AS updated_at_ms`

const usageAnalyticsTotalsColumns = `
	usage.turn_count AS turn_count,
	usage.input_tokens AS input_tokens,
	usage.cached_input_tokens AS cached_input_tokens,
	usage.output_tokens AS output_tokens,
	usage.reasoning_tokens AS reasoning_tokens,
	usage.total_tokens AS total_tokens,
	usage.estimated_usd_micros AS estimated_usd_micros,
	usage.priced_turn_count AS priced_turn_count,
	usage.unpriced_turn_count AS unpriced_turn_count,
	usage.first_activity_at_ms AS first_activity_at_ms,
	usage.last_activity_at_ms AS last_activity_at_ms,
	usage.updated_at_ms AS updated_at_ms`

const projectGroupAnalyticsTotalsColumns = `
	project_group.turn_count AS turn_count,
	project_group.input_tokens AS input_tokens,
	project_group.cached_input_tokens AS cached_input_tokens,
	project_group.output_tokens AS output_tokens,
	project_group.reasoning_tokens AS reasoning_tokens,
	project_group.total_tokens AS total_tokens,
	project_group.estimated_usd_micros AS estimated_usd_micros,
	project_group.priced_turn_count AS priced_turn_count,
	project_group.unpriced_turn_count AS unpriced_turn_count,
	project_group.first_activity_at_ms AS first_activity_at_ms,
	project_group.last_activity_at_ms AS last_activity_at_ms,
	project_group.updated_at_ms AS updated_at_ms`

type projectAnalyticsProjection struct {
	DimensionKey       string                           `gorm:"column:dimension_key"`
	ProjectIDMin       *string                          `gorm:"column:project_id_min"`
	ProjectIDMax       *string                          `gorm:"column:project_id_max"`
	ProjectDisplayName *string                          `gorm:"column:project_display_name"`
	ConfidenceRank     int                              `gorm:"column:confidence_rank"`
	SourceMin          string                           `gorm:"column:source_min"`
	SourceMax          string                           `gorm:"column:source_max"`
	ReasonMin          string                           `gorm:"column:reason_min"`
	ReasonMax          string                           `gorm:"column:reason_max"`
	Totals             sessionAnalyticsTotalsProjection `gorm:"embedded"`
}

func (repository *Repository) ListProjectAnalytics(
	ctx context.Context,
	filter ProjectAnalyticsFilter,
) (ProjectAnalyticsPage, error) {
	if repository == nil || repository.database == nil {
		return ProjectAnalyticsPage{}, ErrInvalidRepository
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProjectAnalyticsPage{}, err
	}
	if err := validateProjectAnalyticsFilter(filter); err != nil {
		return ProjectAnalyticsPage{}, err
	}
	page := ProjectAnalyticsPage{
		Records: make([]ProjectAnalyticsRecord, 0), PricingVersions: make([]string, 0),
	}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		generation, err := loadProjectAnalyticsGeneration(database, filter.Range.ReportingTimezone)
		if err != nil {
			return err
		}
		page.Generation = generationFromModel(generation)
		global, err := reconcileProjectAnalyticsRange(database, filter.Range, generation)
		if err != nil {
			return err
		}
		page.GlobalTotals = global
		page.MatchedTotals, err = aggregateProjectGroupAnalyticsTotals(
			database, projectAnalyticsGroupQuery(database, filter, generation),
		)
		if err != nil {
			return err
		}
		grouped := projectAnalyticsGroupQuery(database, filter, generation)
		if err := grouped.Count(&page.MatchedCount).Error; err != nil {
			return err
		}
		query := projectAnalyticsGroupQuery(database, filter, generation)
		query = applyProjectAnalyticsCursor(query, filter)
		query = orderProjectAnalytics(query, filter).Limit(filter.Limit + 1)
		var projections []projectAnalyticsProjection
		if err := query.Scan(&projections).Error; err != nil {
			return err
		}
		hasMore := len(projections) > filter.Limit
		if hasMore {
			projections = projections[:filter.Limit]
		}
		for _, projection := range projections {
			record, err := projectRecordFromProjection(projection)
			if err != nil {
				return err
			}
			page.Records = append(page.Records, record)
		}
		page.PageTotals, err = aggregateProjectPageTotals(page.Records)
		if err != nil {
			return err
		}
		if hasMore && len(page.Records) > 0 {
			page.NextCursor = projectCursorForRecord(page.Records[len(page.Records)-1], filter.SortField)
		}
		page.PricingVersions, err = loadAnalyticsPricingVersions(database, filter.Range, generation.GenerationID)
		return err
	})
	return page, err
}

func (repository *Repository) ProjectAnalytics(
	ctx context.Context,
	filter ProjectAnalyticsDetailFilter,
) (ProjectAnalyticsSnapshot, error) {
	if repository == nil || repository.database == nil {
		return ProjectAnalyticsSnapshot{}, ErrInvalidRepository
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProjectAnalyticsSnapshot{}, err
	}
	if _, err := validateAnalyticsRange(filter.Range); err != nil {
		return ProjectAnalyticsSnapshot{}, err
	}
	if filter.DimensionKey == "" || len(filter.DimensionKey) > 512 {
		return ProjectAnalyticsSnapshot{}, invalidRecord("project analytics detail identity is invalid")
	}
	result := ProjectAnalyticsSnapshot{
		Daily: make([]ProjectUsageDaily, 0), PricingVersions: make([]string, 0),
	}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		generation, err := loadProjectAnalyticsGeneration(database, filter.Range.ReportingTimezone)
		if err != nil {
			return err
		}
		result.Generation = generationFromModel(generation)
		result.GlobalTotals, err = reconcileProjectAnalyticsRange(database, filter.Range, generation)
		if err != nil {
			return err
		}
		listFilter := ProjectAnalyticsFilter{
			Range: filter.Range, Limit: 1, SortField: ProjectAnalyticsSortTotalTokens,
			SortDirection: AnalyticsSortDescending,
		}
		var projection projectAnalyticsProjection
		query := projectAnalyticsGroupQuery(database, listFilter, generation).
			Where("project_group.dimension_key = ?", filter.DimensionKey).
			Limit(1).Scan(&projection)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected != 1 {
			return ErrNotFound
		}
		result.Record, err = projectRecordFromProjection(projection)
		if err != nil {
			return err
		}
		var models []projectUsageDailyModel
		if err := database.Where(
			"generation_id = ? AND reporting_timezone = ? AND dimension_key = ? AND bucket_start_ms >= ? AND bucket_start_ms < ?",
			generation.GenerationID, filter.Range.ReportingTimezone, filter.DimensionKey,
			filter.Range.StartAtMS, filter.Range.EndAtMS,
		).Order("bucket_start_ms").Find(&models).Error; err != nil {
			return err
		}
		for _, model := range models {
			daily, err := projectDailyFromModel(model, filter.Range, generation.GenerationID)
			if err != nil {
				return err
			}
			result.Daily = append(result.Daily, daily)
		}
		dailyTotals, err := aggregateProjectDailyTotals(result.Daily)
		if err != nil {
			return err
		}
		if !equalAnalyticsTotals(dailyTotals, result.Record.Totals) {
			return invalidRecord("project detail daily reconciliation failed")
		}
		result.PricingVersions, err = loadAnalyticsPricingVersions(database, filter.Range, generation.GenerationID)
		return err
	})
	return result, err
}

func validateProjectAnalyticsFilter(filter ProjectAnalyticsFilter) error {
	if _, err := validateAnalyticsRange(filter.Range); err != nil {
		return err
	}
	if filter.Limit < 1 || filter.Limit > 100 ||
		(filter.SortField != ProjectAnalyticsSortLastActivity &&
			filter.SortField != ProjectAnalyticsSortTotalTokens &&
			filter.SortField != ProjectAnalyticsSortEstimatedCost &&
			filter.SortField != ProjectAnalyticsSortDisplayName) ||
		(filter.SortDirection != AnalyticsSortAscending &&
			filter.SortDirection != AnalyticsSortDescending) {
		return invalidRecord("project analytics filter is invalid")
	}
	if err := validateAnalyticsDimensionValues(filter.ProjectIDs); err != nil {
		return err
	}
	if len(filter.Confidences) > 4 || hasDuplicateStoreStrings(filter.Confidences) {
		return invalidRecord("project confidence filter is invalid")
	}
	for _, confidence := range filter.Confidences {
		if !validProjectConfidence(confidence) {
			return invalidRecord("project confidence filter is invalid")
		}
	}
	if filter.Cursor != nil {
		if filter.Cursor.DimensionKey == "" || len(filter.Cursor.DimensionKey) > 512 {
			return invalidRecord("project analytics cursor identity is invalid")
		}
		if filter.Cursor.Null {
			if filter.Cursor.NumericValue != nil || filter.Cursor.TextValue != nil {
				return invalidRecord("project analytics null cursor is invalid")
			}
		} else if filter.SortField == ProjectAnalyticsSortDisplayName {
			if filter.Cursor.TextValue == nil || *filter.Cursor.TextValue == "" ||
				filter.Cursor.NumericValue != nil {
				return invalidRecord("project analytics text cursor is invalid")
			}
		} else if filter.Cursor.NumericValue == nil || *filter.Cursor.NumericValue < 0 ||
			filter.Cursor.TextValue != nil {
			return invalidRecord("project analytics numeric cursor is invalid")
		}
	}
	return nil
}

func loadProjectAnalyticsGeneration(
	database *gorm.DB,
	reportingTimezone string,
) (costRollupGenerationModel, error) {
	var generation costRollupGenerationModel
	if err := database.Where(
		"reporting_timezone = ? AND state = ?", reportingTimezone, CostRollupGenerationActive,
	).Take(&generation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return costRollupGenerationModel{}, ErrAnalyticsUnavailable
		}
		return costRollupGenerationModel{}, err
	}
	return generation, nil
}

func buildProjectAnalyticsDailyQuery(
	database *gorm.DB,
	filter ProjectAnalyticsFilter,
	generation costRollupGenerationModel,
) *gorm.DB {
	query := database.Table("project_usage_daily AS project").Where(
		"project.generation_id = ? AND project.reporting_timezone = ? AND project.bucket_start_ms >= ? AND project.bucket_start_ms < ?",
		generation.GenerationID, filter.Range.ReportingTimezone,
		filter.Range.StartAtMS, filter.Range.EndAtMS,
	)
	if len(filter.ProjectIDs) > 0 {
		query = query.Where("project.project_id IN ?", filter.ProjectIDs)
	}
	return query
}

func projectAnalyticsGroupQuery(
	database *gorm.DB,
	filter ProjectAnalyticsFilter,
	generation costRollupGenerationModel,
) *gorm.DB {
	subquery := buildProjectAnalyticsDailyQuery(database, filter, generation).
		Select(projectAnalyticsGroupSelect).Group("project.dimension_key")
	query := database.Table("(?) AS project_group", subquery)
	if len(filter.Confidences) > 0 {
		ranks := make([]int, 0, len(filter.Confidences))
		for _, confidence := range filter.Confidences {
			ranks = append(ranks, projectConfidenceRank(confidence))
		}
		query = query.Where("project_group.confidence_rank IN ?", ranks)
	}
	return query
}

func aggregateProjectAnalyticsTotals(database *gorm.DB, query *gorm.DB) (RollupTotals, error) {
	normalized := query.Select(projectAnalyticsTotalsColumns)
	return aggregateNormalizedAnalyticsTotals(database, normalized)
}

func aggregateUsageAnalyticsTotals(database *gorm.DB, query *gorm.DB) (RollupTotals, error) {
	normalized := query.Select(usageAnalyticsTotalsColumns)
	return aggregateNormalizedAnalyticsTotals(database, normalized)
}

func aggregateProjectGroupAnalyticsTotals(database *gorm.DB, query *gorm.DB) (RollupTotals, error) {
	normalized := query.Select(projectGroupAnalyticsTotalsColumns)
	return aggregateNormalizedAnalyticsTotals(database, normalized)
}

func aggregateNormalizedAnalyticsTotals(database *gorm.DB, normalized *gorm.DB) (RollupTotals, error) {
	var projection sessionAnalyticsTotalsProjection
	if err := database.Table("(?) AS rollup", normalized).
		Select(analyticsNormalizedAggregateSelect).Scan(&projection).Error; err != nil {
		return RollupTotals{}, err
	}
	return sessionTotalsFromProjection(projection)
}

func reconcileProjectAnalyticsRange(
	database *gorm.DB,
	filter AnalyticsRange,
	generation costRollupGenerationModel,
) (RollupTotals, error) {
	if err := validateProjectAnalyticsRows(database, filter, generation.GenerationID); err != nil {
		return RollupTotals{}, err
	}
	allProjectFilter := ProjectAnalyticsFilter{Range: filter}
	projects, err := aggregateProjectAnalyticsTotals(
		database, buildProjectAnalyticsDailyQuery(database, allProjectFilter, generation),
	)
	if err != nil {
		return RollupTotals{}, err
	}
	usageQuery := database.Table("usage_daily AS usage").Where(
		"usage.generation_id = ? AND usage.reporting_timezone = ? AND usage.bucket_start_ms >= ? AND usage.bucket_start_ms < ?",
		generation.GenerationID, filter.ReportingTimezone, filter.StartAtMS, filter.EndAtMS,
	)
	global, err := aggregateUsageAnalyticsTotals(database, usageQuery)
	if err != nil {
		return RollupTotals{}, err
	}
	if !equalAnalyticsTotals(projects, global) {
		return RollupTotals{}, invalidRecord("project analytics global reconciliation failed")
	}
	return global, nil
}

func validateProjectAnalyticsRows(
	database *gorm.DB,
	filter AnalyticsRange,
	generationID string,
) error {
	confidences := []string{
		string(AttributionConfidenceHigh), string(AttributionConfidenceMedium),
		string(AttributionConfidenceLow), string(AttributionConfidenceUnknown),
	}
	sources := []string{
		string(AttributionSourceSessionIDFallback), string(AttributionSourceRegisteredRoot),
		string(AttributionSourceCWDPathDigest), string(AttributionSourceModelCanonical),
		string(AttributionSourceModelAlias), string(AttributionSourceConflict),
		string(AttributionSourceMissing), string(AttributionSourceInvalidPath),
		string(AttributionSourceInvalidModel), costDimensionMixed,
	}
	reasons := []string{
		string(AttributionReasonStableIdentity), string(AttributionReasonRootMatched),
		string(AttributionReasonPathDerived), string(AttributionReasonObserved),
		string(AttributionReasonConflict), string(AttributionReasonMissing),
		string(AttributionReasonInvalid), costDimensionMixed,
	}
	var invalid int64
	err := database.Table("project_usage_daily AS project").
		Where(
			"project.generation_id = ? AND project.reporting_timezone = ? AND project.bucket_start_ms >= ? AND project.bucket_start_ms < ?",
			generationID, filter.ReportingTimezone, filter.StartAtMS, filter.EndAtMS,
		).
		Where(
			`project.attribution_confidence NOT IN ?
				OR project.attribution_source NOT IN ?
				OR project.attribution_reason NOT IN ?
				OR (project.project_id IS NULL AND project.dimension_key !=
					'unknown|' || project.attribution_confidence || '|' ||
					project.attribution_source || '|' || project.attribution_reason)`,
			confidences, sources, reasons,
		).Count(&invalid).Error
	if err != nil {
		return err
	}
	if invalid != 0 {
		return invalidRecord("stored project analytics attribution is invalid")
	}
	return nil
}

func projectRecordFromProjection(
	projection projectAnalyticsProjection,
) (ProjectAnalyticsRecord, error) {
	if projection.DimensionKey == "" || projection.Totals.RollupRows <= 0 {
		return ProjectAnalyticsRecord{}, invalidRecord("stored project dimension is invalid")
	}
	confidence := projectConfidenceFromRank(projection.ConfidenceRank)
	if confidence == "" {
		return ProjectAnalyticsRecord{}, invalidRecord("stored project confidence is invalid")
	}
	if !validProjectAttributionSource(projection.SourceMin) ||
		!validProjectAttributionSource(projection.SourceMax) ||
		!validProjectAttributionReason(projection.ReasonMin) ||
		!validProjectAttributionReason(projection.ReasonMax) {
		return ProjectAnalyticsRecord{}, invalidRecord("stored project attribution is invalid")
	}
	source := mergedProjectAttributionValue(projection.SourceMin, projection.SourceMax)
	reason := mergedProjectAttributionValue(projection.ReasonMin, projection.ReasonMax)
	if !validProjectAttributionSource(source) || !validProjectAttributionReason(reason) {
		return ProjectAnalyticsRecord{}, invalidRecord("stored project attribution is invalid")
	}
	record := ProjectAnalyticsRecord{
		DimensionKey: projection.DimensionKey, AttributionConfidence: confidence,
		AttributionSource: source, AttributionReason: reason,
	}
	switch {
	case projection.ProjectIDMin == nil && projection.ProjectIDMax == nil &&
		projection.ProjectDisplayName == nil:
		if projection.DimensionKey != "unknown|"+confidence+"|"+source+"|"+reason {
			return ProjectAnalyticsRecord{}, invalidRecord("stored unknown project dimension is inconsistent")
		}
	case projection.ProjectIDMin != nil && projection.ProjectIDMax != nil &&
		*projection.ProjectIDMin == *projection.ProjectIDMax &&
		*projection.ProjectIDMin == projection.DimensionKey && projection.ProjectDisplayName != nil &&
		*projection.ProjectDisplayName != "":
		record.ProjectID = cloneStringPointerStore(projection.ProjectIDMin)
		record.ProjectDisplayName = cloneStringPointerStore(projection.ProjectDisplayName)
	default:
		return ProjectAnalyticsRecord{}, invalidRecord("stored project identity tuple is inconsistent")
	}
	totals, err := sessionTotalsFromProjection(projection.Totals)
	if err != nil {
		return ProjectAnalyticsRecord{}, err
	}
	record.Totals = totals
	return record, nil
}

func projectDailyFromModel(
	model projectUsageDailyModel,
	filter AnalyticsRange,
	generationID string,
) (ProjectUsageDaily, error) {
	if model.GenerationID != generationID || model.ReportingTimezone != filter.ReportingTimezone ||
		model.BucketStartMS < filter.StartAtMS || model.BucketStartMS >= filter.EndAtMS ||
		model.DimensionKey == "" {
		return ProjectUsageDaily{}, invalidRecord("stored project daily identity is invalid")
	}
	if (model.ProjectID == nil) != (model.ProjectDisplayName == nil) ||
		(model.ProjectID != nil && (*model.ProjectID != model.DimensionKey || *model.ProjectDisplayName == "")) {
		return ProjectUsageDaily{}, invalidRecord("stored project daily identity tuple is invalid")
	}
	if !validProjectConfidence(model.AttributionConfidence) ||
		!validProjectAttributionSource(model.AttributionSource) ||
		!validProjectAttributionReason(model.AttributionReason) {
		return ProjectUsageDaily{}, invalidRecord("stored project daily attribution is invalid")
	}
	if model.ProjectID == nil && model.DimensionKey != "unknown|"+model.AttributionConfidence+"|"+
		model.AttributionSource+"|"+model.AttributionReason {
		return ProjectUsageDaily{}, invalidRecord("stored unknown project daily dimension is inconsistent")
	}
	totals := totalsFromModel(model.Totals)
	if err := validateAnalyticsRollupTotals(totals); err != nil {
		return ProjectUsageDaily{}, err
	}
	return ProjectUsageDaily{
		GenerationID: model.GenerationID, BucketStartMS: model.BucketStartMS,
		ReportingTimezone: model.ReportingTimezone, DimensionKey: model.DimensionKey,
		ProjectID:             cloneStringPointerStore(model.ProjectID),
		ProjectDisplayName:    cloneStringPointerStore(model.ProjectDisplayName),
		AttributionConfidence: model.AttributionConfidence,
		AttributionSource:     model.AttributionSource, AttributionReason: model.AttributionReason,
		RollupTotals: totals,
	}, nil
}

func applyProjectAnalyticsCursor(query *gorm.DB, filter ProjectAnalyticsFilter) *gorm.DB {
	if filter.Cursor == nil {
		return query
	}
	expression := projectAnalyticsSortExpression(filter.SortField)
	cursor := filter.Cursor
	if cursor.Null {
		return query.Where(expression+" IS NULL AND project_group.dimension_key < ?", cursor.DimensionKey)
	}
	comparison := ">"
	if filter.SortDirection == AnalyticsSortDescending {
		comparison = "<"
	}
	value := any(cursor.NumericValue)
	if filter.SortField == ProjectAnalyticsSortDisplayName {
		value = cursor.TextValue
	}
	return query.Where(
		"(("+expression+" IS NOT NULL AND ("+expression+" "+comparison+" ? OR ("+
			expression+" = ? AND project_group.dimension_key < ?))) OR "+expression+" IS NULL)",
		dereferenceProjectCursorValue(value), dereferenceProjectCursorValue(value), cursor.DimensionKey,
	)
}

func orderProjectAnalytics(query *gorm.DB, filter ProjectAnalyticsFilter) *gorm.DB {
	expression := projectAnalyticsSortExpression(filter.SortField)
	query = query.Order(expression + " IS NULL")
	if filter.SortDirection == AnalyticsSortAscending {
		query = query.Order(expression + " ASC")
	} else {
		query = query.Order(expression + " DESC")
	}
	return query.Order("project_group.dimension_key DESC")
}

func projectAnalyticsSortExpression(field ProjectAnalyticsSortField) string {
	switch field {
	case ProjectAnalyticsSortTotalTokens:
		return "project_group.total_tokens"
	case ProjectAnalyticsSortEstimatedCost:
		return "project_group.estimated_usd_micros"
	case ProjectAnalyticsSortDisplayName:
		return "project_group.project_display_name"
	default:
		return "project_group.last_activity_at_ms"
	}
}

func projectCursorForRecord(
	record ProjectAnalyticsRecord,
	field ProjectAnalyticsSortField,
) *ProjectAnalyticsCursor {
	cursor := &ProjectAnalyticsCursor{DimensionKey: record.DimensionKey}
	switch field {
	case ProjectAnalyticsSortTotalTokens:
		cursor.NumericValue = cloneInt64Pointer(record.Totals.TotalTokens)
	case ProjectAnalyticsSortEstimatedCost:
		cursor.NumericValue = cloneInt64Pointer(record.Totals.EstimatedUSDMicros)
	case ProjectAnalyticsSortDisplayName:
		cursor.TextValue = cloneStringPointerStore(record.ProjectDisplayName)
	default:
		cursor.NumericValue = cloneInt64Pointer(&record.Totals.LastActivityAtMS)
	}
	cursor.Null = cursor.NumericValue == nil && cursor.TextValue == nil
	return cursor
}

func aggregateProjectPageTotals(records []ProjectAnalyticsRecord) (RollupTotals, error) {
	accumulator := newAnalyticsRollupAccumulator()
	for _, record := range records {
		if err := accumulator.add(record.Totals); err != nil {
			return RollupTotals{}, err
		}
	}
	return accumulator.totals()
}

func aggregateProjectDailyTotals(rows []ProjectUsageDaily) (RollupTotals, error) {
	accumulator := newAnalyticsRollupAccumulator()
	for _, row := range rows {
		if err := accumulator.add(row.RollupTotals); err != nil {
			return RollupTotals{}, err
		}
	}
	return accumulator.totals()
}

func loadAnalyticsPricingVersions(
	database *gorm.DB,
	filter AnalyticsRange,
	generationID string,
) ([]string, error) {
	versions := make([]string, 0)
	err := database.Table("turn_costs AS cost").
		Joins("JOIN turn_usage AS usage ON usage.turn_id = cost.turn_id").
		Where(
			"cost.generation_id = ? AND usage.is_final = ? AND usage.observed_at_ms >= ? AND usage.observed_at_ms < ? AND cost.pricing_version IS NOT NULL",
			generationID, true, filter.StartAtMS, filter.EndAtMS,
		).
		Distinct("cost.pricing_version").Order("cost.pricing_version").
		Pluck("cost.pricing_version", &versions).Error
	return versions, err
}

func equalAnalyticsTotals(left, right RollupTotals) bool {
	return left.TurnCount == right.TurnCount &&
		equalInt64Pointer(left.InputTokens, right.InputTokens) &&
		equalInt64Pointer(left.CachedInputTokens, right.CachedInputTokens) &&
		equalInt64Pointer(left.OutputTokens, right.OutputTokens) &&
		equalInt64Pointer(left.ReasoningTokens, right.ReasoningTokens) &&
		equalInt64Pointer(left.TotalTokens, right.TotalTokens) &&
		equalInt64Pointer(left.EstimatedUSDMicros, right.EstimatedUSDMicros) &&
		left.PricedTurnCount == right.PricedTurnCount &&
		left.UnpricedTurnCount == right.UnpricedTurnCount &&
		left.FirstActivityAtMS == right.FirstActivityAtMS &&
		left.LastActivityAtMS == right.LastActivityAtMS &&
		left.UpdatedAtMS == right.UpdatedAtMS
}

func projectConfidenceFromRank(rank int) string {
	switch rank {
	case 0:
		return string(AttributionConfidenceUnknown)
	case 1:
		return string(AttributionConfidenceLow)
	case 2:
		return string(AttributionConfidenceMedium)
	case 3:
		return string(AttributionConfidenceHigh)
	default:
		return ""
	}
}

func projectConfidenceRank(value string) int {
	switch value {
	case string(AttributionConfidenceUnknown):
		return 0
	case string(AttributionConfidenceLow):
		return 1
	case string(AttributionConfidenceMedium):
		return 2
	case string(AttributionConfidenceHigh):
		return 3
	default:
		return -1
	}
}

func mergedProjectAttributionValue(minimum, maximum string) string {
	if minimum == "" || maximum == "" {
		return ""
	}
	if minimum == maximum {
		return minimum
	}
	return costDimensionMixed
}

func validProjectConfidence(value string) bool {
	return value == string(AttributionConfidenceHigh) ||
		value == string(AttributionConfidenceMedium) ||
		value == string(AttributionConfidenceLow) ||
		value == string(AttributionConfidenceUnknown)
}

func validProjectAttributionSource(value string) bool {
	return value == string(AttributionSourceSessionIDFallback) ||
		value == string(AttributionSourceRegisteredRoot) ||
		value == string(AttributionSourceCWDPathDigest) ||
		value == string(AttributionSourceModelCanonical) ||
		value == string(AttributionSourceModelAlias) ||
		value == string(AttributionSourceConflict) ||
		value == string(AttributionSourceMissing) ||
		value == string(AttributionSourceInvalidPath) ||
		value == string(AttributionSourceInvalidModel) || value == costDimensionMixed
}

func validProjectAttributionReason(value string) bool {
	return value == string(AttributionReasonStableIdentity) ||
		value == string(AttributionReasonRootMatched) ||
		value == string(AttributionReasonPathDerived) ||
		value == string(AttributionReasonObserved) ||
		value == string(AttributionReasonConflict) ||
		value == string(AttributionReasonMissing) ||
		value == string(AttributionReasonInvalid) || value == costDimensionMixed
}

func hasDuplicateStoreStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func dereferenceProjectCursorValue(value any) any {
	switch typed := value.(type) {
	case *int64:
		return *typed
	case *string:
		return *typed
	default:
		return nil
	}
}

func cloneStringPointerStore(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
