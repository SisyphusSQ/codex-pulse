package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const sessionAnalyticsProjectionSelect = `
	session.session_id AS session_id,
	attribution.display_title AS display_title,
	attribution.title_confidence AS title_confidence,
	attribution.title_source AS title_source,
	attribution.title_reason AS title_reason,
	attribution.project_id AS project_id,
	attribution.project_display_name AS project_display_name,
	attribution.project_confidence AS project_confidence,
	attribution.project_source AS project_source,
	attribution.project_reason AS project_reason,
	attribution.model_key AS model_key,
	attribution.model_display_name AS model_display_name,
	attribution.model_confidence AS model_confidence,
	attribution.model_source AS model_source,
	attribution.model_reason AS model_reason,
	attribution.rule_version AS rule_version,
	attribution.updated_at_ms AS attribution_updated_at_ms,
	current.last_activity_at_ms AS last_activity_at_ms,
	CASE WHEN EXISTS (
		SELECT 1 FROM turns AS active_turn
		WHERE active_turn.session_id = session.session_id
			AND active_turn.completed_at_ms IS NULL
	) THEN 1 ELSE 0 END AS active,
	rollup.session_id AS rollup_session_id,
	rollup.turn_count AS turn_count,
	rollup.input_tokens AS input_tokens,
	rollup.cached_input_tokens AS cached_input_tokens,
	rollup.output_tokens AS output_tokens,
	rollup.reasoning_tokens AS reasoning_tokens,
	rollup.total_tokens AS total_tokens,
	rollup.estimated_usd_micros AS estimated_usd_micros,
	rollup.priced_turn_count AS priced_turn_count,
	rollup.unpriced_turn_count AS unpriced_turn_count,
	rollup.first_activity_at_ms AS first_activity_at_ms,
	rollup.last_activity_at_ms AS rollup_last_activity_at_ms,
	rollup.updated_at_ms AS rollup_updated_at_ms`

const sessionAnalyticsTotalsSelect = `
	COUNT(rollup.session_id) AS rollup_rows,
	COALESCE(SUM(rollup.turn_count), 0) AS turn_count,
	SUM(rollup.input_tokens) AS input_tokens,
	SUM(CASE WHEN rollup.session_id IS NOT NULL AND rollup.input_tokens IS NULL THEN 1 ELSE 0 END) AS input_nulls,
	SUM(rollup.cached_input_tokens) AS cached_input_tokens,
	SUM(CASE WHEN rollup.session_id IS NOT NULL AND rollup.cached_input_tokens IS NULL THEN 1 ELSE 0 END) AS cached_nulls,
	SUM(rollup.output_tokens) AS output_tokens,
	SUM(CASE WHEN rollup.session_id IS NOT NULL AND rollup.output_tokens IS NULL THEN 1 ELSE 0 END) AS output_nulls,
	SUM(rollup.reasoning_tokens) AS reasoning_tokens,
	SUM(CASE WHEN rollup.session_id IS NOT NULL AND rollup.reasoning_tokens IS NULL THEN 1 ELSE 0 END) AS reasoning_nulls,
	SUM(rollup.total_tokens) AS total_tokens,
	SUM(CASE WHEN rollup.session_id IS NOT NULL AND rollup.total_tokens IS NULL THEN 1 ELSE 0 END) AS total_nulls,
	SUM(rollup.estimated_usd_micros) AS estimated_usd_micros,
	COALESCE(SUM(rollup.priced_turn_count), 0) AS priced_turn_count,
	COALESCE(SUM(rollup.unpriced_turn_count), 0) AS unpriced_turn_count,
	MIN(rollup.first_activity_at_ms) AS first_activity_at_ms,
	MAX(rollup.last_activity_at_ms) AS last_activity_at_ms,
	COALESCE(MAX(rollup.updated_at_ms), 0) AS updated_at_ms`

const sessionTurnAnalyticsProjectionSelect = `
	turn_record.turn_id AS turn_id,
	turn_record.started_at_ms AS started_at_ms,
	turn_record.completed_at_ms AS completed_at_ms,
	attribution.turn_id AS attribution_turn_id,
	attribution.model_key AS model_key,
	attribution.model_display_name AS model_display_name,
	attribution.model_confidence AS model_confidence,
	attribution.model_source AS model_source,
	attribution.model_reason AS model_reason,
	attribution.rule_version AS rule_version,
	attribution.updated_at_ms AS attribution_updated_at_ms,
	usage.turn_id AS usage_turn_id,
	usage.observed_at_ms AS usage_observed_at_ms,
	usage.is_final AS usage_is_final,
	usage.input_tokens AS input_tokens,
	usage.cached_input_tokens AS cached_input_tokens,
	usage.output_tokens AS output_tokens,
	usage.reasoning_tokens AS reasoning_tokens,
	cost.turn_id AS cost_turn_id,
	cost.pricing_version AS pricing_version,
	cost.estimated_usd_micros AS estimated_usd_micros,
	cost.pricing_status AS pricing_status,
	cost.pricing_reason AS pricing_reason`

