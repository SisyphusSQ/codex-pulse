package store

import (
	"sort"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
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
	query := database.Table("light_sessions AS session").
		Joins(`LEFT JOIN light_token_scans AS scan
			ON scan.session_id = session.session_id
			AND scan.generation = session.active_token_generation`).
		Select(`session.session_id, session.thread_name, session.cwd,
			COALESCE(session.recency_at_ms, session.updated_at_ms) AS last_activity_at_ms,
			scan.generation, scan.input_tokens, scan.cached_input_tokens,
			scan.output_tokens, scan.reasoning_tokens,
			CASE WHEN scan.generation IS NULL THEN NULL
				ELSE scan.input_tokens + scan.output_tokens + scan.reasoning_tokens END AS total_tokens`)
	if filter.LastActivityAtOrAfterMS != nil {
		query = query.Where("COALESCE(session.recency_at_ms, session.updated_at_ms) >= ?", *filter.LastActivityAtOrAfterMS)
	}
	if filter.LastActivityBeforeMS != nil {
		query = query.Where("COALESCE(session.recency_at_ms, session.updated_at_ms) < ?", *filter.LastActivityBeforeMS)
	}
	var projections []lightSessionAnalyticsProjection
	if err := query.Find(&projections).Error; err != nil {
		return SessionAnalyticsPage{}, false, err
	}
	records := make([]SessionAnalyticsRecord, 0, len(projections))
	for _, projection := range projections {
		record := lightSessionRecord(projection)
		if len(filter.ProjectIDs) == 0 || lightProjectIDIncluded(record.Project.ProjectID, filter.ProjectIDs) {
			records = append(records, record)
		}
	}
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
	page, handled, err := listLightSessionAnalytics(database, SessionAnalyticsFilter{
		Limit: 1, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || !handled {
		return SessionAnalyticsSnapshot{}, handled, err
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
	_ = page
	turns, nextCursor, err := loadSessionTurnAnalytics(database, filter, nil)
	if err != nil {
		return SessionAnalyticsSnapshot{}, true, err
	}
	return SessionAnalyticsSnapshot{
		Mode: AnalyticsReadLightIndex, Record: lightSessionRecord(projection),
		Turns: turns, NextTurnCursor: nextCursor, PricingVersions: make([]string, 0),
		UnpricedReasons: make([]CostReasonCount, 0),
	}, true, nil
}

func lightSessionRecord(projection lightSessionAnalyticsProjection) SessionAnalyticsRecord {
	title := attribution.NormalizeSessionTitle(projection.SessionID)
	if projection.ThreadName != nil && *projection.ThreadName != "" {
		title.DisplayTitle = *projection.ThreadName
		title.Source = attribution.SourceAppServerName
		title.Reason = attribution.ReasonObserved
	}
	lastActivity := projection.LastActivityAtMS
	project := attribution.ResolveProject(attribution.ProjectInput{CWD: projection.CWD})
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
	input, cached, output, reasoning, total := int64(0), int64(0), int64(0), int64(0), int64(0)
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
	}
	if !complete {
		return RollupTotals{}
	}
	return RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total,
	}
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
		return nil
	default:
		return record.LastActivityAtMS
	}
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
