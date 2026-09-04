package dashboardsummary

import (
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

const (
	ContractVersion = "dashboard-summary-v2"
	modelLimit      = 10
	maxRangeDays    = 366
)

var ErrInvalidService = errors.New("dashboard summary query service is invalid")

type CoverageState string

const (
	CoverageComplete    CoverageState = "complete"
	CoveragePartial     CoverageState = "partial"
	CoverageEmpty       CoverageState = "empty"
	CoverageUnavailable CoverageState = "unavailable"
	CoverageUnknown     CoverageState = "unknown"
)

type Request struct {
	Range         basequery.LocalDateRange   `json:"range"`
	ActivityRange basequery.LocalDateRange   `json:"activityRange"`
	ExactRange    *basequery.UTCTimeRange    `json:"exactRange"`
	Granularity   usagecost.TrendGranularity `json:"granularity"`
	EvaluatedAtMS *int64                     `json:"evaluatedAtMs"`
}

type Coverage struct {
	KnownProviderCount     int32         `json:"knownProviderCount"`
	KnownCostProviderCount int32         `json:"knownCostProviderCount"`
	TotalProviderCount     int32         `json:"totalProviderCount"`
	TokenState             CoverageState `json:"tokenState"`
	CostState              CoverageState `json:"costState"`
	OverallState           CoverageState `json:"overallState"`
}

type Totals struct {
	TotalTokens         basequery.NumericValue `json:"totalTokens"`
	EstimatedUSDMicros  basequery.NumericValue `json:"estimatedUsdMicros"`
	ActiveProviderCount basequery.NumericValue `json:"activeProviderCount"`
}

type ProviderSlice struct {
	Provider           string                     `json:"provider"`
	ProviderContext    agentprovider.Context      `json:"providerContext"`
	CoverageState      CoverageState              `json:"coverageState"`
	Totals             usagecost.UsageTotals      `json:"totals"`
	Trend              []usagecost.TrendPoint     `json:"trend"`
	Models             []usagecost.UsageModelItem `json:"models"`
	ReportedUSDMicros  *basequery.NumericValue    `json:"reportedUsdMicros"`
	ReportedCostSource *string                    `json:"reportedCostSource"`
	DataAsOfMS         *basequery.NumericValue    `json:"dataAsOfMs"`
	DegradedReason     *usagecost.DegradedReason  `json:"degradedReason"`
	CostState          CoverageState              `json:"costState"`
	ModelCount         int32                      `json:"modelCount"`
}

type ActivityProviderCoverage struct {
	Provider      string        `json:"provider"`
	CoverageState CoverageState `json:"coverageState"`
}

type ProviderShare struct {
	Provider           string                 `json:"provider"`
	TotalTokens        basequery.NumericValue `json:"totalTokens"`
	EstimatedUSDMicros basequery.NumericValue `json:"estimatedUsdMicros"`
}

type TrendPoint struct {
	Key                string                 `json:"key"`
	StartAtMS          basequery.NumericValue `json:"startAtMs"`
	EndAtMS            basequery.NumericValue `json:"endAtMs"`
	TotalTokens        basequery.NumericValue `json:"totalTokens"`
	EstimatedUSDMicros basequery.NumericValue `json:"estimatedUsdMicros"`
	Shares             []ProviderShare        `json:"shares"`
}

type ModelItem struct {
	Provider     string                     `json:"provider"`
	DimensionKey string                     `json:"dimensionKey"`
	Model        usagecost.AttributionValue `json:"model"`
	Totals       usagecost.UsageTotals      `json:"totals"`
}

type QuotaCard struct {
	Provider        string                     `json:"provider"`
	ProviderContext agentprovider.Context      `json:"providerContext"`
	CoverageState   CoverageState              `json:"coverageState"`
	Meta            basequery.ResponseMeta     `json:"meta"`
	Current         quotaquery.CurrentResponse `json:"current"`
}

type Response struct {
	Meta                     basequery.ResponseMeta     `json:"meta"`
	Range                    basequery.UTCTimeRange     `json:"range"`
	ReportingTimeZone        string                     `json:"reportingTimeZone"`
	Coverage                 Coverage                   `json:"coverage"`
	Totals                   Totals                     `json:"totals"`
	Providers                []ProviderSlice            `json:"providers"`
	Trend                    []TrendPoint               `json:"trend"`
	Distribution             []ProviderShare            `json:"distribution"`
	Models                   []ModelItem                `json:"models"`
	Quotas                   []QuotaCard                `json:"quotas"`
	UsageGeneration          uint64                     `json:"usageGeneration"`
	QuotaGeneration          uint64                     `json:"quotaGeneration"`
	ActivityRange            basequery.UTCTimeRange     `json:"activityRange"`
	ActivityCoverageState    CoverageState              `json:"activityCoverageState"`
	Activity                 []TrendPoint               `json:"activity"`
	ActivityProviderCoverage []ActivityProviderCoverage `json:"activityProviderCoverage"`
}