type sessionAnalyticsProjection struct {
	SessionID              string  `gorm:"column:session_id"`
	DisplayTitle           string  `gorm:"column:display_title"`
	TitleConfidence        string  `gorm:"column:title_confidence"`
	TitleSource            string  `gorm:"column:title_source"`
	TitleReason            string  `gorm:"column:title_reason"`
	ProjectID              *string `gorm:"column:project_id"`
	ProjectDisplay         *string `gorm:"column:project_display_name"`
	ProjectConfidence      string  `gorm:"column:project_confidence"`
	ProjectSource          string  `gorm:"column:project_source"`
	ProjectReason          string  `gorm:"column:project_reason"`
	ModelKey               *string `gorm:"column:model_key"`
	ModelDisplay           *string `gorm:"column:model_display_name"`
	ModelConfidence        string  `gorm:"column:model_confidence"`
	ModelSource            string  `gorm:"column:model_source"`
	ModelReason            string  `gorm:"column:model_reason"`
	RuleVersion            int     `gorm:"column:rule_version"`
	AttributionUpdatedAtMS int64   `gorm:"column:attribution_updated_at_ms"`
	LastActivityAtMS       *int64  `gorm:"column:last_activity_at_ms"`
	Active                 int64   `gorm:"column:active"`
	RollupSessionID        *string `gorm:"column:rollup_session_id"`
	TurnCount              *int64  `gorm:"column:turn_count"`
	InputTokens            *int64  `gorm:"column:input_tokens"`
	CachedInputTokens      *int64  `gorm:"column:cached_input_tokens"`
	OutputTokens           *int64  `gorm:"column:output_tokens"`
	ReasoningTokens        *int64  `gorm:"column:reasoning_tokens"`
	TotalTokens            *int64  `gorm:"column:total_tokens"`
	EstimatedUSDMicros     *int64  `gorm:"column:estimated_usd_micros"`
	PricedTurnCount        *int64  `gorm:"column:priced_turn_count"`
	UnpricedTurnCount      *int64  `gorm:"column:unpriced_turn_count"`
	FirstActivityAtMS      *int64  `gorm:"column:first_activity_at_ms"`
	RollupLastActivityAtMS *int64  `gorm:"column:rollup_last_activity_at_ms"`
	RollupUpdatedAtMS      *int64  `gorm:"column:rollup_updated_at_ms"`
}

type sessionAnalyticsTotalsProjection struct {
	RollupRows         int64  `gorm:"column:rollup_rows"`
	TurnCount          int64  `gorm:"column:turn_count"`
	InputTokens        *int64 `gorm:"column:input_tokens"`
	InputNulls         int64  `gorm:"column:input_nulls"`
	CachedInputTokens  *int64 `gorm:"column:cached_input_tokens"`
	CachedNulls        int64  `gorm:"column:cached_nulls"`
	OutputTokens       *int64 `gorm:"column:output_tokens"`
	OutputNulls        int64  `gorm:"column:output_nulls"`
	ReasoningTokens    *int64 `gorm:"column:reasoning_tokens"`
	ReasoningNulls     int64  `gorm:"column:reasoning_nulls"`
	TotalTokens        *int64 `gorm:"column:total_tokens"`
	TotalNulls         int64  `gorm:"column:total_nulls"`
	EstimatedUSDMicros *int64 `gorm:"column:estimated_usd_micros"`
	PricedTurnCount    int64  `gorm:"column:priced_turn_count"`
	UnpricedTurnCount  int64  `gorm:"column:unpriced_turn_count"`
	FirstActivityAtMS  *int64 `gorm:"column:first_activity_at_ms"`
	LastActivityAtMS   *int64 `gorm:"column:last_activity_at_ms"`
	UpdatedAtMS        int64  `gorm:"column:updated_at_ms"`
}

type sessionTurnAnalyticsProjection struct {
	TurnID                 string  `gorm:"column:turn_id"`
	StartedAtMS            int64   `gorm:"column:started_at_ms"`
	CompletedAtMS          *int64  `gorm:"column:completed_at_ms"`
	AttributionTurnID      *string `gorm:"column:attribution_turn_id"`
	ModelKey               *string `gorm:"column:model_key"`
	ModelDisplay           *string `gorm:"column:model_display_name"`
	ModelConfidence        *string `gorm:"column:model_confidence"`
	ModelSource            *string `gorm:"column:model_source"`
	ModelReason            *string `gorm:"column:model_reason"`
	RuleVersion            *int    `gorm:"column:rule_version"`
	AttributionUpdatedAtMS *int64  `gorm:"column:attribution_updated_at_ms"`
	UsageTurnID            *string `gorm:"column:usage_turn_id"`
	UsageObservedAtMS      *int64  `gorm:"column:usage_observed_at_ms"`
	UsageIsFinal           *bool   `gorm:"column:usage_is_final"`
	InputTokens            *int64  `gorm:"column:input_tokens"`
	CachedInputTokens      *int64  `gorm:"column:cached_input_tokens"`
	OutputTokens           *int64  `gorm:"column:output_tokens"`
	ReasoningTokens        *int64  `gorm:"column:reasoning_tokens"`
	CostTurnID             *string `gorm:"column:cost_turn_id"`
	PricingVersion         *string `gorm:"column:pricing_version"`
	EstimatedUSDMicros     *int64  `gorm:"column:estimated_usd_micros"`
	PricingStatus          *string `gorm:"column:pricing_status"`
	PricingReason          *string `gorm:"column:pricing_reason"`
}

