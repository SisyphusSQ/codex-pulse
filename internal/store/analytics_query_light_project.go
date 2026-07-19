package store

import (
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/attribution"
)

type lightProjectDeltaProjection struct {
	SessionID       string  `gorm:"column:session_id"`
	ThreadName      *string `gorm:"column:thread_name"`
	CWD             string  `gorm:"column:cwd"`
	ObservedAtMS    int64   `gorm:"column:observed_at_ms"`
	InputTokens     int64   `gorm:"column:input_tokens"`
	CachedInput     int64   `gorm:"column:cached_input_tokens"`
	OutputTokens    int64   `gorm:"column:output_tokens"`
	ReasoningTokens int64   `gorm:"column:reasoning_tokens"`
}

type lightProjectGroup struct {
	record   ProjectAnalyticsRecord
	sessions map[string]struct{}
	daily    map[int64]*RollupTotals
}

func listLightProjectAnalytics(
	database *gorm.DB,
	filter ProjectAnalyticsFilter,
) (ProjectAnalyticsPage, bool, error) {
	var sessionCount int64
	if err := database.Model(&lightSessionModel{}).Count(&sessionCount).Error; err != nil {
		return ProjectAnalyticsPage{}, false, err
	}
	if sessionCount == 0 {
		return ProjectAnalyticsPage{}, false, nil
	}
	location, err := time.LoadLocation(filter.Range.ReportingTimezone)
	if err != nil {
		return ProjectAnalyticsPage{}, true, err
	}
	var rows []lightProjectDeltaProjection
	err = database.Table("light_token_timed AS timed").
		Select(`session.session_id, session.cwd, timed.observed_at_ms,
			timed.input_tokens, timed.cached_input_tokens, timed.output_tokens, timed.reasoning_tokens`).
		Joins(`JOIN light_sessions AS session ON session.session_id = timed.session_id
			AND session.active_token_generation = timed.generation`).
		Where("timed.observed_at_ms >= ? AND timed.observed_at_ms < ?", filter.Range.StartAtMS, filter.Range.EndAtMS).
		Order("timed.observed_at_ms, timed.session_id, timed.source_offset").Find(&rows).Error
	if err != nil {
		return ProjectAnalyticsPage{}, true, err
	}
	groups := make(map[string]*lightProjectGroup)
	for _, row := range rows {
		decision := attribution.ResolveProject(attribution.ProjectInput{CWD: row.CWD})
		dimensionKey := decision.ProjectID
		if dimensionKey == "" {
			dimensionKey = "unknown|" + string(decision.Confidence) + "|" + string(decision.Source) + "|" + string(decision.Reason)
		}
		group := groups[dimensionKey]
		if group == nil {
			group = &lightProjectGroup{
				record: ProjectAnalyticsRecord{
					DimensionKey: dimensionKey, ProjectID: cloneStringIfPresent(decision.ProjectID),
					ProjectDisplayName:    cloneStringIfPresent(decision.DisplayName),
					AttributionConfidence: string(decision.Confidence), AttributionSource: string(decision.Source),
					AttributionReason: string(decision.Reason), Trend: make([]ProjectUsageDaily, 0),
				},
				sessions: make(map[string]struct{}), daily: make(map[int64]*RollupTotals),
			}
			groups[dimensionKey] = group
		}
		group.sessions[row.SessionID] = struct{}{}
		if err := addLightProjectDelta(&group.record.Totals, row); err != nil {
			return ProjectAnalyticsPage{}, true, err
		}
		bucket := localDayBucketStart(row.ObservedAtMS, location)
		if group.daily[bucket] == nil {
			group.daily[bucket] = &RollupTotals{}
		}
		if err := addLightProjectDelta(group.daily[bucket], row); err != nil {
			return ProjectAnalyticsPage{}, true, err
		}
	}
	all := make([]ProjectAnalyticsRecord, 0, len(groups))
	for _, group := range groups {
		group.record.SessionCount = int64(len(group.sessions))
		buckets := make([]int64, 0, len(group.daily))
		for bucket := range group.daily {
			buckets = append(buckets, bucket)
		}
		sort.Slice(buckets, func(left, right int) bool { return buckets[left] < buckets[right] })
		if len(buckets) > 30 {
			buckets = buckets[len(buckets)-30:]
		}
		for _, bucket := range buckets {
			group.record.Trend = append(group.record.Trend, lightProjectDaily(group.record, bucket, *group.daily[bucket], filter.Range.ReportingTimezone))
		}
		all = append(all, group.record)
	}
	global, err := sumLightProjectRecords(all)
	if err != nil {
		return ProjectAnalyticsPage{}, true, err
	}
	matched := make([]ProjectAnalyticsRecord, 0, len(all))
	for _, record := range all {
		if lightProjectRecordMatches(record, filter) {
			matched = append(matched, record)
		}
	}
	sortLightProjectRecords(matched, filter)
	matchedTotals, err := sumLightProjectRecords(matched)
	if err != nil {
		return ProjectAnalyticsPage{}, true, err
	}
	matchedCount := int64(len(matched))
	if filter.Cursor != nil {
		filtered := matched[:0]
		for _, record := range matched {
			if lightProjectAfterCursor(record, filter) {
				filtered = append(filtered, record)
			}
		}
		matched = filtered
	}
	hasMore := len(matched) > filter.Limit
	if hasMore {
		matched = matched[:filter.Limit]
	}
	pageTotals, err := sumLightProjectRecords(matched)
	if err != nil {
		return ProjectAnalyticsPage{}, true, err
	}
	page := ProjectAnalyticsPage{
		Mode: AnalyticsReadLightIndex, Records: matched, MatchedCount: matchedCount,
		GlobalTotals: global, MatchedTotals: matchedTotals, PageTotals: pageTotals,
		PricingVersions: make([]string, 0),
	}
	if hasMore && len(matched) > 0 {
		page.NextCursor = lightProjectCursor(matched[len(matched)-1], filter.SortField)
	}
	return page, true, nil
}

