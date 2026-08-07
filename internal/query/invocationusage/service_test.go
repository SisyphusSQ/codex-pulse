package invocationusage

import (
	"context"
	"errors"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

func TestInvocationUsageAggregatesTruthfulToolAndSkillMetrics(t *testing.T) {
	t.Parallel()

	start := invocationTime(t, "2026-08-07T01:00:00Z")
	end := invocationTime(t, "2026-08-07T03:00:00Z")
	duration42, duration58 := int64(42), int64(58)
	reader := invocationReaderFunc(func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error) {
		return []storelight.LightInvocationEvent{
			{SessionID: "a", ObservedAtMS: start + 5*60_000, Kind: "tool", Name: "exec", Source: "response_custom", Outcome: "unknown"},
			{SessionID: "a", ObservedAtMS: start + 6*60_000, Kind: "tool", Name: "exec_command", Source: "exec_nested", Outcome: "unknown"},
			{SessionID: "b", ObservedAtMS: start + 10*60_000, Kind: "tool", Name: "linear.list_issues", Source: "mcp", Outcome: "succeeded", DurationMS: &duration42},
			{SessionID: "a", ObservedAtMS: start + 30*60_000, Kind: "skill", Name: "go-code-style", Source: "skill_explicit", Outcome: "unknown"},
			{SessionID: "b", ObservedAtMS: start + 40*60_000, Kind: "skill", Name: "go-code-style", Source: "skill_file_loaded", Outcome: "unknown"},
			{SessionID: "b", ObservedAtMS: start + 70*60_000, Kind: "tool", Name: "linear.list_issues", Source: "mcp", Outcome: "failed", DurationMS: &duration58},
			{SessionID: "b", ObservedAtMS: start + 80*60_000, Kind: "skill", Name: "discuss-first", Source: "skill_file_loaded", Outcome: "unknown"},
		}, nil
	})
	service, err := NewService(reader)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	response, err := service.InvocationUsage(context.Background(), InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "UTC"},
		Granularity: GranularityHour,
		SourceClass: SourceClassAll,
		TopLimit:    20,
	})
	if err != nil {
		t.Fatalf("InvocationUsage() error = %v", err)
	}
	assertInvocationNumeric(t, response.Totals.ToolCallCount, 4)
	assertInvocationNumeric(t, response.Totals.DistinctToolCount, 3)
	assertInvocationNumeric(t, response.Totals.SkillActivityCount, 3)
	assertInvocationNumeric(t, response.Totals.DistinctSkillCount, 2)
	assertInvocationNumeric(t, response.Totals.ToolFailureCount, 1)
	assertInvocationNumeric(t, response.Totals.SessionCount, 2)
	assertInvocationNumeric(t, response.Coverage.StructuredEventCount, 4)
	assertInvocationNumeric(t, response.Coverage.DetectedEventCount, 3)
	if response.Meta.Status != basequery.ResponseComplete || len(response.Trend) != 2 ||
		len(response.Tools) != 3 || len(response.Skills) != 2 {
		t.Fatalf("response shape = %#v", response)
	}
	assertInvocationNumeric(t, response.Trend[0].ToolCallCount, 3)
	assertInvocationNumeric(t, response.Trend[0].SkillActivityCount, 2)
	if response.Tools[0].Name != "linear.list_issues" {
		t.Fatalf("top tool = %#v", response.Tools[0])
	}
	assertInvocationNumeric(t, response.Tools[0].CallCount, 2)
	assertInvocationNumeric(t, response.Tools[0].SucceededCount, 1)
	assertInvocationNumeric(t, response.Tools[0].FailedCount, 1)
	assertInvocationNumericUnit(t, response.Tools[0].AverageDurationMS, 50, basequery.NumericMilliseconds)
	if response.Skills[0].Name != "go-code-style" {
		t.Fatalf("top skill = %#v", response.Skills[0])
	}
	assertInvocationNumeric(t, response.Skills[0].ActivityCount, 2)
	assertInvocationNumeric(t, response.Skills[0].ExplicitCount, 1)
	assertInvocationNumeric(t, response.Skills[0].FileLoadedCount, 1)
}

