package agyprovider

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func usageRange(local basequery.LocalDateRange, exact *basequery.UTCTimeRange) (*basequery.UTCTimeRange, error) {
	if exact != nil {
		if local != (basequery.LocalDateRange{}) {
			return nil, basequery.NewValidationFailure("range", nil)
		}
		return basequery.NormalizeExactTimeRange(*exact, maxRangeDays)
	}
	return basequery.NormalizeLocalDateRange(local, maxRangeDays)
}

func validTrendGranularity(value usagecost.TrendGranularity) bool {
	return value == usagecost.TrendHour || value == usagecost.TrendDay || value == usagecost.TrendWeek || value == usagecost.TrendMonth
}

func usageEventsInRange(events []store.AGYUsageEvent, rangeValue basequery.UTCTimeRange) []store.AGYUsageEvent {
	result := make([]store.AGYUsageEvent, 0)
	for _, event := range events {
		if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
			result = append(result, event)
		}
	}
	return result
}

// totalsForUsageEvents 按 AGY Uncached Input 语义聚合 Token 与费用：
// input_tokens = uncached_input + cache_read，cached_input_tokens = cache_read，
// output_tokens = outputTokens，reasoning_tokens = thinkingOutputTokens（output 子集），
// total_tokens = uncached_input + cache_read + outputTokens。
func totalsForUsageEvents(events []store.AGYUsageEvent) (usagecost.UsageTotals, bool) {
	if len(events) == 0 {
		return usagecost.UsageTotals{
			TurnCount:          known(0, basequery.NumericCount),
			InputTokens:        known(0, basequery.NumericTokens),
			CachedInputTokens:  known(0, basequery.NumericTokens),
			OutputTokens:       known(0, basequery.NumericTokens),
			ReasoningTokens:    known(0, basequery.NumericTokens),
			TotalTokens:        known(0, basequery.NumericTokens),
			EstimatedUSDMicros: unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable),
			PricedTurnCount:    known(0, basequery.NumericCount),
			UnpricedTurnCount:  known(0, basequery.NumericCount),
			FirstActivityAtMS:  unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable),
			LastActivityAtMS:   unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable),
		}, false
	}
	var input, cached, output, reasoning, total, first, last, priced, unpriced, estimated int64
	estimatedAllKnown := true
	cacheKnown, totalKnown, reasoningKnown := true, true, true
	for _, event := range events {
		cacheKnown = cacheKnown && event.CacheReadKnown
		totalKnown = totalKnown && event.TotalKnown
		reasoningKnown = reasoningKnown && event.ReasoningKnown
		input += event.UncachedInputTokens + event.CacheReadTokens
		cached += event.CacheReadTokens
		output += event.OutputTokens
		reasoning += event.ReasoningTokens
		if event.TotalKnown {
			total += event.TotalTokens
		} else {
			total += event.UncachedInputTokens + event.CacheReadTokens + event.OutputTokens
		}
		first = minPositive(first, event.OccurredAtMS)
		last = maxInt64(last, event.OccurredAtMS)
		if value, ok := estimateEvent(event); ok {
			estimated += value
			priced++
		} else {
			estimatedAllKnown = false
			unpriced++
		}
	}
	estimatedValue := unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	if estimatedAllKnown && priced > 0 {
		estimatedValue = known(estimated, basequery.NumericMicroUSD)
	}
	firstActivity, lastActivity := activityBoundaries(int64(len(events)), first, last)
	inputValue := known(input, basequery.NumericTokens)
	cacheValue := known(cached, basequery.NumericTokens)
	totalValue := known(total, basequery.NumericTokens)
	reasoningValue := known(reasoning, basequery.NumericTokens)
	if !cacheKnown {
		inputValue = unknown(basequery.NumericTokens, basequery.UnknownUnavailable)
		cacheValue = unknown(basequery.NumericTokens, basequery.UnknownUnavailable)
	}
	if !totalKnown {
		totalValue = unknown(basequery.NumericTokens, basequery.UnknownUnavailable)
	}
	if !reasoningKnown {
		reasoningValue = unknown(basequery.NumericTokens, basequery.UnknownUnavailable)
	}
	return usagecost.UsageTotals{
		TurnCount:          known(int64(len(events)), basequery.NumericCount),
		InputTokens:        inputValue,
		CachedInputTokens:  cacheValue,
		OutputTokens:       known(output, basequery.NumericTokens),
		ReasoningTokens:    reasoningValue,
		TotalTokens:        totalValue,
		EstimatedUSDMicros: estimatedValue,
		PricedTurnCount:    known(priced, basequery.NumericCount),
		UnpricedTurnCount:  known(unpriced, basequery.NumericCount),
		FirstActivityAtMS:  firstActivity,
		LastActivityAtMS:   lastActivity,
	}, estimatedAllKnown && priced > 0
}

