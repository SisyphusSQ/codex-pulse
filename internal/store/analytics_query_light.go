package store

import (
	"sort"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
	"github.com/SisyphusSQ/codex-pulse/internal/projectidentity"
)

type lightSessionAnalyticsProjection struct {
	SessionID         string  `gorm:"column:session_id"`
	ThreadName        *string `gorm:"column:thread_name"`
	CWD               string  `gorm:"column:cwd"`
	LastActivityAtMS  int64   `gorm:"column:last_activity_at_ms"`
	Generation        *int64  `gorm:"column:generation"`
	InputTokens       *int64  `gorm:"column:input_tokens"`
	CachedInputTokens *int64  `gorm:"column:cached_input_tokens"`
	OutputTokens      *int64  `gorm:"column:output_tokens"`
	ReasoningTokens   *int64  `gorm:"column:reasoning_tokens"`
	TotalTokens       *int64  `gorm:"column:total_tokens"`
}

type lightSessionPricingProjection struct {
	SessionID         string  `gorm:"column:session_id"`
	ObservedAtMS      int64   `gorm:"column:observed_at_ms"`
	ModelKey          *string `gorm:"column:model_key"`
	ModelSource       string  `gorm:"column:model_source"`
	InputTokens       int64   `gorm:"column:input_tokens"`
	CachedInputTokens int64   `gorm:"column:cached_input_tokens"`
	OutputTokens      int64   `gorm:"column:output_tokens"`
	ReasoningTokens   int64   `gorm:"column:reasoning_tokens"`
}

type lightSessionPricingGroupKey struct {
	sessionID, dimensionKey, pricingVersion string
}

type lightSessionPricingEvidence struct {
	source, currency string
	versions         []string
}