func (repository *Repository) ListSessionAnalytics(
	ctx context.Context,
	filter SessionAnalyticsFilter,
) (SessionAnalyticsPage, error) {
	if repository == nil || repository.database == nil {
		return SessionAnalyticsPage{}, ErrInvalidRepository
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SessionAnalyticsPage{}, err
	}
	if err := validateSessionAnalyticsFilter(filter); err != nil {
		return SessionAnalyticsPage{}, err
	}
	page := SessionAnalyticsPage{Records: make([]SessionAnalyticsRecord, 0)}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		if err := ensureSessionAttributionsPresent(database); err != nil {
			return err
		}
		generation, mode, err := loadSessionAnalyticsGeneration(database, filter.ReportingTimezone)
		if err != nil {
			return err
		}
		page.Mode = mode
		if generation == nil {
		} else {
			record := generationFromModel(*generation)
			page.Generation = &record
		}

		matched := buildSessionAnalyticsQuery(database, filter, generation)
		if err := matched.Count(&page.MatchedCount).Error; err != nil {
			return err
		}
		if generation != nil {
			var totals sessionAnalyticsTotalsProjection
			if err := buildSessionAnalyticsQuery(database, filter, generation).
				Select(sessionAnalyticsTotalsSelect).Scan(&totals).Error; err != nil {
				return err
			}
			mapped, err := sessionTotalsFromProjection(totals)
			if err != nil {
				return err
			}
			page.MatchedTotals = &mapped
		}

		query := buildSessionAnalyticsQuery(database, filter, generation).
			Select(sessionAnalyticsProjectionSelect)
		query = applySessionAnalyticsCursor(query, filter)
		query = orderSessionAnalytics(query, filter).Limit(filter.Limit + 1)
		var projections []sessionAnalyticsProjection
		if err := query.Scan(&projections).Error; err != nil {
			return err
		}
		hasMore := len(projections) > filter.Limit
		if hasMore {
			projections = projections[:filter.Limit]
		}
		for _, projection := range projections {
			record, err := sessionRecordFromProjection(projection)
			if err != nil {
				return err
			}
			page.Records = append(page.Records, record)
		}
		if generation != nil {
			totals, err := aggregateSessionPageTotals(page.Records)
			if err != nil {
				return err
			}
			page.PageTotals = &totals
		}
		if hasMore && len(page.Records) > 0 {
			page.NextCursor = sessionCursorForRecord(page.Records[len(page.Records)-1], filter.SortField)
		}
		return nil
	})
	return page, err
}

func (repository *Repository) SessionAnalytics(
	ctx context.Context,
	filter SessionAnalyticsDetailFilter,
) (SessionAnalyticsSnapshot, error) {
	if repository == nil || repository.database == nil {
		return SessionAnalyticsSnapshot{}, ErrInvalidRepository
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SessionAnalyticsSnapshot{}, err
	}
	if err := validateSessionAnalyticsDetailFilter(filter); err != nil {
		return SessionAnalyticsSnapshot{}, err
	}
	result := SessionAnalyticsSnapshot{
		PricingVersions: make([]string, 0), UnpricedReasons: make([]CostReasonCount, 0),
	}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		if err := ensureSessionAttributionsPresent(database); err != nil {
			return err
		}
		generation, mode, err := loadSessionAnalyticsGeneration(database, filter.ReportingTimezone)
		if err != nil {
			return err
		}
		result.Mode = mode
		if generation == nil {
		} else {
			record := generationFromModel(*generation)
			result.Generation = &record
		}
		queryFilter := SessionAnalyticsFilter{
			Limit: 1, SortField: SessionAnalyticsSortLastActivity,
			SortDirection: AnalyticsSortDescending,
		}
		var projection sessionAnalyticsProjection
		query := buildSessionAnalyticsQuery(database, queryFilter, generation).
			Where("session.session_id = ?", filter.SessionID).
			Select(sessionAnalyticsProjectionSelect).Limit(1).Scan(&projection)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected != 1 {
			return ErrNotFound
		}
		result.Record, err = sessionRecordFromProjection(projection)
		if err != nil {
			return err
		}
		result.Turns, result.NextTurnCursor, err = loadSessionTurnAnalytics(
			database, filter, generation,
		)
		if err != nil {
			return err
		}
		if generation == nil {
			return nil
		}
		if err := database.Table("turn_costs AS cost").
			Joins("JOIN turns AS turn_record ON turn_record.turn_id = cost.turn_id").
			Where("cost.generation_id = ? AND turn_record.session_id = ? AND cost.pricing_version IS NOT NULL",
				generation.GenerationID, filter.SessionID).
			Distinct("cost.pricing_version").Order("cost.pricing_version").
			Pluck("cost.pricing_version", &result.PricingVersions).Error; err != nil {
			return err
		}
		var reasons []analyticsCostReasonCountModel
		if err := database.Table("turn_costs AS cost").
			Select("cost.pricing_reason AS reason, COUNT(*) AS count").
			Joins("JOIN turns AS turn_record ON turn_record.turn_id = cost.turn_id").
			Where("cost.generation_id = ? AND turn_record.session_id = ? AND cost.pricing_status = ?",
				generation.GenerationID, filter.SessionID, pricing.CostStatusUnpriced).
			Group("cost.pricing_reason").Order("cost.pricing_reason").Scan(&reasons).Error; err != nil {
			return err
		}
		for _, reason := range reasons {
			if reason.Count <= 0 || !validStoredCostReason(pricing.CostReason(reason.Reason)) {
				return invalidRecord("stored session unpriced reason summary is invalid")
			}
			result.UnpricedReasons = append(result.UnpricedReasons, CostReasonCount{
				Reason: pricing.CostReason(reason.Reason), Count: reason.Count,
			})
		}
		return nil
	})
	return result, err
}