func unpricedCount(events []store.AGYUsageEvent) int64 {
	var count int64
	for _, event := range events {
		if _, ok := estimateEvent(event); !ok {
			count++
		}
	}
	return count
}

func usageTrend(events []store.AGYUsageEvent, rangeValue basequery.UTCTimeRange, granularity usagecost.TrendGranularity) []usagecost.TrendPoint {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return []usagecost.TrendPoint{}
	}
	type bucket struct {
		start, end time.Time
		events     []store.AGYUsageEvent
	}
	groups := make(map[int64]*bucket)
	for _, event := range events {
		at := time.UnixMilli(event.OccurredAtMS).In(location)
		start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, location)
		switch granularity {
		case usagecost.TrendHour:
			start = time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, location)
		case usagecost.TrendWeek:
			start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		case usagecost.TrendMonth:
			start = time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, location)
		}
		end := start.AddDate(0, 0, 1)
		switch granularity {
		case usagecost.TrendHour:
			end = start.Add(time.Hour)
		case usagecost.TrendWeek:
			end = start.AddDate(0, 0, 7)
		case usagecost.TrendMonth:
			end = start.AddDate(0, 1, 0)
		}
		key := start.UnixMilli()
		if groups[key] == nil {
			groups[key] = &bucket{start: start, end: end}
		}
		groups[key].events = append(groups[key].events, event)
	}
	keys := make([]int64, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := make([]usagecost.TrendPoint, 0, len(keys))
	for _, key := range keys {
		value := groups[key]
		totals, _ := totalsForUsageEvents(value.events)
		result = append(result, usagecost.TrendPoint{
			Key:       trendKey(value.start, granularity),
			StartAtMS: known(value.start.UnixMilli(), basequery.NumericMilliseconds),
			EndAtMS:   known(value.end.UnixMilli(), basequery.NumericMilliseconds),
			Totals:    totals,
		})
	}
	return result
}

func trendKey(value time.Time, granularity usagecost.TrendGranularity) string {
	switch granularity {
	case usagecost.TrendHour:
		return value.Format("2006-01-02T15:00-07:00")
	case usagecost.TrendDay:
		return value.Format("2006-01-02")
	case usagecost.TrendWeek:
		year, week := value.ISOWeek()
		return strconv.FormatInt(int64(year), 10) + "-W" + pad2(week)
	default:
		return value.Format("2006-01")
	}
}

func pad2(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func usageModels(events []store.AGYUsageEvent, rangeValue basequery.UTCTimeRange, granularity usagecost.TrendGranularity) []usagecost.UsageModelItem {
	groups := make(map[string][]store.AGYUsageEvent)
	for _, event := range events {
		key := "unknown"
		if event.ModelKey != nil && *event.ModelKey != "" {
			key = *event.ModelKey
		}
		groups[key] = append(groups[key], event)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]usagecost.UsageModelItem, 0, len(keys))
	for _, key := range keys {
		var model *string
		if key != "unknown" {
			value := key
			model = &value
		}
		totals, _ := totalsForUsageEvents(groups[key])
		result = append(result, usagecost.UsageModelItem{
			DimensionKey: key,
			Model:        modelAttribution(model),
			Totals:       totals,
			Trend:        usageTrend(groups[key], rangeValue, granularity),
		})
	}
	return result
}

