package dashboardsummary

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

var summaryProviders = []string{agentprovider.Codex, agentprovider.Cursor, agentprovider.Grok}

type UsageQuery interface {
	UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error)
}

type QuotaQuery interface {
	QuotaCurrent(context.Context, agentprovider.Scope, int64) (runtimeinfo.QuotaCurrentResponse, error)
}

type usageCacheKey struct {
	provider        string
	startDate       string
	endDate         string
	timeZone        string
	granularity     usagecost.TrendGranularity
	exact           bool
	startAtMS       int64
	endAtMS         int64
	tokenTotalsOnly bool
}

type cachedUsage struct {
	generation uint64
	response   usagecost.UsageCostResponse
}

type cachedQuota struct {
	generation uint64
	evaluated  int64
	response   runtimeinfo.QuotaCurrentResponse
}

type Service struct {
	usage UsageQuery
	quota QuotaQuery
	now   func() time.Time

	mu         sync.Mutex
	usageGen   uint64
	quotaGen   uint64
	usageCache map[usageCacheKey]cachedUsage
	quotaCache map[string]cachedQuota
}

func New(usage UsageQuery, quota QuotaQuery, now func() time.Time) (*Service, error) {
	if usage == nil || quota == nil {
		return nil, ErrInvalidService
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		usage: usage, quota: quota, now: now,
		usageCache: make(map[usageCacheKey]cachedUsage),
		quotaCache: make(map[string]cachedQuota),
	}, nil
}

func (service *Service) InvalidateUsage() {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.usageGen++
	service.usageCache = make(map[usageCacheKey]cachedUsage)
	service.mu.Unlock()
}

func (service *Service) InvalidateUsageAndQuota() {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.usageGen++
	service.quotaGen++
	service.usageCache = make(map[usageCacheKey]cachedUsage)
	service.quotaCache = make(map[string]cachedQuota)
	service.mu.Unlock()
}

func (service *Service) DashboardSummary(ctx context.Context, request Request) (Response, error) {
	if service == nil || service.usage == nil || service.quota == nil {
		return Response{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if !validGranularity(request.Granularity) {
		return Response{}, fmt.Errorf("%w: granularity", basequery.ErrValidation)
	}
	rangeValue, err := validateSummaryRange(request)
	if err != nil {
		return Response{}, err
	}
	activityRange, err := basequery.NormalizeLocalDateRange(request.ActivityRange, maxRangeDays)
	if err != nil {
		return Response{}, err
	}
	if activityRange.TimeZone != rangeValue.TimeZone {
		return Response{}, fmt.Errorf("%w: activityRange.timeZone", basequery.ErrValidation)
	}
	evaluatedAt := service.now().UnixMilli()
	if request.EvaluatedAtMS != nil {
		evaluatedAt = *request.EvaluatedAtMS
	}
	if evaluatedAt < 0 || evaluatedAt > basequery.JavaScriptMaxSafeInteger {
		return Response{}, basequery.NewValidationFailure("evaluatedAtMS", nil)
	}

	usageGen, quotaGen := service.generations()
	fetched := make([]providerFetch, len(summaryProviders))
	var wait sync.WaitGroup
	for index, provider := range summaryProviders {
		wait.Add(1)
		go func(index int, provider string) {
			defer wait.Done()
			fetched[index].provider = provider
			fetched[index].usage, fetched[index].usageErr = service.loadUsage(
				ctx,
				usagecost.UsageCostRequest{
					Provider: agentprovider.Scope{Provider: provider},
					Range:    request.Range, ExactRange: cloneRange(request.ExactRange),
					Granularity: request.Granularity,
				},
				*rangeValue,
				usageGen,
			)
			fetched[index].activity, fetched[index].activityErr = service.loadUsage(
				ctx,
				usagecost.UsageCostRequest{
					Provider: agentprovider.Scope{Provider: provider},
					Range:    request.ActivityRange, Granularity: usagecost.TrendDay,
					TokenTotalsOnly: true,
				},
				*activityRange,
				usageGen,
			)
			fetched[index].quota, fetched[index].quotaErr = service.loadQuota(
				ctx, provider, evaluatedAt, quotaGen,
			)
		}(index, provider)
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	return assembleSummary(*rangeValue, *activityRange, usageGen, quotaGen, fetched)
}

type providerFetch struct {
	provider    string
	usage       usagecost.UsageCostResponse
	usageErr    error
	activity    usagecost.UsageCostResponse
	activityErr error
	quota       runtimeinfo.QuotaCurrentResponse
	quotaErr    error
}

func (service *Service) generations() (uint64, uint64) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.usageGen, service.quotaGen
}

func (service *Service) loadUsage(
	ctx context.Context,
	request usagecost.UsageCostRequest,
	expectedRange basequery.UTCTimeRange,
	generation uint64,
) (usagecost.UsageCostResponse, error) {
	provider := request.Provider.Provider
	key := usageKey(request)
	if cached, ok := service.lookupUsage(key, generation); ok {
		return cached, nil
	}
	response, err := service.usage.UsageCost(ctx, request)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	if err := validateUsageResponse(provider, expectedRange, response); err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	service.storeUsage(key, generation, response)
	return response, nil
}

func (service *Service) loadQuota(
	ctx context.Context,
	provider string,
	evaluatedAt int64,
	generation uint64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	if cached, ok := service.lookupQuota(provider, evaluatedAt, generation); ok {
		return cached, nil
	}
	response, err := service.quota.QuotaCurrent(ctx, agentprovider.Scope{Provider: provider}, evaluatedAt)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	if err := validateQuotaResponse(provider, response); err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	service.storeQuota(provider, evaluatedAt, generation, response)
	return response, nil
}

func (service *Service) lookupUsage(key usageCacheKey, generation uint64) (usagecost.UsageCostResponse, bool) {
	service.mu.Lock()
	defer service.mu.Unlock()
	cached, ok := service.usageCache[key]
	if !ok || cached.generation != generation {
		return usagecost.UsageCostResponse{}, false
	}
	return cached.response, true
}

func (service *Service) storeUsage(key usageCacheKey, generation uint64, response usagecost.UsageCostResponse) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.usageGen != generation {
		return
	}
	if existing, ok := service.usageCache[key]; ok && existing.generation > generation {
		return
	}
	service.usageCache[key] = cachedUsage{generation: generation, response: response}
}

func (service *Service) lookupQuota(
	provider string,
	evaluatedAt int64,
	generation uint64,
) (runtimeinfo.QuotaCurrentResponse, bool) {
	service.mu.Lock()
	defer service.mu.Unlock()
	cached, ok := service.quotaCache[provider]
	if !ok || cached.generation != generation || cached.evaluated != evaluatedAt {
		return runtimeinfo.QuotaCurrentResponse{}, false
	}
	return cached.response, true
}

func (service *Service) storeQuota(
	provider string,
	evaluatedAt int64,
	generation uint64,
	response runtimeinfo.QuotaCurrentResponse,
) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.quotaGen != generation {
		return
	}
	if existing, ok := service.quotaCache[provider]; ok && existing.generation > generation {
		return
	}
	service.quotaCache[provider] = cachedQuota{
		generation: generation, evaluated: evaluatedAt, response: response,
	}
}