func listLightSessionAnalytics(
	database *gorm.DB,
	filter SessionAnalyticsFilter,
) (SessionAnalyticsPage, bool, error) {
	var count int64
	if err := database.Model(&lightSessionModel{}).Count(&count).Error; err != nil {
		return SessionAnalyticsPage{}, false, err
	}
	if count == 0 {
		return SessionAnalyticsPage{}, false, nil
	}
	page := SessionAnalyticsPage{
		Mode: AnalyticsReadLightIndex, Records: make([]SessionAnalyticsRecord, 0), MatchedCount: 0,
	}
	if len(filter.ModelKeys) > 0 ||
		(filter.Activity != nil && *filter.Activity == SessionActivityActive) {
		empty := lightTotalsForRecords(nil)
		page.MatchedTotals = &empty
		page.PageTotals = &empty
		return page, true, nil
	}
	var query *gorm.DB
	if filter.RangeExact {
		exactScan := database.Table("light_token_timed AS timed").
			Joins(`JOIN light_sessions AS source_session
				ON source_session.session_id = timed.session_id
				AND source_session.active_token_generation = timed.generation`).
			Where("timed.observed_at_ms >= ? AND timed.observed_at_ms < ?",
				*filter.LastActivityAtOrAfterMS, *filter.LastActivityBeforeMS).
			Select(`timed.session_id, MAX(timed.generation) AS generation,
				MAX(timed.observed_at_ms) AS last_activity_at_ms,
				SUM(timed.input_tokens) AS input_tokens,
				SUM(timed.cached_input_tokens) AS cached_input_tokens,
				SUM(timed.output_tokens) AS output_tokens,
				SUM(timed.reasoning_tokens) AS reasoning_tokens,
				SUM(timed.input_tokens + timed.output_tokens + timed.reasoning_tokens) AS total_tokens`).
			Group("timed.session_id")
		query = database.Table("light_sessions AS session").
			Joins("JOIN (?) AS scan ON scan.session_id = session.session_id", exactScan).
			Select(`session.session_id, session.thread_name, session.cwd,
				scan.last_activity_at_ms, scan.generation, scan.input_tokens,
				scan.cached_input_tokens, scan.output_tokens, scan.reasoning_tokens,
				scan.total_tokens`)
	} else {
		query = database.Table("light_sessions AS session").
			Joins(`LEFT JOIN light_token_scans AS scan
				ON scan.session_id = session.session_id
				AND scan.generation = session.active_token_generation`).
			Select(`session.session_id, session.thread_name, session.cwd,
				COALESCE(session.recency_at_ms, session.updated_at_ms) AS last_activity_at_ms,
				scan.generation, scan.input_tokens, scan.cached_input_tokens,
				scan.output_tokens, scan.reasoning_tokens,
				CASE WHEN scan.generation IS NULL THEN NULL
					ELSE scan.input_tokens + scan.output_tokens + scan.reasoning_tokens END AS total_tokens`)
	}
	if !filter.RangeExact && filter.LastActivityAtOrAfterMS != nil {
		query = query.Where("COALESCE(session.recency_at_ms, session.updated_at_ms) >= ?", *filter.LastActivityAtOrAfterMS)
	}
	if !filter.RangeExact && filter.LastActivityBeforeMS != nil {
		query = query.Where("COALESCE(session.recency_at_ms, session.updated_at_ms) < ?", *filter.LastActivityBeforeMS)
	}
	var projections []lightSessionAnalyticsProjection
	if err := query.Find(&projections).Error; err != nil {
		return SessionAnalyticsPage{}, false, err
	}
	projectResolver, err := lightProjectIdentityResolver(database)
	if err != nil {
		return SessionAnalyticsPage{}, false, err
	}
	records := make([]SessionAnalyticsRecord, 0, len(projections))
	for _, projection := range projections {
		record := lightSessionRecord(projection, projectResolver)
		if len(filter.ProjectIDs) == 0 || lightProjectIDIncluded(record.Project.ProjectID, filter.ProjectIDs) {
			records = append(records, record)
		}
	}
	pricingEvidence, err := attachLightSessionPricing(database, filter, records)
	if err != nil {
		return SessionAnalyticsPage{}, false, err
	}
	page.PricingSource = pricingEvidence.source
	page.Currency = pricingEvidence.currency
	sortLightSessionRecords(records, filter)
	page.MatchedCount = int64(len(records))
	matchedTotals := lightTotalsForRecords(records)
	page.MatchedTotals = &matchedTotals
	if filter.Cursor != nil {
		filtered := records[:0]
		for _, record := range records {
			if lightSessionAfterCursor(record, filter) {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	hasMore := len(records) > filter.Limit
	if hasMore {
		records = records[:filter.Limit]
	}
	page.Records = records
	pageTotals := lightTotalsForRecords(records)
	page.PageTotals = &pageTotals
	if hasMore && len(records) > 0 {
		page.NextCursor = sessionCursorForRecord(records[len(records)-1], filter.SortField)
	}
	return page, true, nil
}

func lightSessionAnalytics(
	database *gorm.DB,
	filter SessionAnalyticsDetailFilter,
) (SessionAnalyticsSnapshot, bool, error) {
	var count int64
	if err := database.Model(&lightSessionModel{}).Count(&count).Error; err != nil {
		return SessionAnalyticsSnapshot{}, false, err
	}
	if count == 0 {
		return SessionAnalyticsSnapshot{}, false, nil
	}
	var projection lightSessionAnalyticsProjection
	result := database.Table("light_sessions AS session").
		Joins(`LEFT JOIN light_token_scans AS scan
			ON scan.session_id = session.session_id
			AND scan.generation = session.active_token_generation`).
		Select(`session.session_id, session.thread_name, session.cwd,
			COALESCE(session.recency_at_ms, session.updated_at_ms) AS last_activity_at_ms,
			scan.generation, scan.input_tokens, scan.cached_input_tokens,
			scan.output_tokens, scan.reasoning_tokens,
			CASE WHEN scan.generation IS NULL THEN NULL
				ELSE scan.input_tokens + scan.output_tokens + scan.reasoning_tokens END AS total_tokens`).
		Where("session.session_id = ?", filter.SessionID).Take(&projection)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return SessionAnalyticsSnapshot{}, true, ErrNotFound
		}
		return SessionAnalyticsSnapshot{}, true, result.Error
	}
	projectResolver, err := lightProjectIdentityResolver(database)
	if err != nil {
		return SessionAnalyticsSnapshot{}, true, err
	}
	records := []SessionAnalyticsRecord{lightSessionRecord(projection, projectResolver)}
	pricingEvidence, err := attachLightSessionPricing(database, SessionAnalyticsFilter{}, records)
	if err != nil {
		return SessionAnalyticsSnapshot{}, true, err
	}
	turns, nextCursor, err := loadSessionTurnAnalytics(database, filter, nil)
	if err != nil {
		return SessionAnalyticsSnapshot{}, true, err
	}
	daily, err := loadLightSessionDaily(database, filter.SessionID)
	if err != nil {
		return SessionAnalyticsSnapshot{}, true, err
	}
	return SessionAnalyticsSnapshot{
		Mode: AnalyticsReadLightIndex, Record: records[0],
		PricingSource: pricingEvidence.source, Currency: pricingEvidence.currency,
		ReportingTimezone: "UTC", Daily: daily,
		Turns: turns, NextTurnCursor: nextCursor, PricingVersions: pricingEvidence.versions,
		UnpricedReasons: make([]CostReasonCount, 0),
	}, true, nil
}

func loadLightSessionDaily(database *gorm.DB, sessionID string) ([]UsageDaily, error) {
	var models []lightTokenDailyModel
	if err := database.Table("light_token_daily AS daily").
		Select("daily.*").
		Joins(`JOIN light_sessions AS sessions
			ON sessions.session_id = daily.session_id
			AND sessions.active_token_generation = daily.generation`).
		Where("daily.session_id = ?", sessionID).
		Order("daily.day_start_ms").
		Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]UsageDaily, 0, len(models))
	for _, model := range models {
		input := model.InputTokens
		cached := model.CachedInputTokens
		output := model.OutputTokens
		reasoning := model.ReasoningTokens
		total := input + output + reasoning
		result = append(result, UsageDaily{
			BucketStartMS: model.DayStartMS, ReportingTimezone: "UTC",
			RollupTotals: RollupTotals{
				InputTokens: &input, CachedInputTokens: &cached,
				OutputTokens: &output, ReasoningTokens: &reasoning, TotalTokens: &total,
			},
		})
	}
	return result, nil
}