func activityDistribution(events []store.AGYUsageEvent, rangeValue basequery.UTCTimeRange) *usagecost.ActivityDistribution {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return nil
	}
	type bucket struct {
		start, end int64
		tokens     int64
		unknown    bool
		sessions   map[string]struct{}
	}
	timeline := map[int64]*bucket{}
	weekdayHours := map[int]*bucket{}
	for _, event := range events {
		at := time.UnixMilli(event.OccurredAtMS).In(location)
		start := time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, location)
		key := maxInt64(start.UnixMilli(), rangeValue.StartAtMS)
		end := start.Add(time.Hour).UnixMilli()
		if end > rangeValue.EndAtMS {
			end = rangeValue.EndAtMS
		}
		if timeline[key] == nil {
			timeline[key] = &bucket{start: key, end: end, sessions: map[string]struct{}{}}
		}
		evTokens := event.TotalTokens
		if !event.TotalKnown {
			evTokens = event.UncachedInputTokens + event.CacheReadTokens + event.OutputTokens
		}
		timeline[key].unknown = timeline[key].unknown || !event.TotalKnown
		timeline[key].tokens += evTokens
		timeline[key].sessions[event.ExternalSessionID] = struct{}{}

		weekday := int(at.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		cellKey := weekday*24 + at.Hour()
		if weekdayHours[cellKey] == nil {
			weekdayHours[cellKey] = &bucket{sessions: map[string]struct{}{}}
		}
		weekdayHours[cellKey].unknown = weekdayHours[cellKey].unknown || !event.TotalKnown
		weekdayHours[cellKey].tokens += evTokens
		weekdayHours[cellKey].sessions[event.ExternalSessionID] = struct{}{}
	}
	result := &usagecost.ActivityDistribution{
		TimelineGranularity:   usagecost.TrendHour,
		TimelineBucketMinutes: 60,
		Timeline:              []usagecost.ActivityTimelinePoint{},
		WeekdayHours:          []usagecost.ActivityWeekdayHourPoint{},
	}
	keys := make([]int64, 0, len(timeline))
	for key := range timeline {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		item := timeline[key]
		result.Timeline = append(result.Timeline, usagecost.ActivityTimelinePoint{
			StartAtMS: known(item.start, basequery.NumericMilliseconds),
			EndAtMS:   known(item.end, basequery.NumericMilliseconds),
			Metrics: usagecost.ActivityMetrics{
				TotalTokens:  activityTokens(item.tokens, item.unknown),
				SessionCount: known(int64(len(item.sessions)), basequery.NumericCount),
			},
		})
	}
	cellKeys := make([]int, 0, len(weekdayHours))
	for key := range weekdayHours {
		cellKeys = append(cellKeys, key)
	}
	sort.Ints(cellKeys)
	for _, key := range cellKeys {
		item := weekdayHours[key]
		result.WeekdayHours = append(result.WeekdayHours, usagecost.ActivityWeekdayHourPoint{
			Weekday: key / 24,
			Hour:    key % 24,
			Metrics: usagecost.ActivityMetrics{
				TotalTokens:  activityTokens(item.tokens, item.unknown),
				SessionCount: known(int64(len(item.sessions)), basequery.NumericCount),
			},
		})
	}
	return result
}

func sessionItem(session store.AGYSession, events []store.AGYUsageEvent) usagecost.SessionItem {
	var totals usagecost.UsageTotals
	if len(events) == 0 {
		totals = usagecost.UsageTotals{
			InputTokens:        unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
			CachedInputTokens:  unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
			OutputTokens:       unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
			ReasoningTokens:    unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
			TotalTokens:        unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
			EstimatedUSDMicros: unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable),
			PricedTurnCount:    known(0, basequery.NumericCount),
			UnpricedTurnCount:  known(0, basequery.NumericCount),
			FirstActivityAtMS:  unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable),
			LastActivityAtMS:   known(session.LastActivityAtMS, basequery.NumericMilliseconds),
		}
	} else {
		totals, _ = totalsForUsageEvents(events)
	}
	totals.TurnCount = known(session.RequestCount, basequery.NumericCount)
	cache := cacheRate(totals)
	tp := sessionThroughput(session.Throughput)

	title, source, confidence, reason := sessionTitlePresentation(session)
	projConfidence, projReason := projectAttributionMeta(session.ProjectSource)
	return usagecost.SessionItem{
		CacheHitRate:    &cache,
		Throughput:      tp,
		SessionID:       session.ExternalSessionID,
		DisplayTitle:    title,
		TitleConfidence: confidence,
		TitleSource:     source,
		TitleReason:     reason,
		Project: usagecost.AttributionValue{
			ID:          &session.ProjectKey,
			DisplayName: &session.ProjectDisplayName,
			Confidence:  projConfidence,
			Source:      session.ProjectSource,
			Reason:      projReason,
		},
		Model:          modelAttribution(session.ModelKey),
		Activity:       "idle",
		LastActivityAt: known(session.LastActivityAtMS, basequery.NumericMilliseconds),
		Totals:         totals,
	}
}

