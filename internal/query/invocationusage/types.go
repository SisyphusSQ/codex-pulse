package invocationusage

import (
	"errors"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

const ContractVersion = "invocation-usage-v1"

var ErrInvalidService = errors.New("invocation usage query service is invalid")

type Granularity string

const (
	GranularityHour Granularity = "hour"
	GranularityDay  Granularity = "day"
)

type SourceClass string

const (
	SourceClassAll        SourceClass = "all"
	SourceClassStructured SourceClass = "structured"
	SourceClassDetected   SourceClass = "detected"
)

type InvocationUsageRequest struct {
	Range       basequery.UTCTimeRange `json:"range"`
	Granularity Granularity            `json:"granularity"`
	SourceClass SourceClass            `json:"sourceClass"`
	TopLimit    int                    `json:"topLimit"`
}

type InvocationTotals struct {
	ToolCallCount      basequery.NumericValue `json:"toolCallCount"`
	DistinctToolCount  basequery.NumericValue `json:"distinctToolCount"`
	SkillActivityCount basequery.NumericValue `json:"skillActivityCount"`
	DistinctSkillCount basequery.NumericValue `json:"distinctSkillCount"`
	ToolFailureCount   basequery.NumericValue `json:"toolFailureCount"`
	SessionCount       basequery.NumericValue `json:"sessionCount"`
}

type InvocationTrendPoint struct {
	Key                string                 `json:"key"`
	StartAtMS          basequery.NumericValue `json:"startAtMs"`
	EndAtMS            basequery.NumericValue `json:"endAtMs"`
	ToolCallCount      basequery.NumericValue `json:"toolCallCount"`
	SkillActivityCount basequery.NumericValue `json:"skillActivityCount"`
}

type ToolUsageItem struct {
	Name              string                 `json:"name"`
	CallCount         basequery.NumericValue `json:"callCount"`
	SessionCount      basequery.NumericValue `json:"sessionCount"`
	SucceededCount    basequery.NumericValue `json:"succeededCount"`
	FailedCount       basequery.NumericValue `json:"failedCount"`
	UnknownCount      basequery.NumericValue `json:"unknownCount"`
	AverageDurationMS basequery.NumericValue `json:"averageDurationMs"`
	LastSeenAtMS      basequery.NumericValue `json:"lastSeenAtMs"`
	Sources           []string               `json:"sources"`
}

type SkillUsageItem struct {
	Name            string                 `json:"name"`
	ActivityCount   basequery.NumericValue `json:"activityCount"`
	SessionCount    basequery.NumericValue `json:"sessionCount"`
	ExplicitCount   basequery.NumericValue `json:"explicitCount"`
	FileLoadedCount basequery.NumericValue `json:"fileLoadedCount"`
	LastSeenAtMS    basequery.NumericValue `json:"lastSeenAtMs"`
}

type InvocationCoverage struct {
	StructuredEventCount basequery.NumericValue `json:"structuredEventCount"`
	DetectedEventCount   basequery.NumericValue `json:"detectedEventCount"`
}

type InvocationUsageResponse struct {
	Meta        basequery.ResponseMeta `json:"meta"`
	Range       basequery.UTCTimeRange `json:"range"`
	Granularity Granularity            `json:"granularity"`
	SourceClass SourceClass            `json:"sourceClass"`
	Totals      InvocationTotals       `json:"totals"`
	Trend       []InvocationTrendPoint `json:"trend"`
	Tools       []ToolUsageItem        `json:"tools"`
	Skills      []SkillUsageItem       `json:"skills"`
	Coverage    InvocationCoverage     `json:"coverage"`
}