func lightSessionRecord(
	projection lightSessionAnalyticsProjection,
	projectResolver projectidentity.Resolver,
) SessionAnalyticsRecord {
	title := attribution.NormalizeSessionTitle(projection.SessionID)
	if projection.ThreadName != nil && *projection.ThreadName != "" {
		title.DisplayTitle = *projection.ThreadName
		title.Source = attribution.SourceAppServerName
		title.Reason = attribution.ReasonObserved
	}
	lastActivity := projection.LastActivityAtMS
	project := lightProjectDecision(projectResolver, projection.CWD)
	record := SessionAnalyticsRecord{
		SessionID: projection.SessionID, DisplayTitle: title.DisplayTitle,
		TitleConfidence: title.Confidence, TitleSource: title.Source, TitleReason: title.Reason,
		Project: ProjectAttribution{
			ProjectID: cloneStringIfPresent(project.ProjectID), DisplayName: cloneStringIfPresent(project.DisplayName),
			Confidence: project.Confidence, Source: project.Source, Reason: project.Reason,
		},
		Model: ModelAttribution{
			Confidence: AttributionConfidenceUnknown, Source: AttributionSourceMissing, Reason: AttributionReasonMissing,
		},
		Activity: SessionActivityIdle, LastActivityAtMS: &lastActivity,
	}
	if projection.Generation != nil {
		record.Rollup = &RollupTotals{
			InputTokens: cloneInt64Pointer(projection.InputTokens), CachedInputTokens: cloneInt64Pointer(projection.CachedInputTokens),
			OutputTokens: cloneInt64Pointer(projection.OutputTokens), ReasoningTokens: cloneInt64Pointer(projection.ReasoningTokens),
			TotalTokens: cloneInt64Pointer(projection.TotalTokens), LastActivityAtMS: projection.LastActivityAtMS,
			UpdatedAtMS: projection.LastActivityAtMS,
		}
	}
	return record
}

func lightProjectIdentityResolver(database *gorm.DB) (projectidentity.Resolver, error) {
	var paths []string
	if err := database.Model(&lightSessionModel{}).Distinct().Order("cwd").Pluck("cwd", &paths).Error; err != nil {
		return projectidentity.Resolver{}, err
	}
	return projectidentity.NewResolver(paths), nil
}

func lightProjectDecision(
	resolver projectidentity.Resolver,
	cwd string,
) attribution.ProjectDecision {
	resolution := resolver.Resolve(cwd)
	if resolution.Other {
		return attribution.ResolveProject(attribution.ProjectInput{})
	}
	return attribution.ResolveProject(attribution.ProjectInput{CWD: resolution.CanonicalPath})
}

