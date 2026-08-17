package invocationusage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

const invocationMaxRangeDays = 90

type Reader interface {
	ListLightInvocations(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error)
}

type Service struct {
	reader Reader
}

func NewService(reader Reader) (*Service, error) {
	if reader == nil {
		return nil, ErrInvalidService
	}
	return &Service{reader: reader}, nil
}

func (service *Service) InvocationUsage(
	ctx context.Context,
	request InvocationUsageRequest,
) (InvocationUsageResponse, error) {
	if service == nil || service.reader == nil {
		return InvocationUsageResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return InvocationUsageResponse{}, err
	}
	location, err := validateRequest(request)
	if err != nil {
		return InvocationUsageResponse{}, err
	}
	events, err := service.reader.ListLightInvocations(ctx, request.Range.StartAtMS, request.Range.EndAtMS)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return InvocationUsageResponse{}, err
		}
		return InvocationUsageResponse{}, basequery.NewUnavailableFailure(err)
	}
	filtered := make([]storelight.LightInvocationEvent, 0, len(events))
	for _, event := range events {
		if err := validateStoredEvent(event, request.Range); err != nil {
			return InvocationUsageResponse{}, basequery.NewUnavailableFailure(err)
		}
		if matchesSourceClass(event.Source, request.SourceClass) {
			filtered = append(filtered, event)
		}
	}
	return aggregateResponse(request, location, filtered)
}

func validateRequest(request InvocationUsageRequest) (*time.Location, error) {
	if request.Granularity != GranularityHour && request.Granularity != GranularityDay {
		return nil, fmt.Errorf("%w: invocation granularity", basequery.ErrValidation)
	}
	if request.SourceClass != SourceClassAll && request.SourceClass != SourceClassStructured &&
		request.SourceClass != SourceClassDetected {
		return nil, fmt.Errorf("%w: invocation source class", basequery.ErrValidation)
	}
	if request.TopLimit < 1 || request.TopLimit > 50 || request.Range.TimeZone == "" ||
		request.Range.TimeZone == "Local" || request.Range.StartAtMS < 0 ||
		request.Range.EndAtMS <= request.Range.StartAtMS ||
		request.Range.EndAtMS > basequery.JavaScriptMaxSafeInteger {
		return nil, fmt.Errorf("%w: invocation range", basequery.ErrValidation)
	}
	location, err := time.LoadLocation(request.Range.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("%w: invocation timezone", basequery.ErrValidation)
	}
	if request.Range.EndAtMS-request.Range.StartAtMS > int64(invocationMaxRangeDays)*int64(24*time.Hour/time.Millisecond) {
		return nil, fmt.Errorf("%w: invocation range", basequery.ErrValidation)
	}
	return location, nil
}

func validateStoredEvent(event storelight.LightInvocationEvent, rangeValue basequery.UTCTimeRange) error {
	if event.SessionID == "" || event.ObservedAtMS < rangeValue.StartAtMS || event.ObservedAtMS >= rangeValue.EndAtMS ||
		event.Name == "" || len(event.Name) > 128 || strings.TrimSpace(event.Name) != event.Name ||
		(event.Kind != "tool" && event.Kind != "skill") ||
		(event.Outcome != "unknown" && event.Outcome != "succeeded" && event.Outcome != "failed") ||
		(event.DurationMS != nil && (*event.DurationMS < 0 || *event.DurationMS > basequery.JavaScriptMaxSafeInteger)) {
		return errors.New("stored invocation event is invalid")
	}
	if !matchesSourceClass(event.Source, SourceClassAll) {
		return errors.New("stored invocation source is invalid")
	}
	if (event.Kind == "tool" && !isToolSource(event.Source)) ||
		(event.Kind == "skill" && !isSkillSource(event.Source)) {
		return errors.New("stored invocation kind/source pair is invalid")
	}
	return nil
}

func isToolSource(source string) bool {
	switch source {
	case "response_function", "response_custom", "exec_nested", "mcp", "web_search", "image_generation":
		return true
	default:
		return false
	}
}