func TestInvocationUsageFiltersDetectedSourcesAndMapsReaderFailure(t *testing.T) {
	t.Parallel()

	start := invocationTime(t, "2026-08-07T01:00:00Z")
	end := invocationTime(t, "2026-08-07T03:00:00Z")
	reader := invocationReaderFunc(func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error) {
		return []storelight.LightInvocationEvent{
			{SessionID: "a", ObservedAtMS: start, Kind: "tool", Name: "exec", Source: "response_custom", Outcome: "unknown"},
			{SessionID: "a", ObservedAtMS: start, Kind: "tool", Name: "exec_command", Source: "exec_nested", Outcome: "unknown"},
			{SessionID: "a", ObservedAtMS: start, Kind: "skill", Name: "go-code-style", Source: "skill_explicit", Outcome: "unknown"},
			{SessionID: "a", ObservedAtMS: start, Kind: "skill", Name: "go-code-style", Source: "skill_file_loaded", Outcome: "unknown"},
		}, nil
	})
	service, err := NewService(reader)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.InvocationUsage(context.Background(), InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "UTC"},
		Granularity: GranularityHour, SourceClass: SourceClassDetected, TopLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertInvocationNumeric(t, response.Totals.ToolCallCount, 1)
	assertInvocationNumeric(t, response.Totals.SkillActivityCount, 1)

	want := errors.New("database unavailable")
	failedService, err := NewService(invocationReaderFunc(func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error) {
		return nil, want
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = failedService.InvocationUsage(context.Background(), InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "UTC"},
		Granularity: GranularityHour, SourceClass: SourceClassAll, TopLimit: 20,
	})
	if !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("reader failure = %v, want ErrUnavailable", err)
	}
}

func TestInvocationUsageRejectsCorruptStoredKindSourcePair(t *testing.T) {
	t.Parallel()

	start := invocationTime(t, "2026-08-07T01:00:00Z")
	end := invocationTime(t, "2026-08-07T02:00:00Z")
	service, err := NewService(invocationReaderFunc(func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error) {
		return []storelight.LightInvocationEvent{{
			SessionID: "a", ObservedAtMS: start, Kind: "skill", Name: "go-code-style",
			Source: "mcp", Outcome: "unknown",
		}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.InvocationUsage(context.Background(), InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "UTC"},
		Granularity: GranularityHour, SourceClass: SourceClassAll, TopLimit: 20,
	})
	if !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("corrupt kind/source pair error = %v, want ErrUnavailable", err)
	}
}

func TestInvocationUsageDistinguishesRepeatedDSTHour(t *testing.T) {
	t.Parallel()

	start := invocationTime(t, "2026-11-01T05:00:00Z")
	end := invocationTime(t, "2026-11-01T07:00:00Z")
	service, err := NewService(invocationReaderFunc(func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error) {
		return []storelight.LightInvocationEvent{
			{SessionID: "a", ObservedAtMS: invocationTime(t, "2026-11-01T05:10:00Z"), Kind: "tool", Name: "exec", Source: "response_custom", Outcome: "unknown"},
			{SessionID: "a", ObservedAtMS: invocationTime(t, "2026-11-01T06:10:00Z"), Kind: "tool", Name: "exec", Source: "response_custom", Outcome: "unknown"},
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.InvocationUsage(context.Background(), InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "America/New_York"},
		Granularity: GranularityHour, SourceClass: SourceClassAll, TopLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Trend) != 2 {
		t.Fatalf("trend = %#v, want two repeated-hour buckets", response.Trend)
	}
	wantKeys := []string{"2026-11-01T01:00-04:00", "2026-11-01T01:00-05:00"}
	for index, point := range response.Trend {
		if point.Key != wantKeys[index] {
			t.Errorf("trend[%d].Key = %q, want %q", index, point.Key, wantKeys[index])
		}
		assertInvocationNumeric(t, point.ToolCallCount, 1)
	}
}

type invocationReaderFunc func(context.Context, int64, int64) ([]storelight.LightInvocationEvent, error)

func (function invocationReaderFunc) ListLightInvocations(
	ctx context.Context,
	startAtMS int64,
	endAtMS int64,
) ([]storelight.LightInvocationEvent, error) {
	return function(ctx, startAtMS, endAtMS)
}

func invocationTime(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.UnixMilli()
}

func assertInvocationNumeric(t *testing.T, value basequery.NumericValue, want int64) {
	t.Helper()
	assertInvocationNumericUnit(t, value, want, basequery.NumericCount)
}

func assertInvocationNumericUnit(
	t *testing.T,
	value basequery.NumericValue,
	want int64,
	unit basequery.NumericUnit,
) {
	t.Helper()
	if value.Value == nil || *value.Value != want || value.Unit != unit {
		t.Fatalf("numeric = %#v, want %d %s", value, want, unit)
	}
}