func lightProjectIDIncluded(projectID *string, allowed []string) bool {
	if projectID == nil {
		return false
	}
	for _, value := range allowed {
		if value == *projectID {
			return true
		}
	}
	return false
}

func cloneStringIfPresent(value string) *string {
	if value == "" {
		return nil
	}
	cloned := value
	return &cloned
}

func lightTotalsForRecords(records []SessionAnalyticsRecord) RollupTotals {
	input, cached, output, reasoning, total, estimated := int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)
	hasEstimated := false
	complete := true
	for _, record := range records {
		if record.Rollup == nil || record.Rollup.InputTokens == nil || record.Rollup.CachedInputTokens == nil ||
			record.Rollup.OutputTokens == nil || record.Rollup.ReasoningTokens == nil || record.Rollup.TotalTokens == nil {
			complete = false
			continue
		}
		input += *record.Rollup.InputTokens
		cached += *record.Rollup.CachedInputTokens
		output += *record.Rollup.OutputTokens
		reasoning += *record.Rollup.ReasoningTokens
		total += *record.Rollup.TotalTokens
		if record.Rollup.EstimatedUSDMicros != nil {
			estimated += *record.Rollup.EstimatedUSDMicros
			hasEstimated = true
		}
	}
	if !complete {
		return RollupTotals{}
	}
	result := RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total,
	}
	if hasEstimated {
		result.EstimatedUSDMicros = &estimated
	}
	return result
}

func sortLightSessionRecords(records []SessionAnalyticsRecord, filter SessionAnalyticsFilter) {
	sort.SliceStable(records, func(left, right int) bool {
		leftValue := lightSessionSortValue(records[left], filter.SortField)
		rightValue := lightSessionSortValue(records[right], filter.SortField)
		if leftValue == nil || rightValue == nil {
			if leftValue == nil && rightValue == nil {
				return records[left].SessionID > records[right].SessionID
			}
			return rightValue == nil
		}
		if *leftValue == *rightValue {
			return records[left].SessionID > records[right].SessionID
		}
		if filter.SortDirection == AnalyticsSortAscending {
			return *leftValue < *rightValue
		}
		return *leftValue > *rightValue
	})
}

func lightSessionSortValue(record SessionAnalyticsRecord, field SessionAnalyticsSortField) *int64 {
	switch field {
	case SessionAnalyticsSortTotalTokens:
		if record.Rollup != nil {
			return record.Rollup.TotalTokens
		}
		return nil
	case SessionAnalyticsSortEstimatedCost:
		if record.Rollup != nil {
			return record.Rollup.EstimatedUSDMicros
		}
		return nil
	default:
		return record.LastActivityAtMS
	}
}