func isSkillSource(source string) bool {
	return source == "skill_explicit" || source == "skill_file_loaded"
}

func matchesSourceClass(source string, class SourceClass) bool {
	structured := false
	detected := false
	switch source {
	case "response_function", "response_custom", "mcp", "web_search", "image_generation", "skill_explicit":
		structured = true
	case "exec_nested", "skill_file_loaded":
		detected = true
	default:
		return false
	}
	switch class {
	case SourceClassAll:
		return structured || detected
	case SourceClassStructured:
		return structured
	case SourceClassDetected:
		return detected
	default:
		return false
	}
}

type toolAggregate struct {
	name                              string
	count, succeeded, failed, unknown int64
	durationTotal, durationCount      int64
	lastSeen                          int64
	sessions                          map[string]struct{}
	sources                           map[string]struct{}
}

type skillAggregate struct {
	name                        string
	count, explicit, fileLoaded int64
	lastSeen                    int64
	sessions                    map[string]struct{}
}

type trendAggregate struct {
	tool, skill int64
}

func aggregateResponse(
	request InvocationUsageRequest,
	location *time.Location,
	events []storelight.LightInvocationEvent,
) (InvocationUsageResponse, error) {
	tools := make(map[string]*toolAggregate)
	skills := make(map[string]*skillAggregate)
	sessions := make(map[string]struct{})
	trendCounts := make(map[int64]trendAggregate)
	var toolCount, skillCount, failureCount, structuredCount, detectedCount int64
	for _, event := range events {
		sessions[event.SessionID] = struct{}{}
		if matchesSourceClass(event.Source, SourceClassStructured) {
			structuredCount++
		} else {
			detectedCount++
		}
		bucket := bucketStart(time.UnixMilli(event.ObservedAtMS), location, request.Granularity).UnixMilli()
		trend := trendCounts[bucket]
		if event.Kind == "tool" {
			toolCount++
			trend.tool++
			item := tools[event.Name]
			if item == nil {
				item = &toolAggregate{name: event.Name, sessions: make(map[string]struct{}), sources: make(map[string]struct{})}
				tools[event.Name] = item
			}
			item.count++
			item.sessions[event.SessionID] = struct{}{}
			item.sources[event.Source] = struct{}{}
			item.lastSeen = max(item.lastSeen, event.ObservedAtMS)
			switch event.Outcome {
			case "succeeded":
				item.succeeded++
			case "failed":
				item.failed++
				failureCount++
			default:
				item.unknown++
			}
			if event.DurationMS != nil {
				item.durationTotal += *event.DurationMS
				item.durationCount++
			}
		} else {
			skillCount++
			trend.skill++
			item := skills[event.Name]
			if item == nil {
				item = &skillAggregate{name: event.Name, sessions: make(map[string]struct{})}
				skills[event.Name] = item
			}
			item.count++
			item.sessions[event.SessionID] = struct{}{}
			item.lastSeen = max(item.lastSeen, event.ObservedAtMS)
			if event.Source == "skill_explicit" {
				item.explicit++
			} else {
				item.fileLoaded++
			}
		}
		trendCounts[bucket] = trend
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return InvocationUsageResponse{}, err
	}
	response := InvocationUsageResponse{
		Meta: meta, Range: request.Range, Granularity: request.Granularity, SourceClass: request.SourceClass,
		Totals: InvocationTotals{
			ToolCallCount: knownCount(toolCount), DistinctToolCount: knownCount(int64(len(tools))),
			SkillActivityCount: knownCount(skillCount), DistinctSkillCount: knownCount(int64(len(skills))),
			ToolFailureCount: knownCount(failureCount), SessionCount: knownCount(int64(len(sessions))),
			AIEditCount: unknownCount(),
		},
		Trend: buildTrend(request, location, trendCounts),
		Tools: buildToolItems(tools, request.TopLimit), Skills: buildSkillItems(skills, request.TopLimit),
		Coverage: InvocationCoverage{
			StructuredEventCount: knownCount(structuredCount), DetectedEventCount: knownCount(detectedCount),
		},
	}
	return response, nil
}