// loadSessionTurnAnalytics 在Session detail所属的同一Store.View中读取bounded turn page。
func loadSessionTurnAnalytics(
	database *gorm.DB,
	filter SessionAnalyticsDetailFilter,
	generation *costRollupGenerationModel,
) ([]SessionTurnAnalyticsRecord, *SessionTurnAnalyticsCursor, error) {
	query := database.Table("turns AS turn_record").
		Joins("LEFT JOIN turn_attributions AS attribution ON attribution.turn_id = turn_record.turn_id").
		Joins(`LEFT JOIN turn_usage AS usage ON usage.turn_id = turn_record.turn_id
			AND usage.source_generation = turn_record.source_generation`).
		Where("turn_record.session_id = ?", filter.SessionID)
	if generation == nil {
		query = query.Joins("LEFT JOIN turn_costs AS cost ON 1 = 0")
	} else {
		query = query.Joins(
			`LEFT JOIN turn_costs AS cost ON cost.turn_id = turn_record.turn_id
				AND cost.generation_id = ?`,
			generation.GenerationID,
		)
	}
	if filter.TurnCursor != nil {
		query = query.Where(
			`turn_record.started_at_ms < ? OR
				(turn_record.started_at_ms = ? AND turn_record.turn_id < ?)`,
			filter.TurnCursor.StartedAtMS,
			filter.TurnCursor.StartedAtMS,
			filter.TurnCursor.TurnID,
		)
	}
	var projections []sessionTurnAnalyticsProjection
	if err := query.Select(sessionTurnAnalyticsProjectionSelect).
		Order("turn_record.started_at_ms DESC").
		Order("turn_record.turn_id DESC").
		Limit(filter.TurnLimit + 1).
		Scan(&projections).Error; err != nil {
		return nil, nil, err
	}
	hasMore := len(projections) > filter.TurnLimit
	if hasMore {
		projections = projections[:filter.TurnLimit]
	}
	records := make([]SessionTurnAnalyticsRecord, 0, len(projections))
	for _, projection := range projections {
		record, err := sessionTurnRecordFromProjection(projection)
		if err != nil {
			return nil, nil, err
		}
		records = append(records, record)
	}
	if !hasMore {
		return records, nil, nil
	}
	last := records[len(records)-1]
	return records, &SessionTurnAnalyticsCursor{
		SessionID: filter.SessionID, TurnID: last.TurnID, StartedAtMS: last.StartedAtMS,
	}, nil
}

func sessionTurnRecordFromProjection(
	projection sessionTurnAnalyticsProjection,
) (SessionTurnAnalyticsRecord, error) {
	if projection.TurnID == "" || projection.StartedAtMS < 0 ||
		(projection.CompletedAtMS != nil && *projection.CompletedAtMS < projection.StartedAtMS) ||
		projection.AttributionTurnID == nil || *projection.AttributionTurnID != projection.TurnID ||
		projection.ModelConfidence == nil || projection.ModelSource == nil ||
		projection.ModelReason == nil || projection.RuleVersion == nil ||
		projection.AttributionUpdatedAtMS == nil {
		return SessionTurnAnalyticsRecord{}, invalidRecord("stored session turn attribution is invalid")
	}
	if (projection.ModelKey == nil) != (projection.ModelDisplay == nil) {
		return SessionTurnAnalyticsRecord{}, invalidRecord("stored session turn model attribution is invalid")
	}
	record := SessionTurnAnalyticsRecord{
		TurnID: projection.TurnID, StartedAtMS: projection.StartedAtMS,
		CompletedAtMS: cloneInt64Pointer(projection.CompletedAtMS),
		Model: ModelAttribution{
			ModelKey:    cloneAttributionString(projection.ModelKey),
			DisplayName: cloneAttributionString(projection.ModelDisplay),
			Confidence:  AttributionConfidence(*projection.ModelConfidence),
			Source:      AttributionSource(*projection.ModelSource),
			Reason:      AttributionReason(*projection.ModelReason),
		},
	}
	if projection.UsageTurnID != nil {
		if *projection.UsageTurnID != projection.TurnID || projection.UsageObservedAtMS == nil ||
			projection.UsageIsFinal == nil || *projection.UsageObservedAtMS < 0 ||
			*projection.UsageObservedAtMS < projection.StartedAtMS ||
			(*projection.UsageIsFinal != (projection.CompletedAtMS != nil)) ||
			invalidOptionalAnalyticsNumber(projection.InputTokens) ||
			invalidOptionalAnalyticsNumber(projection.CachedInputTokens) ||
			invalidOptionalAnalyticsNumber(projection.OutputTokens) ||
			invalidOptionalAnalyticsNumber(projection.ReasoningTokens) {
			return SessionTurnAnalyticsRecord{}, invalidRecord("stored session turn usage is invalid")
		}
		record.Usage = &SessionTurnUsageAnalytics{
			ObservedAtMS: *projection.UsageObservedAtMS, IsFinal: *projection.UsageIsFinal,
			InputTokens:       cloneInt64Pointer(projection.InputTokens),
			CachedInputTokens: cloneInt64Pointer(projection.CachedInputTokens),
			OutputTokens:      cloneInt64Pointer(projection.OutputTokens),
			ReasoningTokens:   cloneInt64Pointer(projection.ReasoningTokens),
		}
	} else if projection.UsageObservedAtMS != nil || projection.UsageIsFinal != nil ||
		projection.InputTokens != nil || projection.CachedInputTokens != nil ||
		projection.OutputTokens != nil || projection.ReasoningTokens != nil {
		return SessionTurnAnalyticsRecord{}, invalidRecord("stored absent session turn usage is invalid")
	}
	if projection.CostTurnID == nil {
		if projection.PricingVersion != nil || projection.EstimatedUSDMicros != nil ||
			projection.PricingStatus != nil || projection.PricingReason != nil {
			return SessionTurnAnalyticsRecord{}, invalidRecord("stored absent session turn cost is invalid")
		}
		return record, nil
	}
	if *projection.CostTurnID != projection.TurnID || projection.PricingStatus == nil ||
		projection.PricingReason == nil {
		return SessionTurnAnalyticsRecord{}, invalidRecord("stored session turn cost is invalid")
	}
	status := pricing.CostStatus(*projection.PricingStatus)
	reason := pricing.CostReason(*projection.PricingReason)
	if (status == pricing.CostStatusPriced &&
		(reason != pricing.CostReasonPriced || projection.PricingVersion == nil ||
			*projection.PricingVersion == "" ||
			projection.EstimatedUSDMicros == nil)) ||
		(status == pricing.CostStatusUnpriced &&
			(!validStoredCostReason(reason) || projection.PricingVersion != nil ||
				projection.EstimatedUSDMicros != nil)) ||
		(status != pricing.CostStatusPriced && status != pricing.CostStatusUnpriced) ||
		invalidOptionalAnalyticsNumber(projection.EstimatedUSDMicros) {
		return SessionTurnAnalyticsRecord{}, invalidRecord("stored session turn pricing evidence is invalid")
	}
	record.Cost = &SessionTurnCostAnalytics{
		PricingVersion:     cloneAttributionString(projection.PricingVersion),
		EstimatedUSDMicros: cloneInt64Pointer(projection.EstimatedUSDMicros),
		Status:             status, Reason: reason,
	}
	return record, nil
}