func projectAttributionMeta(projectSource string) (confidence, reason string) {
	switch projectSource {
	case "workspace":
		return "high", "path_redacted"
	case "inherited_parent":
		return "medium", "inherited_parent"
	default:
		return "low", "data_source_not_provided"
	}
}

func sessionTitlePresentation(session store.AGYSession) (string, string, string, string) {
	if session.DisplayTitle == "" || session.TitleSource == "fallback" {
		title := session.DisplayTitle
		if title == "" {
			title = fallbackSessionTitle(session.LastActivityAtMS)
		}
		return title, "fallback", "low", sessionRoleReason(session.SessionRole, "data_source_not_provided")
	}
	return session.DisplayTitle, session.TitleSource, "high", sessionRoleReason(session.SessionRole, "provider_metadata")
}

func sessionRoleReason(role, fallback string) string {
	switch role {
	case "subagent", "swarm_worker", "side_question":
		return "role:" + role
	default:
		return fallback
	}
}

func modelAttribution(model *string) usagecost.AttributionValue {
	if model == nil || *model == "" {
		return usagecost.AttributionValue{Confidence: "unknown", Source: "missing", Reason: "data_source_not_provided"}
	}
	value := *model
	_, display := pricing.ResolveAGYModelID(value, "")
	if display == "" {
		display = value
	}
	return usagecost.AttributionValue{
		ID:          &value,
		DisplayName: &display,
		Confidence:  "high",
		Source:      "agy_step_metadata",
		Reason:      "provider_metadata",
	}
}

func filterSessions(sessions []store.AGYSession, rangeValue *basequery.UTCTimeRange) []store.AGYSession {
	result := make([]store.AGYSession, 0, len(sessions))
	for _, session := range sessions {
		if rangeValue == nil || (session.LastActivityAtMS >= rangeValue.StartAtMS && session.LastActivityAtMS < rangeValue.EndAtMS) {
			result = append(result, session)
		}
	}
	return result
}

func agySessions(snapshot store.AGYSnapshot, request basequery.ValidatedRequest) ([]store.AGYSession, error) {
	result := filterSessions(snapshot.Sessions, request.TimeRange)
	seen := make(map[string]struct{}, len(request.Filters))
	for _, filter := range request.Filters {
		if _, exists := seen[filter.Field]; exists || duplicateStrings(filter.Values) {
			return nil, basequery.NewValidationFailure("filters", nil)
		}
		seen[filter.Field] = struct{}{}
		switch filter.Field {
		case "projectId":
			result = filterAGYSessions(result, func(session store.AGYSession) bool {
				return containsString(filter.Values, session.ProjectKey) || containsString(filter.Values, session.ProjectDisplayName)
			})
		case "modelKey":
			result = filterAGYSessions(result, func(session store.AGYSession) bool {
				return session.ModelKey != nil && containsString(filter.Values, *session.ModelKey)
			})
		case "activity":
			if len(filter.Values) != 1 || (filter.Values[0] != "active" && filter.Values[0] != "idle") {
				return nil, basequery.NewValidationFailure("filters.values", nil)
			}
			if filter.Values[0] == "active" {
				result = []store.AGYSession{}
			}
		default:
			return nil, basequery.NewValidationFailure("filters.field", nil)
		}
	}
	return result, nil
}