func usageKey(request usagecost.UsageCostRequest) usageCacheKey {
	key := usageCacheKey{
		provider: request.Provider.Provider, startDate: request.Range.StartDate, endDate: request.Range.EndDateExclusive,
		timeZone: request.Range.TimeZone, granularity: request.Granularity,
		tokenTotalsOnly: request.TokenTotalsOnly,
	}
	if request.ExactRange != nil {
		key.exact = true
		key.startAtMS = request.ExactRange.StartAtMS
		key.endAtMS = request.ExactRange.EndAtMS
		key.timeZone = request.ExactRange.TimeZone
	}
	return key
}

func cloneRange(value *basequery.UTCTimeRange) *basequery.UTCTimeRange {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validateSummaryRange(request Request) (*basequery.UTCTimeRange, error) {
	if request.ExactRange == nil {
		return basequery.NormalizeLocalDateRange(request.Range, maxRangeDays)
	}
	if request.Range != (basequery.LocalDateRange{}) {
		return nil, fmt.Errorf("%w: timeRange", basequery.ErrValidation)
	}
	return basequery.NormalizeExactTimeRange(*request.ExactRange, maxRangeDays)
}

func validGranularity(value usagecost.TrendGranularity) bool {
	return value == usagecost.TrendHour || value == usagecost.TrendDay
}

func validateUsageResponse(
	provider string,
	expectedRange basequery.UTCTimeRange,
	response usagecost.UsageCostResponse,
) error {
	if response.Meta.Status == basequery.ResponseUnavailable {
		return nil
	}
	if response.Range != expectedRange || response.ReportingTimeZone != expectedRange.TimeZone ||
		response.ProviderContext.EffectiveProvider != provider {
		return errors.New("dashboard summary provider usage envelope is inconsistent")
	}
	return nil
}

func validateQuotaResponse(provider string, response runtimeinfo.QuotaCurrentResponse) error {
	if response.Meta.Status == basequery.ResponseUnavailable {
		return nil
	}
	if response.ProviderContext.EffectiveProvider != provider {
		return errors.New("dashboard summary provider quota envelope is inconsistent")
	}
	return nil
}

func assembleSummary(
	rangeValue basequery.UTCTimeRange,
	activityRange basequery.UTCTimeRange,
	usageGen uint64,
	quotaGen uint64,
	fetched []providerFetch,
) (Response, error) {
	slices := make([]ProviderSlice, 0, len(fetched))
	quotas := make([]QuotaCard, 0, len(fetched))
	for _, item := range fetched {
		slices = append(slices, mapProviderSlice(item))
		quotas = append(quotas, mapQuotaCard(item))
	}
	totals, coverage, err := aggregateTotals(slices)
	if err != nil {
		return Response{}, err
	}
	trend, err := aggregateTrend(slices)
	if err != nil {
		return Response{}, err
	}
	distribution, err := aggregateDistribution(slices)
	if err != nil {
		return Response{}, err
	}
	models := aggregateModels(slices)
	activitySlices := make([]ProviderSlice, 0, len(fetched))
	activityProviderCoverage := make([]ActivityProviderCoverage, 0, len(fetched))
	for _, item := range fetched {
		slice := mapActivitySlice(item)
		activitySlices = append(activitySlices, slice)
		activityProviderCoverage = append(activityProviderCoverage, ActivityProviderCoverage{
			Provider: slice.Provider, CoverageState: slice.CoverageState,
		})
	}
	activityCoverageState, activity := aggregateActivity(activitySlices)
	status := basequery.ResponseComplete
	var issueCodes []basequery.ErrorCode
	switch coverage.OverallState {
	case CoverageComplete, CoverageEmpty:
		status = basequery.ResponseComplete
	case CoveragePartial, CoverageUnknown:
		status = basequery.ResponsePartial
		issueCodes = []basequery.ErrorCode{basequery.ErrorPartial}
	default:
		status = basequery.ResponseUnavailable
		issueCodes = []basequery.ErrorCode{basequery.ErrorUnavailable}
	}
	if err := reconcileComplete(
		slices, totals, trend, coverage.TokenState, coverage.CostState,
	); err != nil {
		status = basequery.ResponseUnavailable
		issueCodes = []basequery.ErrorCode{basequery.ErrorUnavailable}
		totals.TotalTokens, err = unknownNumeric(basequery.NumericTokens, basequery.UnknownUnavailable)
		if err != nil {
			return Response{}, err
		}
		totals.EstimatedUSDMicros, err = unknownNumeric(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
		if err != nil {
			return Response{}, err
		}
		totals.ActiveProviderCount, err = unknownNumeric(basequery.NumericCount, basequery.UnknownUnavailable)
		if err != nil {
			return Response{}, err
		}
		coverage.OverallState = CoverageUnavailable
		coverage.TokenState = CoverageUnavailable
		coverage.CostState = CoverageUnavailable
	}
	if status == basequery.ResponseComplete &&
		activityCoverageState != CoverageComplete && activityCoverageState != CoverageEmpty {
		status = basequery.ResponsePartial
		issueCodes = []basequery.ErrorCode{basequery.ErrorPartial}
	}
	meta, err := basequery.NewResponseMeta(status, nil, issueCodes)
	if err != nil {
		return Response{}, err
	}
	meta.Version = ContractVersion
	return Response{
		Meta: meta, Range: rangeValue, ReportingTimeZone: rangeValue.TimeZone,
		Coverage: coverage, Totals: totals, Providers: slices, Trend: trend,
		Distribution: distribution, Models: models, Quotas: quotas,
		UsageGeneration: usageGen, QuotaGeneration: quotaGen,
		ActivityRange: activityRange, ActivityCoverageState: activityCoverageState,
		Activity: activity, ActivityProviderCoverage: activityProviderCoverage,
	}, nil
}

func mapProviderSlice(item providerFetch) ProviderSlice {
	if item.usageErr != nil {
		return unavailableSlice(item.provider)
	}
	coverage := coverageFromUsage(item.usage)
	if coverage == CoverageUnavailable {
		return unavailableSlice(item.provider)
	}
	costState := costStateFromUsage(item.usage, coverage)
	models := namespacedModels(item.provider, item.usage.Models)
	modelCount := int32(len(models))
	if len(models) > modelLimit {
		models = models[:modelLimit]
	}
	contextValue := item.usage.ProviderContext
	if contextValue.EffectiveProvider == "" {
		contextValue.EffectiveProvider = item.provider
	}
	return ProviderSlice{
		Provider: item.provider, ProviderContext: contextValue, CoverageState: coverage,
		Totals: item.usage.Totals, Trend: append([]usagecost.TrendPoint(nil), item.usage.Trend...),
		Models: models, ReportedUSDMicros: item.usage.ReportedUSDMicros,
		ReportedCostSource: item.usage.ReportedCostSource, DataAsOfMS: item.usage.DataAsOfMS,
		DegradedReason: item.usage.DegradedReason, CostState: costState,
		ModelCount: modelCount,
	}
}

func mapActivitySlice(item providerFetch) ProviderSlice {
	if item.activityErr != nil {
		return unavailableSlice(item.provider)
	}
	coverage := coverageFromUsage(item.activity)
	if coverage == CoverageUnavailable {
		return unavailableSlice(item.provider)
	}
	return ProviderSlice{
		Provider:        item.provider,
		ProviderContext: item.activity.ProviderContext,
		CoverageState:   coverage,
		Totals:          item.activity.Totals,
		Trend:           append([]usagecost.TrendPoint(nil), item.activity.Trend...),
		CostState:       CoverageUnknown,
		Models:          []usagecost.UsageModelItem{},
	}
}

func aggregateActivity(slices []ProviderSlice) (CoverageState, []TrendPoint) {
	states := make([]CoverageState, 0, len(slices))
	for _, slice := range slices {
		states = append(states, slice.CoverageState)
	}
	state := mergeStates(states)
	trend, err := aggregateTrend(slices)
	if err != nil {
		return CoverageUnavailable, []TrendPoint{}
	}
	if state != CoverageComplete && state != CoverageEmpty {
		return state, trend
	}
	totals, _, err := aggregateTotals(slices)
	if err != nil || reconcileTokens(slices, totals, trend) != nil {
		return CoverageUnavailable, []TrendPoint{}
	}
	return state, trend
}

func mapQuotaCard(item providerFetch) QuotaCard {
	if item.quotaErr != nil {
		return unavailableQuota(item.provider)
	}
	coverage := CoverageComplete
	switch item.quota.Meta.Status {
	case basequery.ResponsePartial:
		coverage = CoveragePartial
	case basequery.ResponseUnavailable:
		return unavailableQuota(item.provider)
	case basequery.ResponseComplete:
		if len(item.quota.Current.Windows) == 0 {
			coverage = CoverageEmpty
		}
	default:
		coverage = CoverageUnknown
	}
	contextValue := item.quota.ProviderContext
	if contextValue.EffectiveProvider == "" {
		contextValue.EffectiveProvider = item.provider
	}
	return QuotaCard{
		Provider: item.provider, ProviderContext: contextValue, CoverageState: coverage,
		Meta: item.quota.Meta, Current: item.quota.Current,
	}
}

func unavailableSlice(provider string) ProviderSlice {
	totals, _ := unknownUsageTotals(basequery.UnknownUnavailable)
	unknownCost, _ := unknownNumeric(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	return ProviderSlice{
		Provider: provider, ProviderContext: providerContext(provider),
		CoverageState: CoverageUnavailable, Totals: totals, Trend: []usagecost.TrendPoint{},
		Models: []usagecost.UsageModelItem{}, ReportedUSDMicros: &unknownCost,
		CostState: CoverageUnavailable,
	}
}

func unavailableQuota(provider string) QuotaCard {
	meta, _ := basequery.NewResponseMeta(basequery.ResponseUnavailable, nil, []basequery.ErrorCode{basequery.ErrorUnavailable})
	meta.Version = runtimeinfo.ContractVersion
	return QuotaCard{
		Provider: provider, ProviderContext: providerContext(provider),
		CoverageState: CoverageUnavailable, Meta: meta,
		Current: quotaquery.CurrentResponse{},
	}
}

func providerContext(provider string) agentprovider.Context {
	switch provider {
	case agentprovider.Codex:
		return agentprovider.CodexContext()
	case agentprovider.Grok:
		return agentprovider.GrokContext()
	default:
		return agentprovider.Context{EffectiveProvider: provider}
	}
}

func coverageFromUsage(response usagecost.UsageCostResponse) CoverageState {
	if response.Meta.Status == basequery.ResponseUnavailable {
		return CoverageUnavailable
	}
	if response.Totals.TotalTokens.Value == nil {
		return CoverageUnknown
	}
	if response.Meta.Status == basequery.ResponsePartial {
		return CoveragePartial
	}
	if response.Meta.Status != basequery.ResponseComplete {
		return CoverageUnknown
	}
	if numericValue(response.Totals.TotalTokens) == 0 {
		return CoverageEmpty
	}
	return CoverageComplete
}

func costStateFromUsage(response usagecost.UsageCostResponse, coverage CoverageState) CoverageState {
	if coverage == CoverageUnavailable {
		return CoverageUnavailable
	}
	if response.Totals.EstimatedUSDMicros.Value == nil {
		return CoverageUnknown
	}
	if response.Totals.UnpricedTurnCount.Value != nil && *response.Totals.UnpricedTurnCount.Value > 0 {
		return CoveragePartial
	}
	if coverage == CoverageEmpty {
		return CoverageEmpty
	}
	if coverage == CoveragePartial {
		return CoveragePartial
	}
	return CoverageComplete
}

func namespacedModels(provider string, items []usagecost.UsageModelItem) []usagecost.UsageModelItem {
	result := make([]usagecost.UsageModelItem, 0, len(items))
	for _, item := range items {
		cloned := item
		cloned.DimensionKey = provider + ":" + item.DimensionKey
		cloned.Trend = append([]usagecost.TrendPoint(nil), item.Trend...)
		result = append(result, cloned)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := numericValue(result[i].Totals.TotalTokens), numericValue(result[j].Totals.TotalTokens)
		if left == right {
			return result[i].DimensionKey < result[j].DimensionKey
		}
		return left > right
	})
	return result
}

func aggregateTotals(slices []ProviderSlice) (Totals, Coverage, error) {
	knownTokens := int32(0)
	knownCost := int32(0)
	active := int64(0)
	tokenValues := make([]basequery.NumericValue, 0, len(slices))
	costValues := make([]basequery.NumericValue, 0, len(slices))
	tokenStates := make([]CoverageState, 0, len(slices))
	costStates := make([]CoverageState, 0, len(slices))
	for _, slice := range slices {
		tokenStates = append(tokenStates, slice.CoverageState)
		costStates = append(costStates, slice.CostState)
		if isKnownState(slice.CoverageState) && slice.Totals.TotalTokens.Value != nil {
			knownTokens++
			tokenValues = append(tokenValues, slice.Totals.TotalTokens)
			if *slice.Totals.TotalTokens.Value > 0 {
				active++
			}
		}
		if isKnownState(slice.CostState) && slice.Totals.EstimatedUSDMicros.Value != nil {
			knownCost++
			costValues = append(costValues, slice.Totals.EstimatedUSDMicros)
		}
	}
	tokens, err := sumKnown(tokenValues, basequery.NumericTokens, len(tokenValues) == 0)
	if err != nil {
		return Totals{}, Coverage{}, err
	}
	cost, err := sumKnown(costValues, basequery.NumericMicroUSD, len(costValues) == 0)
	if err != nil {
		return Totals{}, Coverage{}, err
	}
	activeValue, err := basequery.KnownNumeric(active, basequery.NumericCount)
	if err != nil {
		return Totals{}, Coverage{}, err
	}
	if knownTokens == 0 {
		activeValue, err = unknownNumeric(basequery.NumericCount, basequery.UnknownUnavailable)
		if err != nil {
			return Totals{}, Coverage{}, err
		}
	}
	coverage := Coverage{
		KnownProviderCount: knownTokens, KnownCostProviderCount: knownCost,
		TotalProviderCount: int32(len(slices)), TokenState: mergeStates(tokenStates),
		CostState: mergeCostStates(costStates),
	}
	coverage.OverallState = mergeStates([]CoverageState{coverage.TokenState, coverage.CostState})
	return Totals{
		TotalTokens: tokens, EstimatedUSDMicros: cost, ActiveProviderCount: activeValue,
	}, coverage, nil
}

func isKnownState(state CoverageState) bool {
	return state == CoverageComplete || state == CoveragePartial || state == CoverageEmpty
}

func mergeStates(states []CoverageState) CoverageState {
	if len(states) == 0 {
		return CoverageUnavailable
	}
	unavailable := 0
	empty := 0
	partial := 0
	unknown := 0
	complete := 0
	for _, state := range states {
		switch state {
		case CoverageUnavailable:
			unavailable++
		case CoverageEmpty:
			empty++
		case CoveragePartial:
			partial++
		case CoverageUnknown:
			unknown++
		case CoverageComplete:
			complete++
		default:
			unknown++
		}
	}
	switch {
	case unavailable == len(states):
		return CoverageUnavailable
	case empty == len(states):
		return CoverageEmpty
	case complete+empty == len(states):
		return CoverageComplete
	case complete+empty+partial > 0:
		return CoveragePartial
	case unknown == len(states):
		return CoverageUnknown
	default:
		return CoveragePartial
	}
}

func mergeCostStates(states []CoverageState) CoverageState {
	if len(states) == 0 {
		return CoverageUnavailable
	}
	complete := 0
	empty := 0
	known := 0
	unavailable := 0
	unknown := 0
	for _, state := range states {
		switch state {
		case CoverageComplete:
			complete++
			known++
		case CoverageEmpty:
			empty++
			known++
		case CoveragePartial:
			known++
		case CoverageUnavailable:
			unavailable++
		default:
			unknown++
		}
	}
	switch {
	case unavailable == len(states):
		return CoverageUnavailable
	case complete+empty == len(states) && complete > 0:
		return CoverageComplete
	case empty == len(states):
		return CoverageEmpty
	case known > 0:
		return CoveragePartial
	case unknown > 0:
		return CoverageUnknown
	default:
		return CoverageUnavailable
	}
}

func aggregateTrend(slices []ProviderSlice) ([]TrendPoint, error) {
	type bucket struct {
		start int64
		end   int64
	}
	index := map[string]bucket{}
	for _, slice := range slices {
		if slice.CoverageState == CoverageUnavailable || slice.CoverageState == CoverageUnknown {
			continue
		}
		for _, point := range slice.Trend {
			start, err := requiredNumeric(point.StartAtMS, basequery.NumericMilliseconds)
			if err != nil {
				return nil, err
			}
			end, err := requiredNumeric(point.EndAtMS, basequery.NumericMilliseconds)
			if err != nil || point.Key == "" || end <= start {
				return nil, errors.New("dashboard summary trend bucket is invalid")
			}
			if existing, exists := index[point.Key]; exists {
				if existing.start != start || existing.end != end {
					return nil, errors.New("dashboard summary trend bucket is inconsistent")
				}
				continue
			}
			index[point.Key] = bucket{start: start, end: end}
		}
	}
	keys := make([]string, 0, len(index))
	for key := range index {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := index[keys[i]], index[keys[j]]
		if left.start == right.start {
			return keys[i] < keys[j]
		}
		return left.start < right.start
	})
	result := make([]TrendPoint, 0, len(keys))
	for _, key := range keys {
		shares := make([]ProviderShare, 0, len(slices))
		tokenValues := make([]basequery.NumericValue, 0, len(slices))
		costValues := make([]basequery.NumericValue, 0, len(slices))
		for _, slice := range slices {
			share, err := trendShare(slice, key)
			if err != nil {
				return nil, err
			}
			shares = append(shares, share)
			if share.TotalTokens.Value != nil {
				tokenValues = append(tokenValues, share.TotalTokens)
			}
			if share.EstimatedUSDMicros.Value != nil {
				costValues = append(costValues, share.EstimatedUSDMicros)
			}
		}
		tokens, err := sumKnown(tokenValues, basequery.NumericTokens, len(tokenValues) == 0)
		if err != nil {
			return nil, err
		}
		cost, err := sumKnown(costValues, basequery.NumericMicroUSD, len(costValues) == 0)
		if err != nil {
			return nil, err
		}
		start, err := basequery.KnownNumeric(index[key].start, basequery.NumericMilliseconds)
		if err != nil {
			return nil, err
		}
		end, err := basequery.KnownNumeric(index[key].end, basequery.NumericMilliseconds)
		if err != nil {
			return nil, err
		}
		result = append(result, TrendPoint{
			Key: key, StartAtMS: start, EndAtMS: end,
			TotalTokens: tokens, EstimatedUSDMicros: cost, Shares: shares,
		})
	}
	return result, nil
}

func trendShare(slice ProviderSlice, key string) (ProviderShare, error) {
	unknownTokens, err := unknownNumeric(basequery.NumericTokens, basequery.UnknownUnavailable)
	if err != nil {
		return ProviderShare{}, err
	}
	unknownCost, err := unknownNumeric(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	if err != nil {
		return ProviderShare{}, err
	}
	for _, point := range slice.Trend {
		if point.Key != key {
			continue
		}
		return ProviderShare{
			Provider: slice.Provider, TotalTokens: point.Totals.TotalTokens,
			EstimatedUSDMicros: point.Totals.EstimatedUSDMicros,
		}, nil
	}
	tokens := unknownTokens
	if slice.CoverageState == CoverageComplete || slice.CoverageState == CoverageEmpty {
		tokens, err = basequery.KnownNumeric(0, basequery.NumericTokens)
		if err != nil {
			return ProviderShare{}, err
		}
	}
	cost := unknownCost
	if slice.CostState == CoverageComplete || slice.CostState == CoverageEmpty {
		cost, err = basequery.KnownNumeric(0, basequery.NumericMicroUSD)
		if err != nil {
			return ProviderShare{}, err
		}
	}
	return ProviderShare{
		Provider: slice.Provider, TotalTokens: tokens, EstimatedUSDMicros: cost,
	}, nil
}

func aggregateDistribution(slices []ProviderSlice) ([]ProviderShare, error) {
	result := make([]ProviderShare, 0, len(slices))
	for _, slice := range slices {
		share := ProviderShare{
			Provider: slice.Provider, TotalTokens: slice.Totals.TotalTokens,
			EstimatedUSDMicros: slice.Totals.EstimatedUSDMicros,
		}
		if !isKnownState(slice.CoverageState) || share.TotalTokens.Value == nil {
			unknownTokens, err := unknownNumeric(basequery.NumericTokens, basequery.UnknownUnavailable)
			if err != nil {
				return nil, err
			}
			share.TotalTokens = unknownTokens
		}
		if !isKnownState(slice.CostState) || share.EstimatedUSDMicros.Value == nil {
			unknownCost, err := unknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
			if err != nil {
				return nil, err
			}
			share.EstimatedUSDMicros = unknownCost
		}
		result = append(result, share)
	}
	return result, nil
}

func aggregateModels(slices []ProviderSlice) []ModelItem {
	items := make([]ModelItem, 0)
	for _, slice := range slices {
		if slice.CoverageState == CoverageUnavailable || slice.CoverageState == CoverageUnknown {
			continue
		}
		for _, model := range slice.Models {
			items = append(items, ModelItem{
				Provider: slice.Provider, DimensionKey: model.DimensionKey,
				Model: model.Model, Totals: model.Totals,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := numericValue(items[i].Totals.TotalTokens), numericValue(items[j].Totals.TotalTokens)
		if left == right {
			if items[i].Provider == items[j].Provider {
				return items[i].DimensionKey < items[j].DimensionKey
			}
			return items[i].Provider < items[j].Provider
		}
		return left > right
	})
	if len(items) > modelLimit {
		items = items[:modelLimit]
	}
	return items
}

func reconcileComplete(
	slices []ProviderSlice,
	totals Totals,
	trend []TrendPoint,
	tokenState CoverageState,
	costState CoverageState,
) error {
	if tokenState == CoverageComplete || tokenState == CoverageEmpty {
		if err := reconcileTokens(slices, totals, trend); err != nil {
			return err
		}
	}
	if costState == CoverageComplete || costState == CoverageEmpty {
		if err := reconcileCost(slices, totals, trend); err != nil {
			return err
		}
	}
	return nil
}

func reconcileTokens(slices []ProviderSlice, totals Totals, trend []TrendPoint) error {
	sliceTotals := make([]basequery.NumericValue, 0, len(slices))
	for _, slice := range slices {
		sliceTotal, err := requiredNumeric(slice.Totals.TotalTokens, basequery.NumericTokens)
		if err != nil {
			return err
		}
		sliceTrend := make([]basequery.NumericValue, 0, len(slice.Trend))
		for _, point := range slice.Trend {
			sliceTrend = append(sliceTrend, point.Totals.TotalTokens)
		}
		trendTotal, err := sumRequired(sliceTrend, basequery.NumericTokens)
		if err != nil || trendTotal != sliceTotal {
			return errors.New("dashboard summary token reconcile failed")
		}
		sliceTotals = append(sliceTotals, slice.Totals.TotalTokens)
	}
	total, err := requiredNumeric(totals.TotalTokens, basequery.NumericTokens)
	if err != nil {
		return err
	}
	sliceTotal, err := sumRequired(sliceTotals, basequery.NumericTokens)
	if err != nil || sliceTotal != total {
		return errors.New("dashboard summary token reconcile failed")
	}
	trendTotals := make([]basequery.NumericValue, 0, len(trend))
	for _, point := range trend {
		pointTotal, err := requiredNumeric(point.TotalTokens, basequery.NumericTokens)
		if err != nil {
			return err
		}
		shares := make([]basequery.NumericValue, 0, len(point.Shares))
		for _, share := range point.Shares {
			shares = append(shares, share.TotalTokens)
		}
		shareTotal, err := sumRequired(shares, basequery.NumericTokens)
		if err != nil || shareTotal != pointTotal {
			return errors.New("dashboard summary token reconcile failed")
		}
		trendTotals = append(trendTotals, point.TotalTokens)
	}
	trendTotal, err := sumRequired(trendTotals, basequery.NumericTokens)
	if err != nil || trendTotal != total {
		return errors.New("dashboard summary token reconcile failed")
	}
	return nil
}

func reconcileCost(slices []ProviderSlice, totals Totals, trend []TrendPoint) error {
	sliceTotals := make([]basequery.NumericValue, 0, len(slices))
	for _, slice := range slices {
		sliceTotal, err := requiredNumeric(slice.Totals.EstimatedUSDMicros, basequery.NumericMicroUSD)
		if err != nil {
			return err
		}
		sliceTrend := make([]basequery.NumericValue, 0, len(slice.Trend))
		for _, point := range slice.Trend {
			sliceTrend = append(sliceTrend, point.Totals.EstimatedUSDMicros)
		}
		trendTotal, err := sumRequired(sliceTrend, basequery.NumericMicroUSD)
		if err != nil || trendTotal != sliceTotal {
			return errors.New("dashboard summary cost reconcile failed")
		}
		sliceTotals = append(sliceTotals, slice.Totals.EstimatedUSDMicros)
	}
	total, err := requiredNumeric(totals.EstimatedUSDMicros, basequery.NumericMicroUSD)
	if err != nil {
		return err
	}
	sliceTotal, err := sumRequired(sliceTotals, basequery.NumericMicroUSD)
	if err != nil || sliceTotal != total {
		return errors.New("dashboard summary cost reconcile failed")
	}
	trendTotals := make([]basequery.NumericValue, 0, len(trend))
	for _, point := range trend {
		pointTotal, err := requiredNumeric(point.EstimatedUSDMicros, basequery.NumericMicroUSD)
		if err != nil {
			return err
		}
		shares := make([]basequery.NumericValue, 0, len(point.Shares))
		for _, share := range point.Shares {
			shares = append(shares, share.EstimatedUSDMicros)
		}
		shareTotal, err := sumRequired(shares, basequery.NumericMicroUSD)
		if err != nil || shareTotal != pointTotal {
			return errors.New("dashboard summary cost reconcile failed")
		}
		trendTotals = append(trendTotals, point.EstimatedUSDMicros)
	}
	trendTotal, err := sumRequired(trendTotals, basequery.NumericMicroUSD)
	if err != nil || trendTotal != total {
		return errors.New("dashboard summary cost reconcile failed")
	}
	return nil
}

func sumRequired(values []basequery.NumericValue, unit basequery.NumericUnit) (int64, error) {
	var sum int64
	for _, value := range values {
		current, err := requiredNumeric(value, unit)
		if err != nil {
			return 0, err
		}
		if current > basequery.JavaScriptMaxSafeInteger-sum {
			return 0, errors.New("dashboard summary numeric overflow")
		}
		sum += current
	}
	return sum, nil
}

func requiredNumeric(value basequery.NumericValue, unit basequery.NumericUnit) (int64, error) {
	if err := value.Validate(); err != nil || value.Unit != unit || value.Value == nil {
		return 0, errors.New("dashboard summary numeric value is invalid")
	}
	return *value.Value, nil
}

func sumKnown(values []basequery.NumericValue, unit basequery.NumericUnit, none bool) (basequery.NumericValue, error) {
	if none || len(values) == 0 {
		reason := basequery.UnknownUnavailable
		if unit == basequery.NumericMicroUSD {
			reason = basequery.UnknownNotComputed
		}
		return unknownNumeric(unit, reason)
	}
	var sum int64
	for _, value := range values {
		if value.Value == nil {
			continue
		}
		current, err := requiredNumeric(value, unit)
		if err != nil {
			return basequery.NumericValue{}, err
		}
		if current > basequery.JavaScriptMaxSafeInteger-sum {
			return basequery.NumericValue{}, errors.New("dashboard summary numeric overflow")
		}
		sum += current
	}
	return basequery.KnownNumeric(sum, unit)
}

func numericValue(value basequery.NumericValue) int64 {
	if value.Value == nil {
		return 0
	}
	return *value.Value
}

func unknownNumeric(unit basequery.NumericUnit, reason basequery.UnknownReason) (basequery.NumericValue, error) {
	return basequery.UnknownNumeric(unit, reason)
}

func unknownUsageTotals(reason basequery.UnknownReason) (usagecost.UsageTotals, error) {
	var err error
	totals := usagecost.UsageTotals{}
	totals.TurnCount, err = unknownNumeric(basequery.NumericCount, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.InputTokens, err = unknownNumeric(basequery.NumericTokens, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.CachedInputTokens, err = unknownNumeric(basequery.NumericTokens, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.OutputTokens, err = unknownNumeric(basequery.NumericTokens, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.ReasoningTokens, err = unknownNumeric(basequery.NumericTokens, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.TotalTokens, err = unknownNumeric(basequery.NumericTokens, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.EstimatedUSDMicros, err = unknownNumeric(basequery.NumericMicroUSD, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.PricedTurnCount, err = unknownNumeric(basequery.NumericCount, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.UnpricedTurnCount, err = unknownNumeric(basequery.NumericCount, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.FirstActivityAtMS, err = unknownNumeric(basequery.NumericMilliseconds, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	totals.LastActivityAtMS, err = unknownNumeric(basequery.NumericMilliseconds, reason)
	if err != nil {
		return usagecost.UsageTotals{}, err
	}
	return totals, nil
}