func invalidOptionalAnalyticsNumber(value *int64) bool {
	return value != nil && *value < 0
}

func ensureSessionAttributionsPresent(database *gorm.DB) error {
	var missing int64
	if err := database.Table("sessions AS session").
		Joins("LEFT JOIN session_attributions AS attribution ON attribution.session_id = session.session_id").
		Where("attribution.session_id IS NULL").Count(&missing).Error; err != nil {
		return err
	}
	if missing != 0 {
		return invalidRecord("session analytics attribution is incomplete")
	}
	return nil
}

func loadSessionAnalyticsGeneration(
	database *gorm.DB,
	reportingTimezone *string,
) (*costRollupGenerationModel, AnalyticsReadMode, error) {
	query := database.Where("state = ?", CostRollupGenerationActive)
	if reportingTimezone != nil {
		query = query.Where("reporting_timezone = ?", *reportingTimezone)
		var generation costRollupGenerationModel
		if err := query.Take(&generation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, AnalyticsReadDetailFallback, nil
			}
			return nil, "", err
		}
		return &generation, AnalyticsReadActiveRollup, nil
	}
	var generations []costRollupGenerationModel
	if err := query.Order("generation_id").Limit(2).Find(&generations).Error; err != nil {
		return nil, "", err
	}
	if len(generations) == 0 {
		return nil, AnalyticsReadDetailFallback, nil
	}
	if len(generations) > 1 {
		return nil, AnalyticsReadAmbiguousFallback, nil
	}
	return &generations[0], AnalyticsReadActiveRollup, nil
}

func buildSessionAnalyticsQuery(
	database *gorm.DB,
	filter SessionAnalyticsFilter,
	generation *costRollupGenerationModel,
) *gorm.DB {
	query := database.Table("sessions AS session").
		Joins("JOIN session_attributions AS attribution ON attribution.session_id = session.session_id").
		Joins("LEFT JOIN session_current AS current ON current.session_id = session.session_id")
	if generation == nil {
		query = query.Joins("LEFT JOIN session_usage_rollups AS rollup ON 1 = 0")
	} else {
		query = query.Joins(
			"LEFT JOIN session_usage_rollups AS rollup ON rollup.session_id = session.session_id AND rollup.generation_id = ?",
			generation.GenerationID,
		)
	}
	if len(filter.ProjectIDs) > 0 {
		query = query.Where("attribution.project_id IN ?", filter.ProjectIDs)
	}
	if len(filter.ModelKeys) > 0 {
		query = query.Where("attribution.model_key IN ?", filter.ModelKeys)
	}
	if filter.Activity != nil {
		activeClause := `EXISTS (
			SELECT 1 FROM turns AS active_turn
			WHERE active_turn.session_id = session.session_id
				AND active_turn.completed_at_ms IS NULL
		)`
		if *filter.Activity == SessionActivityActive {
			query = query.Where(activeClause)
		} else {
			query = query.Where("NOT " + activeClause)
		}
	}
	if filter.LastActivityAtOrAfterMS != nil {
		query = query.Where("current.last_activity_at_ms >= ?", *filter.LastActivityAtOrAfterMS)
	}
	if filter.LastActivityBeforeMS != nil {
		query = query.Where("current.last_activity_at_ms < ?", *filter.LastActivityBeforeMS)
	}
	return query
}

func applySessionAnalyticsCursor(query *gorm.DB, filter SessionAnalyticsFilter) *gorm.DB {
	if filter.Cursor == nil {
		return query
	}
	expression := sessionAnalyticsSortExpression(filter.SortField)
	cursor := filter.Cursor
	if cursor.Null {
		return query.Where(expression+" IS NULL AND session.session_id < ?", cursor.SessionID)
	}
	comparison := ">"
	if filter.SortDirection == AnalyticsSortDescending {
		comparison = "<"
	}
	return query.Where(
		"(("+expression+" IS NOT NULL AND ("+expression+" "+comparison+" ? OR ("+
			expression+" = ? AND session.session_id < ?))) OR "+expression+" IS NULL)",
		*cursor.Value, *cursor.Value, cursor.SessionID,
	)
}