func agySessionsForProjects(snapshot store.AGYSnapshot, request basequery.ValidatedRequest) ([]store.AGYSession, error) {
	result := filterSessions(snapshot.Sessions, request.TimeRange)
	seen := make(map[string]struct{}, len(request.Filters))
	for _, filter := range request.Filters {
		if _, exists := seen[filter.Field]; exists || duplicateStrings(filter.Values) {
			return nil, basequery.NewValidationFailure("filters", nil)
		}
		seen[filter.Field] = struct{}{}
		switch filter.Field {
		case "projectId":
			result = filterAGYSessions(result, func(session store.AGYSession) bool {
				return containsString(filter.Values, session.ProjectKey) || containsString(filter.Values, session.ProjectDisplayName)
			})
		case "confidence":
			for _, value := range filter.Values {
				if value != "high" && value != "medium" && value != "low" && value != "unknown" {
					return nil, basequery.NewValidationFailure("filters.values", nil)
				}
			}
			result = filterAGYSessions(result, func(session store.AGYSession) bool {
				conf, _ := projectAttributionMeta(session.ProjectSource)
				return containsString(filter.Values, conf)
			})
		default:
			return nil, basequery.NewValidationFailure("filters.field", nil)
		}
	}
	return result, nil
}

func filterAGYSessions(sessions []store.AGYSession, keep func(store.AGYSession) bool) []store.AGYSession {
	result := make([]store.AGYSession, 0, len(sessions))
	for _, session := range sessions {
		if keep(session) {
			result = append(result, session)
		}
	}
	return result
}