func buildTrend(
	request InvocationUsageRequest,
	location *time.Location,
	counts map[int64]trendAggregate,
) []InvocationTrendPoint {
	start := bucketStart(time.UnixMilli(request.Range.StartAtMS), location, request.Granularity)
	end := time.UnixMilli(request.Range.EndAtMS)
	result := make([]InvocationTrendPoint, 0)
	for cursor := start; cursor.Before(end); cursor = nextBucket(cursor, request.Granularity) {
		next := nextBucket(cursor, request.Granularity)
		pointStart := max(cursor.UnixMilli(), request.Range.StartAtMS)
		pointEnd := min(next.UnixMilli(), request.Range.EndAtMS)
		count := counts[cursor.UnixMilli()]
		key := cursor.In(location).Format("2006-01-02")
		if request.Granularity == GranularityHour {
			key = cursor.In(location).Format("2006-01-02T15:00Z07:00")
		}
		result = append(result, InvocationTrendPoint{
			Key: key, StartAtMS: knownMilliseconds(pointStart), EndAtMS: knownMilliseconds(pointEnd),
			ToolCallCount: knownCount(count.tool), SkillActivityCount: knownCount(count.skill),
		})
	}
	return result
}

func bucketStart(value time.Time, location *time.Location, granularity Granularity) time.Time {
	local := value.In(location)
	if granularity == GranularityHour {
		_, offsetSeconds := local.Zone()
		return time.Date(
			local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0,
			time.FixedZone("", offsetSeconds),
		)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func nextBucket(value time.Time, granularity Granularity) time.Time {
	if granularity == GranularityHour {
		return value.Add(time.Hour)
	}
	return value.AddDate(0, 0, 1)
}

func buildToolItems(values map[string]*toolAggregate, limit int) []ToolUsageItem {
	ordered := make([]*toolAggregate, 0, len(values))
	for _, value := range values {
		ordered = append(ordered, value)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].count == ordered[right].count {
			return ordered[left].name < ordered[right].name
		}
		return ordered[left].count > ordered[right].count
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	result := make([]ToolUsageItem, 0, len(ordered))
	for _, value := range ordered {
		sources := make([]string, 0, len(value.sources))
		for source := range value.sources {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		average, _ := basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
		if value.durationCount > 0 {
			average = knownMilliseconds(value.durationTotal / value.durationCount)
		}
		result = append(result, ToolUsageItem{
			Name: value.name, CallCount: knownCount(value.count), SessionCount: knownCount(int64(len(value.sessions))),
			SucceededCount: knownCount(value.succeeded), FailedCount: knownCount(value.failed),
			UnknownCount: knownCount(value.unknown), AverageDurationMS: average,
			LastSeenAtMS: knownMilliseconds(value.lastSeen), Sources: sources,
		})
	}
	return result
}

func buildSkillItems(values map[string]*skillAggregate, limit int) []SkillUsageItem {
	ordered := make([]*skillAggregate, 0, len(values))
	for _, value := range values {
		ordered = append(ordered, value)
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].count == ordered[right].count {
			return ordered[left].name < ordered[right].name
		}
		return ordered[left].count > ordered[right].count
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	result := make([]SkillUsageItem, 0, len(ordered))
	for _, value := range ordered {
		result = append(result, SkillUsageItem{
			Name: value.name, ActivityCount: knownCount(value.count), SessionCount: knownCount(int64(len(value.sessions))),
			ExplicitCount: knownCount(value.explicit), FileLoadedCount: knownCount(value.fileLoaded),
			LastSeenAtMS: knownMilliseconds(value.lastSeen),
		})
	}
	return result
}

func knownCount(value int64) basequery.NumericValue {
	numeric, _ := basequery.KnownNumeric(value, basequery.NumericCount)
	return numeric
}

func unknownCount() basequery.NumericValue {
	numeric, _ := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownUnavailable)
	return numeric
}

func knownMilliseconds(value int64) basequery.NumericValue {
	numeric, _ := basequery.KnownNumeric(value, basequery.NumericMilliseconds)
	return numeric
}