func orderSessionAnalytics(query *gorm.DB, filter SessionAnalyticsFilter) *gorm.DB {
	expression := sessionAnalyticsSortExpression(filter.SortField)
	query = query.Order(expression + " IS NULL")
	if filter.SortDirection == AnalyticsSortAscending {
		query = query.Order(expression + " ASC")
	} else {
		query = query.Order(expression + " DESC")
	}
	return query.Order("session.session_id DESC")
}

func sessionAnalyticsSortExpression(field SessionAnalyticsSortField) string {
	switch field {
	case SessionAnalyticsSortTotalTokens:
		return "rollup.total_tokens"
	case SessionAnalyticsSortEstimatedCost:
		return "rollup.estimated_usd_micros"
	default:
		return "current.last_activity_at_ms"
	}
}

func sessionRecordFromProjection(projection sessionAnalyticsProjection) (SessionAnalyticsRecord, error) {
	if projection.SessionID == "" || projection.DisplayTitle == "" ||
		(projection.Active != 0 && projection.Active != 1) {
		return SessionAnalyticsRecord{}, invalidRecord("stored session analytics identity is invalid")
	}
	attribution := sessionAttributionFromModel(sessionAttributionModel{
		SessionID: projection.SessionID, DisplayTitle: projection.DisplayTitle,
		TitleConfidence: projection.TitleConfidence, TitleSource: projection.TitleSource,
		TitleReason: projection.TitleReason, ProjectID: projection.ProjectID,
		ProjectDisplay: projection.ProjectDisplay, ProjectConfidence: projection.ProjectConfidence,
		ProjectSource: projection.ProjectSource, ProjectReason: projection.ProjectReason,
		ModelKey: projection.ModelKey, ModelDisplay: projection.ModelDisplay,
		ModelConfidence: projection.ModelConfidence, ModelSource: projection.ModelSource,
		ModelReason: projection.ModelReason, RuleVersion: projection.RuleVersion,
		UpdatedAtMS: projection.AttributionUpdatedAtMS,
	})
	activity := SessionActivityIdle
	if projection.Active == 1 {
		activity = SessionActivityActive
	}
	record := SessionAnalyticsRecord{
		SessionID: attribution.SessionID, DisplayTitle: attribution.DisplayTitle,
		TitleConfidence: attribution.TitleConfidence, TitleSource: attribution.TitleSource,
		TitleReason: attribution.TitleReason, Project: attribution.Project, Model: attribution.Model,
		Activity: activity, LastActivityAtMS: cloneInt64Pointer(projection.LastActivityAtMS),
	}
	if projection.RollupSessionID != nil {
		if *projection.RollupSessionID != projection.SessionID || projection.TurnCount == nil ||
			projection.PricedTurnCount == nil || projection.UnpricedTurnCount == nil ||
			projection.FirstActivityAtMS == nil || projection.RollupLastActivityAtMS == nil ||
			projection.RollupUpdatedAtMS == nil {
			return SessionAnalyticsRecord{}, invalidRecord("stored session rollup shape is invalid")
		}
		rollup := RollupTotals{
			TurnCount:          *projection.TurnCount,
			InputTokens:        cloneInt64Pointer(projection.InputTokens),
			CachedInputTokens:  cloneInt64Pointer(projection.CachedInputTokens),
			OutputTokens:       cloneInt64Pointer(projection.OutputTokens),
			ReasoningTokens:    cloneInt64Pointer(projection.ReasoningTokens),
			TotalTokens:        cloneInt64Pointer(projection.TotalTokens),
			EstimatedUSDMicros: cloneInt64Pointer(projection.EstimatedUSDMicros),
			PricedTurnCount:    *projection.PricedTurnCount,
			UnpricedTurnCount:  *projection.UnpricedTurnCount,
			FirstActivityAtMS:  *projection.FirstActivityAtMS,
			LastActivityAtMS:   *projection.RollupLastActivityAtMS,
			UpdatedAtMS:        *projection.RollupUpdatedAtMS,
		}
		if err := validateAnalyticsRollupTotals(rollup); err != nil {
			return SessionAnalyticsRecord{}, err
		}
		record.Rollup = &rollup
	}
	return record, nil
}

func sessionTotalsFromProjection(value sessionAnalyticsTotalsProjection) (RollupTotals, error) {
	zero := int64(0)
	totals := RollupTotals{
		TurnCount:         value.TurnCount,
		InputTokens:       pointerFromAggregate(value.InputTokens, value.InputNulls, value.RollupRows, &zero),
		CachedInputTokens: pointerFromAggregate(value.CachedInputTokens, value.CachedNulls, value.RollupRows, &zero),
		OutputTokens:      pointerFromAggregate(value.OutputTokens, value.OutputNulls, value.RollupRows, &zero),
		ReasoningTokens:   pointerFromAggregate(value.ReasoningTokens, value.ReasoningNulls, value.RollupRows, &zero),
		TotalTokens:       pointerFromAggregate(value.TotalTokens, value.TotalNulls, value.RollupRows, &zero),
		PricedTurnCount:   value.PricedTurnCount, UnpricedTurnCount: value.UnpricedTurnCount,
		UpdatedAtMS: value.UpdatedAtMS,
	}
	if value.PricedTurnCount > 0 {
		totals.EstimatedUSDMicros = cloneInt64Pointer(value.EstimatedUSDMicros)
	}
	if value.RollupRows > 0 {
		if value.FirstActivityAtMS == nil || value.LastActivityAtMS == nil {
			return RollupTotals{}, invalidRecord("stored session totals activity range is invalid")
		}
		totals.FirstActivityAtMS = *value.FirstActivityAtMS
		totals.LastActivityAtMS = *value.LastActivityAtMS
	}
	if err := validateAnalyticsRollupTotals(totals); err != nil {
		return RollupTotals{}, err
	}
	return totals, nil
}