func lightProjectAnalytics(
	database *gorm.DB,
	filter ProjectAnalyticsDetailFilter,
) (ProjectAnalyticsSnapshot, bool, error) {
	page, handled, err := listLightProjectAnalytics(database, ProjectAnalyticsFilter{
		Range: filter.Range, ProjectIDs: []string{filter.DimensionKey}, Limit: 1,
		SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || !handled {
		return ProjectAnalyticsSnapshot{}, handled, err
	}
	if len(page.Records) != 1 {
		return ProjectAnalyticsSnapshot{}, true, ErrNotFound
	}
	var rows []lightProjectDeltaProjection
	if err := database.Table("light_token_timed AS timed").
		Select(`session.session_id, session.thread_name, session.cwd, timed.observed_at_ms,
			timed.input_tokens, timed.cached_input_tokens, timed.output_tokens, timed.reasoning_tokens`).
		Joins(`JOIN light_sessions AS session ON session.session_id = timed.session_id
			AND session.active_token_generation = timed.generation`).
		Where("timed.observed_at_ms >= ? AND timed.observed_at_ms < ?", filter.Range.StartAtMS, filter.Range.EndAtMS).
		Order("timed.observed_at_ms, timed.session_id, timed.source_offset").Find(&rows).Error; err != nil {
		return ProjectAnalyticsSnapshot{}, true, err
	}
	type sessionTotals struct {
		name   *string
		totals RollupTotals
	}
	bySession := make(map[string]*sessionTotals)
	for _, row := range rows {
		decision := attribution.ResolveProject(attribution.ProjectInput{CWD: row.CWD})
		dimensionKey := decision.ProjectID
		if dimensionKey == "" {
			dimensionKey = "unknown|" + string(decision.Confidence) + "|" + string(decision.Source) + "|" + string(decision.Reason)
		}
		if dimensionKey != filter.DimensionKey {
			continue
		}
		session := bySession[row.SessionID]
		if session == nil {
			session = &sessionTotals{name: row.ThreadName}
			bySession[row.SessionID] = session
		}
		if err := addLightProjectDelta(&session.totals, row); err != nil {
			return ProjectAnalyticsSnapshot{}, true, err
		}
	}
	items := make([]ProjectSessionAnalyticsRecord, 0, len(bySession))
	for sessionID, session := range bySession {
		title := attribution.NormalizeSessionTitle(sessionID)
		if session.name != nil && *session.name != "" {
			title.DisplayTitle = *session.name
			title.Source = attribution.SourceAppServerName
			title.Reason = attribution.ReasonObserved
		}
		items = append(items, ProjectSessionAnalyticsRecord{
			SessionID: sessionID, DisplayTitle: title.DisplayTitle,
			TitleConfidence: title.Confidence, TitleSource: title.Source, TitleReason: title.Reason,
			Model: ModelAttribution{
				Confidence: AttributionConfidenceUnknown, Source: AttributionSourceMissing, Reason: AttributionReasonMissing,
			},
			Activity: SessionActivityIdle, LastActivityAtMS: session.totals.LastActivityAtMS, Totals: session.totals,
		})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].LastActivityAtMS == items[right].LastActivityAtMS {
			return items[left].SessionID > items[right].SessionID
		}
		return items[left].LastActivityAtMS > items[right].LastActivityAtMS
	})
	if filter.SessionCursor != nil {
		filtered := items[:0]
		for _, item := range items {
			if item.LastActivityAtMS < filter.SessionCursor.LastActivityAtMS ||
				(item.LastActivityAtMS == filter.SessionCursor.LastActivityAtMS && item.SessionID < filter.SessionCursor.SessionID) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	hasMore := len(items) > filter.SessionLimit
	if hasMore {
		items = items[:filter.SessionLimit]
	}
	record := page.Records[0]
	result := ProjectAnalyticsSnapshot{
		Mode: AnalyticsReadLightIndex, Record: record,
		Daily: append([]ProjectUsageDaily(nil), record.Trend...), Sessions: items,
		Models: make([]ProjectModelAnalyticsRecord, 0), GlobalTotals: page.GlobalTotals,
		PricingVersions: make([]string, 0),
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		result.NextSessionCursor = &ProjectSessionAnalyticsCursor{
			DimensionKey: filter.DimensionKey, SessionID: last.SessionID, LastActivityAtMS: last.LastActivityAtMS,
		}
	}
	return result, true, nil
}

func addLightProjectDelta(total *RollupTotals, row lightProjectDeltaProjection) error {
	values := []struct {
		target **int64
		value  int64
	}{
		{&total.InputTokens, row.InputTokens}, {&total.CachedInputTokens, row.CachedInput},
		{&total.OutputTokens, row.OutputTokens}, {&total.ReasoningTokens, row.ReasoningTokens},
	}
	for _, value := range values {
		current := int64(0)
		if *value.target != nil {
			current = **value.target
		}
		next, err := checkedAdd(current, value.value)
		if err != nil {
			return err
		}
		*value.target = &next
	}
	totalValue, err := checkedAdd(*total.InputTokens, *total.OutputTokens)
	if err == nil {
		totalValue, err = checkedAdd(totalValue, *total.ReasoningTokens)
	}
	if err != nil {
		return err
	}
	total.TotalTokens = &totalValue
	if total.FirstActivityAtMS == 0 || row.ObservedAtMS < total.FirstActivityAtMS {
		total.FirstActivityAtMS = row.ObservedAtMS
	}
	if row.ObservedAtMS > total.LastActivityAtMS {
		total.LastActivityAtMS = row.ObservedAtMS
	}
	total.UpdatedAtMS = total.LastActivityAtMS
	return nil
}

func lightProjectDaily(record ProjectAnalyticsRecord, bucket int64, totals RollupTotals, timezone string) ProjectUsageDaily {
	return ProjectUsageDaily{
		BucketStartMS: bucket, ReportingTimezone: timezone, DimensionKey: record.DimensionKey,
		ProjectID: cloneStringPointerStore(record.ProjectID), ProjectDisplayName: cloneStringPointerStore(record.ProjectDisplayName),
		AttributionConfidence: record.AttributionConfidence, AttributionSource: record.AttributionSource,
		AttributionReason: record.AttributionReason, RollupTotals: totals,
	}
}

func lightProjectRecordMatches(record ProjectAnalyticsRecord, filter ProjectAnalyticsFilter) bool {
	if len(filter.ProjectIDs) > 0 && !lightProjectIDIncluded(record.ProjectID, filter.ProjectIDs) {
		return false
	}
	if len(filter.Confidences) > 0 {
		for _, value := range filter.Confidences {
			if record.AttributionConfidence == value {
				return true
			}
		}
		return false
	}
	return true
}

func sumLightProjectRecords(records []ProjectAnalyticsRecord) (RollupTotals, error) {
	zero := int64(0)
	result := RollupTotals{
		InputTokens: &zero, CachedInputTokens: cloneInt64Pointer(&zero), OutputTokens: cloneInt64Pointer(&zero),
		ReasoningTokens: cloneInt64Pointer(&zero), TotalTokens: cloneInt64Pointer(&zero),
	}
	for _, record := range records {
		for _, pair := range []struct{ target, value *int64 }{
			{result.InputTokens, record.Totals.InputTokens}, {result.CachedInputTokens, record.Totals.CachedInputTokens},
			{result.OutputTokens, record.Totals.OutputTokens}, {result.ReasoningTokens, record.Totals.ReasoningTokens},
			{result.TotalTokens, record.Totals.TotalTokens},
		} {
			if pair.value == nil {
				continue
			}
			next, err := checkedAdd(*pair.target, *pair.value)
			if err != nil {
				return RollupTotals{}, err
			}
			*pair.target = next
		}
		if result.FirstActivityAtMS == 0 || record.Totals.FirstActivityAtMS < result.FirstActivityAtMS {
			result.FirstActivityAtMS = record.Totals.FirstActivityAtMS
		}
		if record.Totals.LastActivityAtMS > result.LastActivityAtMS {
			result.LastActivityAtMS = record.Totals.LastActivityAtMS
		}
	}
	result.UpdatedAtMS = result.LastActivityAtMS
	return result, nil
}

func sortLightProjectRecords(records []ProjectAnalyticsRecord, filter ProjectAnalyticsFilter) {
	sort.SliceStable(records, func(left, right int) bool {
		comparison := lightProjectCompare(records[left], records[right], filter.SortField)
		if comparison == 0 {
			return records[left].DimensionKey > records[right].DimensionKey
		}
		if filter.SortDirection == AnalyticsSortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func lightProjectCompare(left, right ProjectAnalyticsRecord, field ProjectAnalyticsSortField) int {
	if field == ProjectAnalyticsSortDisplayName {
		leftName, rightName := "", ""
		if left.ProjectDisplayName != nil {
			leftName = *left.ProjectDisplayName
		}
		if right.ProjectDisplayName != nil {
			rightName = *right.ProjectDisplayName
		}
		if leftName < rightName {
			return -1
		}
		if leftName > rightName {
			return 1
		}
		return 0
	}
	leftValue, rightValue := lightProjectSortNumeric(left, field), lightProjectSortNumeric(right, field)
	if leftValue == nil || rightValue == nil {
		if leftValue == nil && rightValue == nil {
			return 0
		}
		if leftValue == nil {
			return -1
		}
		return 1
	}
	if *leftValue < *rightValue {
		return -1
	}
	if *leftValue > *rightValue {
		return 1
	}
	return 0
}

func lightProjectSortNumeric(record ProjectAnalyticsRecord, field ProjectAnalyticsSortField) *int64 {
	switch field {
	case ProjectAnalyticsSortTotalTokens:
		return record.Totals.TotalTokens
	case ProjectAnalyticsSortEstimatedCost:
		return nil
	default:
		return &record.Totals.LastActivityAtMS
	}
}

func lightProjectCursor(record ProjectAnalyticsRecord, field ProjectAnalyticsSortField) *ProjectAnalyticsCursor {
	cursor := &ProjectAnalyticsCursor{DimensionKey: record.DimensionKey}
	if field == ProjectAnalyticsSortDisplayName {
		cursor.TextValue = cloneStringPointerStore(record.ProjectDisplayName)
	} else {
		cursor.NumericValue = cloneInt64Pointer(lightProjectSortNumeric(record, field))
	}
	cursor.Null = cursor.NumericValue == nil && cursor.TextValue == nil
	return cursor
}

func lightProjectAfterCursor(record ProjectAnalyticsRecord, filter ProjectAnalyticsFilter) bool {
	if filter.Cursor == nil {
		return true
	}
	current := lightProjectCursor(record, filter.SortField)
	comparison := 0
	if filter.SortField == ProjectAnalyticsSortDisplayName {
		left, right := "", ""
		if current.TextValue != nil {
			left = *current.TextValue
		}
		if filter.Cursor.TextValue != nil {
			right = *filter.Cursor.TextValue
		}
		if left < right {
			comparison = -1
		} else if left > right {
			comparison = 1
		}
	} else if current.NumericValue == nil || filter.Cursor.NumericValue == nil {
		if current.NumericValue == nil && filter.Cursor.NumericValue != nil {
			comparison = -1
		}
		if current.NumericValue != nil && filter.Cursor.NumericValue == nil {
			comparison = 1
		}
	} else if *current.NumericValue < *filter.Cursor.NumericValue {
		comparison = -1
	} else if *current.NumericValue > *filter.Cursor.NumericValue {
		comparison = 1
	}
	if comparison == 0 {
		return record.DimensionKey < filter.Cursor.DimensionKey
	}
	if filter.SortDirection == AnalyticsSortAscending {
		return comparison > 0
	}
	return comparison < 0
}