func attachLightSessionPricing(
	database *gorm.DB,
	filter SessionAnalyticsFilter,
	records []SessionAnalyticsRecord,
) (lightSessionPricingEvidence, error) {
	if len(records) == 0 {
		return lightSessionPricingEvidence{versions: make([]string, 0)}, nil
	}
	sessionIDs := make([]string, 0, len(records))
	for _, record := range records {
		sessionIDs = append(sessionIDs, record.SessionID)
	}
	var rows []lightSessionPricingProjection
	const sessionBatchSize = 500
	for start := 0; start < len(sessionIDs); start += sessionBatchSize {
		end := min(start+sessionBatchSize, len(sessionIDs))
		query := database.Table("light_token_timed AS timed").
			Select(`timed.session_id, timed.observed_at_ms, timed.model_key, timed.model_source,
				timed.input_tokens, timed.cached_input_tokens, timed.output_tokens, timed.reasoning_tokens`).
			Joins(`JOIN light_sessions AS session ON session.session_id = timed.session_id
				AND session.active_token_generation = timed.generation`).
			Where("timed.session_id IN ?", sessionIDs[start:end])
		if filter.RangeExact {
			query = query.Where("timed.observed_at_ms >= ? AND timed.observed_at_ms < ?",
				*filter.LastActivityAtOrAfterMS, *filter.LastActivityBeforeMS)
		}
		var batch []lightSessionPricingProjection
		if err := query.Find(&batch).Error; err != nil {
			return lightSessionPricingEvidence{}, err
		}
		rows = append(rows, batch...)
	}
	if len(rows) == 0 {
		return lightSessionPricingEvidence{versions: make([]string, 0)}, nil
	}
	pricingEndAtMS := int64(0)
	for _, row := range rows {
		if row.ObservedAtMS >= pricingEndAtMS {
			if row.ObservedAtMS == int64(1<<63-1) {
				return lightSessionPricingEvidence{}, invalidRecord("light session pricing timestamp overflows")
			}
			pricingEndAtMS = row.ObservedAtMS + 1
		}
	}
	if filter.RangeExact {
		pricingEndAtMS = *filter.LastActivityBeforeMS
	}
	catalogs, err := loadLightPricingCatalogs(database, pricingEndAtMS)
	if err != nil {
		return lightSessionPricingEvidence{}, err
	}
	evidence := lightSessionPricingEvidence{versions: make([]string, 0)}
	if len(catalogs) == 0 {
		return evidence, nil
	}
	evidence.source, evidence.currency = "openai-api", "USD"
	catalogByVersion := make(map[string]lightPricingCatalog, len(catalogs))
	for _, catalog := range catalogs {
		catalogByVersion[catalog.version.PricingVersion] = catalog
	}
	groups := make(map[lightSessionPricingGroupKey]*lightCostGroup)
	usedVersions := make(map[string]struct{})
	for _, row := range rows {
		dimension := lightModelDimension(row.ModelKey, row.ModelSource)
		catalog := effectiveLightPricingCatalog(catalogs, row.ObservedAtMS)
		pricingVersion := ""
		if catalog != nil {
			pricingVersion = catalog.version.PricingVersion
			usedVersions[pricingVersion] = struct{}{}
		}
		key := lightSessionPricingGroupKey{
			sessionID: row.SessionID, dimensionKey: dimension.key, pricingVersion: pricingVersion,
		}
		group := groups[key]
		if group == nil {
			group = &lightCostGroup{dimension: dimension}
			groups[key] = group
		}
		if err := addLightTokens(
			group, row.InputTokens, row.CachedInputTokens, row.OutputTokens, row.ReasoningTokens,
		); err != nil {
			return lightSessionPricingEvidence{}, err
		}
	}
	bySession := make(map[string]*lightRollupAccumulator)
	for key, group := range groups {
		estimated, err := calculateLightGroupCost(group, catalogByVersion, key.pricingVersion)
		if err != nil {
			return lightSessionPricingEvidence{}, err
		}
		if err := accumulatorForLight(bySession, key.sessionID).add(group, estimated); err != nil {
			return lightSessionPricingEvidence{}, err
		}
	}
	for version := range usedVersions {
		evidence.versions = append(evidence.versions, version)
	}
	sort.Strings(evidence.versions)
	for index := range records {
		priced := bySession[records[index].SessionID]
		if priced == nil || records[index].Rollup == nil {
			continue
		}
		pricedTotals := priced.totals()
		if !lightSessionTokenTotalsEqual(*records[index].Rollup, pricedTotals) {
			if filter.RangeExact {
				return lightSessionPricingEvidence{}, invalidRecord("light session pricing totals are inconsistent")
			}
			continue
		}
		records[index].Rollup.EstimatedUSDMicros = cloneInt64Pointer(pricedTotals.EstimatedUSDMicros)
	}
	return evidence, nil
}

func lightSessionTokenTotalsEqual(left, right RollupTotals) bool {
	return equalInt64Pointer(left.InputTokens, right.InputTokens) &&
		equalInt64Pointer(left.CachedInputTokens, right.CachedInputTokens) &&
		equalInt64Pointer(left.OutputTokens, right.OutputTokens) &&
		equalInt64Pointer(left.ReasoningTokens, right.ReasoningTokens) &&
		equalInt64Pointer(left.TotalTokens, right.TotalTokens)
}

func lightSessionAfterCursor(record SessionAnalyticsRecord, filter SessionAnalyticsFilter) bool {
	if filter.Cursor == nil {
		return true
	}
	value := lightSessionSortValue(record, filter.SortField)
	cursor := filter.Cursor
	if cursor.Null {
		return value == nil && record.SessionID < cursor.SessionID
	}
	if value == nil {
		return true
	}
	if *value == *cursor.Value {
		return record.SessionID < cursor.SessionID
	}
	if filter.SortDirection == AnalyticsSortAscending {
		return *value > *cursor.Value
	}
	return *value < *cursor.Value
}