func pointerFromAggregate(value *int64, nulls, rows int64, zero *int64) *int64 {
	if nulls > 0 {
		return nil
	}
	if rows == 0 {
		return cloneInt64Pointer(zero)
	}
	return cloneInt64Pointer(value)
}

func aggregateSessionPageTotals(records []SessionAnalyticsRecord) (RollupTotals, error) {
	accumulator := newAnalyticsRollupAccumulator()
	for _, record := range records {
		if record.Rollup != nil {
			if err := accumulator.add(*record.Rollup); err != nil {
				return RollupTotals{}, err
			}
		}
	}
	return accumulator.totals()
}

type analyticsRollupAccumulator struct {
	rows                                    int64
	turnCount, pricedCount, unpricedCount   int64
	input, cached, output, reasoning, total nullableSum
	estimated                               int64
	firstActivityAtMS, lastActivityAtMS     int64
	updatedAtMS                             int64
}

func newAnalyticsRollupAccumulator() *analyticsRollupAccumulator {
	return &analyticsRollupAccumulator{
		input: newNullableSum(), cached: newNullableSum(), output: newNullableSum(),
		reasoning: newNullableSum(), total: newNullableSum(),
	}
}

func (accumulator *analyticsRollupAccumulator) add(value RollupTotals) error {
	if err := validateAnalyticsRollupTotals(value); err != nil {
		return err
	}
	var err error
	accumulator.turnCount, err = addCostInteger(accumulator.turnCount, value.TurnCount)
	if err != nil {
		return err
	}
	accumulator.pricedCount, err = addCostInteger(accumulator.pricedCount, value.PricedTurnCount)
	if err != nil {
		return err
	}
	accumulator.unpricedCount, err = addCostInteger(accumulator.unpricedCount, value.UnpricedTurnCount)
	if err != nil {
		return err
	}
	for _, component := range []struct {
		sum   *nullableSum
		value *int64
	}{
		{sum: &accumulator.input, value: value.InputTokens},
		{sum: &accumulator.cached, value: value.CachedInputTokens},
		{sum: &accumulator.output, value: value.OutputTokens},
		{sum: &accumulator.reasoning, value: value.ReasoningTokens},
		{sum: &accumulator.total, value: value.TotalTokens},
	} {
		if err := component.sum.add(component.value); err != nil {
			return err
		}
	}
	if value.EstimatedUSDMicros != nil {
		accumulator.estimated, err = addCostInteger(accumulator.estimated, *value.EstimatedUSDMicros)
		if err != nil {
			return err
		}
	}
	if accumulator.rows == 0 || value.FirstActivityAtMS < accumulator.firstActivityAtMS {
		accumulator.firstActivityAtMS = value.FirstActivityAtMS
	}
	if accumulator.rows == 0 || value.LastActivityAtMS > accumulator.lastActivityAtMS {
		accumulator.lastActivityAtMS = value.LastActivityAtMS
	}
	if value.UpdatedAtMS > accumulator.updatedAtMS {
		accumulator.updatedAtMS = value.UpdatedAtMS
	}
	accumulator.rows++
	return nil
}

func (accumulator *analyticsRollupAccumulator) totals() (RollupTotals, error) {
	if accumulator.rows == 0 {
		zero := int64(0)
		return RollupTotals{
			InputTokens: &zero, CachedInputTokens: cloneInt64Pointer(&zero),
			OutputTokens: cloneInt64Pointer(&zero), ReasoningTokens: cloneInt64Pointer(&zero),
			TotalTokens: cloneInt64Pointer(&zero),
		}, nil
	}
	totals := RollupTotals{
		TurnCount: accumulator.turnCount, InputTokens: accumulator.input.pointer(),
		CachedInputTokens: accumulator.cached.pointer(), OutputTokens: accumulator.output.pointer(),
		ReasoningTokens: accumulator.reasoning.pointer(), TotalTokens: accumulator.total.pointer(),
		PricedTurnCount: accumulator.pricedCount, UnpricedTurnCount: accumulator.unpricedCount,
		FirstActivityAtMS: accumulator.firstActivityAtMS,
		LastActivityAtMS:  accumulator.lastActivityAtMS, UpdatedAtMS: accumulator.updatedAtMS,
	}
	if accumulator.pricedCount > 0 {
		totals.EstimatedUSDMicros = cloneInt64Pointer(&accumulator.estimated)
	}
	return totals, validateAnalyticsRollupTotals(totals)
}