func sortAGYSessions(sessions []store.AGYSession, primary basequery.SortTerm, usage map[string][]store.AGYUsageEvent) {
	sort.SliceStable(sessions, func(i, j int) bool {
		left, right := sessions[i], sessions[j]
		comparison := 0
		switch primary.Field {
		case "lastActivityAt":
			comparison = compareInt64(left.LastActivityAtMS, right.LastActivityAtMS)
		case "totalTokens":
			comparison = compareInt64(tokenTotal(usage[left.ExternalSessionID]), tokenTotal(usage[right.ExternalSessionID]))
		case "estimatedCost":
			comparison = compareInt64(estimatedTotal(usage[left.ExternalSessionID]), estimatedTotal(usage[right.ExternalSessionID]))
		default:
			comparison = strings.Compare(left.ExternalSessionID, right.ExternalSessionID)
		}
		if comparison == 0 {
			return left.ExternalSessionID > right.ExternalSessionID
		}
		if primary.Direction == basequery.SortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

type agyProjectGroup struct {
	key      string
	name     string
	source   string
	sessions []store.AGYSession
	events   []store.AGYUsageEvent
}

func projectGroups(sessions []store.AGYSession, events []store.AGYUsageEvent) []agyProjectGroup {
	usage := groupUsageBySession(events)
	groups := map[string]*agyProjectGroup{}
	for _, session := range sessions {
		group := groups[session.ProjectKey]
		if group == nil {
			group = &agyProjectGroup{
				key:    session.ProjectKey,
				name:   session.ProjectDisplayName,
				source: session.ProjectSource,
			}
			groups[session.ProjectKey] = group
		}
		if session.ProjectSource == "workspace" {
			group.source = "workspace"
		}
		group.sessions = append(group.sessions, session)
		group.events = append(group.events, usage[session.ExternalSessionID]...)
	}
	result := make([]agyProjectGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	return result
}

func sortAGYProjects(groups []agyProjectGroup, primary basequery.SortTerm) {
	sort.SliceStable(groups, func(i, j int) bool {
		left, right := groups[i], groups[j]
		comparison := 0
		switch primary.Field {
		case "lastActivityAt":
			comparison = compareInt64(projectLastActivity(left), projectLastActivity(right))
		case "totalTokens":
			comparison = compareInt64(tokenTotal(left.events), tokenTotal(right.events))
		case "displayName":
			comparison = strings.Compare(left.name, right.name)
		case "estimatedCost":
			comparison = compareInt64(estimatedTotal(left.events), estimatedTotal(right.events))
		default:
			comparison = strings.Compare(left.key, right.key)
		}
		if comparison == 0 {
			return left.key > right.key
		}
		if primary.Direction == basequery.SortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func projectItem(group agyProjectGroup) usagecost.ProjectItem {
	totals, _ := totalsForUsageEvents(group.events)
	conf, reason := projectAttributionMeta(group.source)
	return usagecost.ProjectItem{
		DimensionKey: group.key,
		Project: usagecost.AttributionValue{
			ID:          &group.key,
			DisplayName: &group.name,
			Confidence:  conf,
			Source:      "agy_local",
			Reason:      reason,
		},
		SessionCount: known(int64(len(group.sessions)), basequery.NumericCount),
		Trend:        []usagecost.ProjectDailyPoint{},
		Totals:       totals,
	}
}

func projectDaily(events []store.AGYUsageEvent, rangeValue basequery.UTCTimeRange) []usagecost.ProjectDailyPoint {
	trend := usageTrend(events, rangeValue, usagecost.TrendDay)
	result := make([]usagecost.ProjectDailyPoint, 0, len(trend))
	for _, point := range trend {
		result = append(result, usagecost.ProjectDailyPoint{
			BucketStartAt: point.StartAtMS,
			Confidence:    "high",
			Source:        "agy_step_metadata",
			Reason:        "step_usage",
			Totals:        point.Totals,
		})
	}
	return result
}

func projectLastActivity(group agyProjectGroup) int64 {
	var value int64
	for _, session := range group.sessions {
		value = maxInt64(value, session.LastActivityAtMS)
	}
	return value
}

func groupUsageBySession(events []store.AGYUsageEvent) map[string][]store.AGYUsageEvent {
	result := make(map[string][]store.AGYUsageEvent)
	for _, event := range events {
		result[event.ExternalSessionID] = append(result[event.ExternalSessionID], event)
	}
	return result
}

func sessionRange(session store.AGYSession, timezone string) basequery.UTCTimeRange {
	end := session.LastActivityAtMS + 1
	return basequery.UTCTimeRange{StartAtMS: session.CreatedAtMS, EndAtMS: end, TimeZone: timezone}
}

func sessionRequestCount(sessions []store.AGYSession) int64 {
	var count int64
	for _, session := range sessions {
		count += session.RequestCount
	}
	return count
}

func tokenTotal(events []store.AGYUsageEvent) int64 {
	var total int64
	for _, event := range events {
		if event.TotalKnown {
			total += event.TotalTokens
		} else {
			total += event.UncachedInputTokens + event.CacheReadTokens + event.OutputTokens
		}
	}
	return total
}

func estimatedTotal(events []store.AGYUsageEvent) int64 {
	totals, ok := totalsForUsageEvents(events)
	if !ok || totals.EstimatedUSDMicros.Value == nil {
		return 0
	}
	return *totals.EstimatedUSDMicros.Value
}

func page(request basequery.PageRequest, generation int64, fingerprint string) (int, int, error) {
	limit := request.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		return 0, 0, basequery.NewValidationFailure("page.limit", nil)
	}
	offset := 0
	if request.Cursor != nil {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(*request.Cursor)
		if decodeErr != nil {
			return 0, 0, basequery.NewValidationFailure("page.cursor", nil)
		}
		parts := strings.Split(string(decoded), ":")
		if len(parts) != 3 || parts[2] != fingerprint {
			return 0, 0, basequery.NewValidationFailure("page.cursor", nil)
		}
		parsedGeneration, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || parsedGeneration != generation {
			return 0, 0, basequery.NewValidationFailure("page.cursor", nil)
		}
		offset, err = strconv.Atoi(parts[1])
		if err != nil || offset < 0 {
			return 0, 0, basequery.NewValidationFailure("page.cursor", nil)
		}
	}
	return offset, limit, nil
}

func pageInfo(offset, limit, total int, generation int64, fingerprint string) *basequery.PageInfo {
	hasMore := offset+limit < total
	var next *string
	if hasMore {
		payload := strconv.FormatInt(generation, 10) + ":" + strconv.Itoa(offset+limit) + ":" + fingerprint
		value := base64.RawURLEncoding.EncodeToString([]byte(payload))
		next = &value
	}
	return &basequery.PageInfo{Limit: limit, HasMore: hasMore, NextCursor: next}
}

func queryFingerprint(request basequery.ValidatedRequest) string {
	var builder strings.Builder
	for _, term := range request.Sort {
		builder.WriteString(term.Field)
		builder.WriteByte('=')
		builder.WriteString(string(term.Direction))
		builder.WriteByte(';')
	}
	for _, filter := range request.Filters {
		builder.WriteString(filter.Field)
		builder.WriteByte('=')
		builder.WriteString(string(filter.Operator))
		builder.WriteByte(':')
		builder.WriteString(strings.Join(filter.Values, ","))
		builder.WriteByte(';')
	}
	if request.TimeRange != nil {
		builder.WriteString(strconv.FormatInt(request.TimeRange.StartAtMS, 10))
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(request.TimeRange.EndAtMS, 10))
		builder.WriteByte(':')
		builder.WriteString(request.TimeRange.TimeZone)
	}
	return digestString(builder.String())[:16]
}

func activityBoundaries(count, first, last int64) (basequery.NumericValue, basequery.NumericValue) {
	if count == 0 {
		return unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable),
			unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	}
	return known(first, basequery.NumericMilliseconds), known(last, basequery.NumericMilliseconds)
}

func known(value int64, unit basequery.NumericUnit) basequery.NumericValue {
	numeric, err := basequery.KnownNumeric(value, unit)
	if err != nil {
		return unknown(unit, basequery.UnknownUnavailable)
	}
	return numeric
}

func unknown(unit basequery.NumericUnit, reason basequery.UnknownReason) basequery.NumericValue {
	numeric, err := basequery.UnknownNumeric(unit, reason)
	if err != nil {
		return basequery.NumericValue{Unit: unit}
	}
	return numeric
}

func activityTokens(tokens int64, incomplete bool) basequery.NumericValue {
	if incomplete {
		return unknown(basequery.NumericTokens, basequery.UnknownUnavailable)
	}
	return known(tokens, basequery.NumericTokens)
}

func snapshotMeta(snapshot store.AGYSnapshot, page *basequery.PageInfo) basequery.ResponseMeta {
	for _, source := range snapshot.Sources {
		if source.State == "not_configured" {
			continue
		}
		if source.State != "available" || source.CoverageState != "exact" {
			return partialMeta(page)
		}
	}
	for _, session := range snapshot.Sessions {
		if session.CoverageState != "exact" {
			return partialMeta(page)
		}
	}
	return completeMeta(page)
}

func completeMeta(page *basequery.PageInfo) basequery.ResponseMeta {
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, page, nil)
	if err != nil {
		return basequery.ResponseMeta{Status: basequery.ResponseComplete}
	}
	return meta
}

func partialMeta(page *basequery.PageInfo) basequery.ResponseMeta {
	meta, err := basequery.NewResponseMeta(basequery.ResponsePartial, page, []basequery.ErrorCode{basequery.ErrorPartial})
	if err != nil {
		return basequery.ResponseMeta{Status: basequery.ResponsePartial}
	}
	return meta
}

func slicePage[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func minPositive(current, candidate int64) int64 {
	if current == 0 || (candidate > 0 && candidate < current) {
		return candidate
	}
	return current
}

func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func duplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func hasEstimatedCost(events []store.AGYUsageEvent) bool {
	for _, event := range events {
		if _, ok := estimateEvent(event); ok {
			return true
		}
	}
	return false
}

func estimateEvent(event store.AGYUsageEvent) (int64, bool) {
	cost, _, ok := priceEvent(event)
	return cost, ok
}

func priceEvent(event store.AGYUsageEvent) (int64, pricing.AGYRate, bool) {
	if event.ModelKey == nil || !event.CacheReadKnown {
		return 0, pricing.AGYRate{}, false
	}
	at := event.OccurredAtMS
	if event.StartedAtMS != nil && *event.StartedAtMS > 0 {
		at = *event.StartedAtMS
	}
	rate, ok := pricing.AGYRateAt(*event.ModelKey, at)
	if !ok {
		return 0, pricing.AGYRate{}, false
	}
	cost, ok := pricing.EstimateAGYCost(rate, event.UncachedInputTokens, event.CacheReadTokens, event.OutputTokens)
	return cost, rate, ok
}

func pricingMetadata(events []store.AGYUsageEvent) (*string, []string) {
	versions := map[string]bool{}
	for _, event := range events {
		if _, rate, ok := priceEvent(event); ok {
			versions[rate.PricingVersion()] = true
		}
	}
	result := make([]string, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, result
	}
	source := pricing.AGYPriceSource
	return &source, result
}

func fallbackSessionTitle(at int64) string {
	return "未命名会话 " + time.UnixMilli(at).Format("01-02 15:04")
}