func validateAnalyticsRollupTotals(value RollupTotals) error {
	if value.TurnCount < 0 || value.PricedTurnCount < 0 || value.UnpricedTurnCount < 0 ||
		value.PricedTurnCount+value.UnpricedTurnCount != value.TurnCount {
		return invalidRecord("stored analytics rollup counts are invalid")
	}
	if (value.PricedTurnCount > 0 && value.EstimatedUSDMicros == nil) ||
		(value.PricedTurnCount == 0 && value.EstimatedUSDMicros != nil) {
		return invalidRecord("stored analytics rollup cost shape is invalid")
	}
	if value.EstimatedUSDMicros != nil && *value.EstimatedUSDMicros < 0 {
		return invalidRecord("stored analytics rollup cost is invalid")
	}
	components := []*int64{value.InputTokens, value.CachedInputTokens, value.OutputTokens, value.ReasoningTokens}
	complete := true
	total := int64(0)
	for _, component := range components {
		if component == nil {
			complete = false
			continue
		}
		if *component < 0 {
			return invalidRecord("stored analytics rollup token is invalid")
		}
		var err error
		total, err = addCostInteger(total, *component)
		if err != nil {
			return err
		}
	}
	if complete {
		if value.TotalTokens == nil || *value.TotalTokens != total {
			return invalidRecord("stored analytics rollup total is inconsistent")
		}
	} else if value.TotalTokens != nil {
		return invalidRecord("stored analytics rollup total shape is invalid")
	}
	if value.TurnCount == 0 {
		if value.FirstActivityAtMS != 0 || value.LastActivityAtMS != 0 {
			return invalidRecord("stored empty analytics rollup activity is invalid")
		}
	} else if value.FirstActivityAtMS < 0 || value.LastActivityAtMS < value.FirstActivityAtMS ||
		value.UpdatedAtMS < value.LastActivityAtMS {
		return invalidRecord("stored analytics rollup activity is invalid")
	}
	return nil
}

func sessionCursorForRecord(
	record SessionAnalyticsRecord,
	field SessionAnalyticsSortField,
) *SessionAnalyticsCursor {
	var value *int64
	switch field {
	case SessionAnalyticsSortTotalTokens:
		if record.Rollup != nil {
			value = record.Rollup.TotalTokens
		}
	case SessionAnalyticsSortEstimatedCost:
		if record.Rollup != nil {
			value = record.Rollup.EstimatedUSDMicros
		}
	default:
		value = record.LastActivityAtMS
	}
	return &SessionAnalyticsCursor{
		SessionID: record.SessionID, Null: value == nil, Value: cloneInt64Pointer(value),
	}
}

func validateSessionAnalyticsFilter(filter SessionAnalyticsFilter) error {
	if filter.Limit < 1 || filter.Limit > 100 ||
		(filter.SortField != SessionAnalyticsSortLastActivity &&
			filter.SortField != SessionAnalyticsSortTotalTokens &&
			filter.SortField != SessionAnalyticsSortEstimatedCost) ||
		(filter.SortDirection != AnalyticsSortAscending &&
			filter.SortDirection != AnalyticsSortDescending) {
		return invalidRecord("session analytics filter is invalid")
	}
	if err := validateAnalyticsTimezone(filter.ReportingTimezone); err != nil {
		return err
	}
	if err := validateAnalyticsDimensionValues(filter.ProjectIDs); err != nil {
		return err
	}
	if err := validateAnalyticsDimensionValues(filter.ModelKeys); err != nil {
		return err
	}
	if filter.Activity != nil && *filter.Activity != SessionActivityActive &&
		*filter.Activity != SessionActivityIdle {
		return invalidRecord("session activity filter is invalid")
	}
	if (filter.LastActivityAtOrAfterMS == nil) != (filter.LastActivityBeforeMS == nil) {
		return invalidRecord("session activity range is incomplete")
	}
	if filter.LastActivityAtOrAfterMS != nil && (*filter.LastActivityAtOrAfterMS < 0 ||
		*filter.LastActivityBeforeMS <= *filter.LastActivityAtOrAfterMS) {
		return invalidRecord("session activity range is invalid")
	}
	if filter.Cursor != nil {
		if filter.Cursor.SessionID == "" || len(filter.Cursor.SessionID) > 512 ||
			filter.Cursor.Null == (filter.Cursor.Value != nil) ||
			(filter.Cursor.Value != nil && *filter.Cursor.Value < 0) {
			return invalidRecord("session analytics cursor is invalid")
		}
	}
	return nil
}

func validateSessionAnalyticsDetailFilter(filter SessionAnalyticsDetailFilter) error {
	if filter.SessionID == "" || len(filter.SessionID) > 512 ||
		filter.TurnLimit < 1 || filter.TurnLimit > 50 {
		return invalidRecord("session analytics detail identity is invalid")
	}
	if filter.TurnCursor != nil &&
		(filter.TurnCursor.SessionID != filter.SessionID || filter.TurnCursor.TurnID == "" ||
			len(filter.TurnCursor.TurnID) > 512 || filter.TurnCursor.StartedAtMS < 0) {
		return invalidRecord("session analytics detail turn cursor is invalid")
	}
	return validateAnalyticsTimezone(filter.ReportingTimezone)
}

func validateAnalyticsTimezone(value *string) error {
	if value == nil {
		return nil
	}
	if *value == "" || *value == "Local" {
		return invalidRecord("analytics reporting timezone is invalid")
	}
	if _, err := time.LoadLocation(*value); err != nil {
		return invalidRecord("analytics reporting timezone is invalid")
	}
	return nil
}

func validateAnalyticsDimensionValues(values []string) error {
	if len(values) > 100 {
		return invalidRecord("analytics dimension filter is too large")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || len(value) > 512 {
			return invalidRecord("analytics dimension filter is invalid")
		}
		if _, found := seen[value]; found {
			return invalidRecord("analytics dimension filter is duplicated")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
