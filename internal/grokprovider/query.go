package grokprovider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const maxRangeDays = 366

type SnapshotReader interface {
	GrokSnapshot(context.Context) (store.GrokSnapshot, error)
}

type Refresher interface {
	Refresh(context.Context) error
}

type conditionalRefresher interface {
	RefreshIfDue(context.Context) (bool, error)
}

type QueryService struct {
	collector       *Collector
	billing         Refresher
	reader          SnapshotReader
	sessions        basequery.Specification
	projects        basequery.Specification
	snapshotMu      sync.RWMutex
	snapshotLoadMu  sync.Mutex
	snapshotCache   *store.GrokSnapshot
	snapshotEpoch   uint64
	refreshMu       sync.Mutex
	localRefreshing bool
	refreshing      bool
	onRefresh       func()
}

type discardSnapshotWriter struct{}

func (discardSnapshotWriter) ReplaceGrokSnapshot(context.Context, store.GrokSnapshot) error {
	return nil
}

type emptySnapshotReader struct{}

func (emptySnapshotReader) GrokSnapshot(context.Context) (store.GrokSnapshot, error) {
	return store.GrokSnapshot{}, nil
}

func NewDisabledQueryService() (*QueryService, error) {
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		SessionsRoot:   os.TempDir(),
		MinimumRefresh: time.Hour,
		Now:            time.Now,
	})
	if err != nil {
		return nil, err
	}
	return NewQueryService(collector, emptySnapshotReader{})
}

func NewQueryService(collector *Collector, reader SnapshotReader, billing ...Refresher) (*QueryService, error) {
	if collector == nil || reader == nil || len(billing) > 1 || (len(billing) == 1 && billing[0] == nil) {
		return nil, ErrCollector
	}
	sessions, err := basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 50, MaxLimit: 100, MaxRangeDays: maxRangeDays,
		SortFields: []string{"lastActivityAt", "totalTokens", "estimatedCost", "sessionId"},
		FilterFields: []basequery.FilterField{
			{Field: "projectId", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "modelKey", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "activity", Operators: []basequery.FilterOperator{basequery.FilterEqual}},
		},
		DefaultSort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
		TieBreaker:  basequery.SortTerm{Field: "sessionId", Direction: basequery.SortDescending},
	})
	if err != nil {
		return nil, ErrCollector
	}
	projects, err := basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 50, MaxLimit: 100, MaxRangeDays: maxRangeDays,
		SortFields: []string{"lastActivityAt", "totalTokens", "estimatedCost", "displayName", "projectKey"},
		FilterFields: []basequery.FilterField{
			{Field: "projectId", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "confidence", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
		},
		DefaultSort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
		TieBreaker:  basequery.SortTerm{Field: "projectKey", Direction: basequery.SortDescending},
	})
	if err != nil {
		return nil, ErrCollector
	}
	service := &QueryService{collector: collector, reader: reader, sessions: sessions, projects: projects}
	if len(billing) == 1 {
		service.billing = billing[0]
	}
	return service, nil
}

func (service *QueryService) Refresh(ctx context.Context) error {
	if service == nil || service.collector == nil || service.reader == nil {
		return ErrCollector
	}
	performed, err := service.collector.RefreshIfDue(ctx)
	if err != nil {
		return err
	}
	if performed {
		service.invalidateSnapshot()
	}
	service.scheduleBillingRefresh()
	return nil
}

func (service *QueryService) RefreshQuota(ctx context.Context) error {
	if service == nil || service.billing == nil || ctx == nil {
		return ErrCollector
	}
	if err := service.billing.Refresh(ctx); err != nil {
		return err
	}
	service.invalidateSnapshot()
	service.refreshMu.Lock()
	notifier := service.onRefresh
	service.refreshMu.Unlock()
	if notifier != nil {
		notifier()
	}
	return nil
}

func (service *QueryService) SetRefreshNotifier(notifier func()) {
	if service == nil {
		return
	}
	service.refreshMu.Lock()
	service.onRefresh = notifier
	service.refreshMu.Unlock()
}

func (service *QueryService) snapshot(ctx context.Context) (store.GrokSnapshot, error) {
	if service == nil || service.collector == nil || service.reader == nil {
		return store.GrokSnapshot{}, ErrCollector
	}
	snapshot, err := service.loadSnapshot(ctx)
	if err != nil {
		return store.GrokSnapshot{}, err
	}
	service.scheduleLocalRefresh()
	service.scheduleBillingRefresh()
	return snapshot, nil
}

func (service *QueryService) loadSnapshot(ctx context.Context) (store.GrokSnapshot, error) {
	service.snapshotMu.RLock()
	if service.snapshotCache != nil {
		snapshot := *service.snapshotCache
		service.snapshotMu.RUnlock()
		return snapshot, nil
	}
	service.snapshotMu.RUnlock()

	service.snapshotLoadMu.Lock()
	defer service.snapshotLoadMu.Unlock()
	service.snapshotMu.RLock()
	if service.snapshotCache != nil {
		snapshot := *service.snapshotCache
		service.snapshotMu.RUnlock()
		return snapshot, nil
	}
	epoch := service.snapshotEpoch
	service.snapshotMu.RUnlock()

	snapshot, err := service.reader.GrokSnapshot(ctx)
	if errors.Is(err, store.ErrNotFound) {
		if refreshErr := service.collector.Refresh(ctx); refreshErr != nil {
			return store.GrokSnapshot{}, refreshErr
		}
		snapshot, err = service.reader.GrokSnapshot(ctx)
	}
	if err != nil {
		return store.GrokSnapshot{}, err
	}
	service.snapshotMu.Lock()
	if service.snapshotEpoch == epoch {
		service.snapshotCache = &snapshot
	}
	service.snapshotMu.Unlock()
	return snapshot, nil
}

func (service *QueryService) invalidateSnapshot() {
	service.snapshotMu.Lock()
	service.snapshotCache = nil
	service.snapshotEpoch++
	service.snapshotMu.Unlock()
}

func (service *QueryService) scheduleLocalRefresh() {
	service.refreshMu.Lock()
	if service.localRefreshing {
		service.refreshMu.Unlock()
		return
	}
	service.localRefreshing = true
	service.refreshMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		performed, err := service.collector.RefreshIfDue(ctx)
		service.refreshMu.Lock()
		service.localRefreshing = false
		notifier := service.onRefresh
		service.refreshMu.Unlock()
		if performed && err == nil {
			service.invalidateSnapshot()
			if notifier != nil {
				notifier()
			}
		}
	}()
}

func (service *QueryService) scheduleBillingRefresh() {
	if service == nil || service.billing == nil {
		return
	}
	service.refreshMu.Lock()
	if service.refreshing {
		service.refreshMu.Unlock()
		return
	}
	service.refreshing = true
	service.refreshMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		performed := true
		var err error
		if refresher, ok := service.billing.(conditionalRefresher); ok {
			performed, err = refresher.RefreshIfDue(ctx)
		} else {
			err = service.billing.Refresh(ctx)
		}
		service.refreshMu.Lock()
		service.refreshing = false
		notifier := service.onRefresh
		service.refreshMu.Unlock()
		if performed && err == nil {
			service.invalidateSnapshot()
			if notifier != nil {
				notifier()
			}
		}
	}()
}

func (service *QueryService) UsageCost(ctx context.Context, request usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	rangeValue, err := usageRange(request.Range, request.ExactRange)
	if err != nil || !validTrendGranularity(request.Granularity) {
		return usagecost.UsageCostResponse{}, fmt.Errorf("%w: grok usage request", basequery.ErrValidation)
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	events := usageEventsInRange(snapshot.UsageEvents, *rangeValue)
	response := usagecost.UsageCostResponse{
		ProviderContext: contextFor(snapshot), Meta: completeMeta(nil), Range: *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone, PricingVersions: []string{},
		Trend: []usagecost.TrendPoint{}, Models: []usagecost.UsageModelItem{},
		UnpricedReasons: []usagecost.ReasonCount{},
	}
	totals, estimatedOK := totalsForUsageEvents(events)
	response.Totals = totals
	response.Trend = usageTrend(events, *rangeValue, request.Granularity)
	if request.IncludeActivityDistribution {
		response.ActivityDistribution = activityDistribution(events, *rangeValue)
	}
	if reported, ok := reportedTotal(events); ok {
		value := known(reported, basequery.NumericMicroUSD)
		source := SourceUpdates
		response.ReportedUSDMicros = &value
		response.ReportedCostSource = &source
		currency := "USD"
		response.Currency = &currency
	}
	if estimatedOK && response.Totals.EstimatedUSDMicros.Value != nil {
		source := pricing.GrokPricingSourceURL
		response.PricingSource = &source
		response.PricingVersions = []string{pricing.GrokPricingVersion}
		currency := "USD"
		response.Currency = &currency
	}
	if snapshot.Billing != nil {
		dataAsOf := known(snapshot.Billing.CollectedAtMS, basequery.NumericMilliseconds)
		response.DataAsOfMS = &dataAsOf
		if snapshot.BillingStale {
			response.Meta = partialMeta(nil)
		}
	}
	if !request.TokenTotalsOnly {
		response.Models = usageModels(events, *rangeValue, request.Granularity)
		if unpriced := unpricedCount(events); unpriced > 0 {
			response.UnpricedReasons = []usagecost.ReasonCount{{
				Reason: pricing.CostReasonMissingPriceComponent,
				Count:  known(unpriced, basequery.NumericCount),
			}}
		}
	}
	return response, nil
}

func (service *QueryService) ListSessions(ctx context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	validated, err := service.sessions.Validate(ctx, request)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	sessions, err := grokSessions(snapshot, validated)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	usageBySession := groupUsageBySession(snapshot.UsageEvents)
	sortGrokSessions(sessions, validated.Sort[0], usageBySession)
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	items := make([]usagecost.SessionItem, 0, minInt(limit, maxInt(0, len(sessions)-offset)))
	for _, session := range slicePage(sessions, offset, limit) {
		items = append(items, sessionItem(session, usageBySession[session.ExternalSessionID]))
	}
	allEvents := make([]store.GrokUsageEvent, 0)
	pageEvents := make([]store.GrokUsageEvent, 0)
	for _, session := range sessions {
		allEvents = append(allEvents, usageBySession[session.ExternalSessionID]...)
	}
	for _, item := range slicePage(sessions, offset, limit) {
		pageEvents = append(pageEvents, usageBySession[item.ExternalSessionID]...)
	}
	matchedTotals, _ := totalsForUsageEvents(allEvents)
	pageTotals, _ := totalsForUsageEvents(pageEvents)
	matchedTotals.TurnCount = known(sessionRequestCount(sessions), basequery.NumericCount)
	pageTotals.TurnCount = known(sessionRequestCount(slicePage(sessions, offset, limit)), basequery.NumericCount)
	return usagecost.SessionListResponse{
		ProviderContext: contextFor(snapshot), Meta: completeMeta(pageInfo(offset, limit, len(sessions), snapshot.Generation, queryFingerprint(validated))),
		Items: items, MatchedCount: known(int64(len(sessions)), basequery.NumericCount),
		MatchedTotals: matchedTotals, PageTotals: pageTotals,
	}, nil
}

func (service *QueryService) SessionDetail(ctx context.Context, request usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	var session *store.GrokSession
	for index := range snapshot.Sessions {
		if snapshot.Sessions[index].ExternalSessionID == request.SessionID {
			session = &snapshot.Sessions[index]
			break
		}
	}
	if session == nil {
		return usagecost.SessionDetailResponse{}, basequery.NewNotFoundFailure(nil)
	}
	timezone := "UTC"
	if request.ReportingTimezone != nil && *request.ReportingTimezone != "" {
		if _, loadErr := time.LoadLocation(*request.ReportingTimezone); loadErr != nil {
			return usagecost.SessionDetailResponse{}, basequery.NewValidationFailure("reportingTimezone", nil)
		}
		timezone = *request.ReportingTimezone
	}
	rangeValue := sessionRange(*session, timezone)
	events := groupUsageBySession(snapshot.UsageEvents)[session.ExternalSessionID]
	sort.Slice(events, func(i, j int) bool { return events[i].OccurredAtMS < events[j].OccurredAtMS })
	offset, limit, err := page(request.TurnPage, snapshot.Generation, digestString("session-turns:" + session.ExternalSessionID)[:16])
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	turns := make([]usagecost.SessionTurnItem, 0)
	for _, event := range slicePage(events, offset, limit) {
		turnTotals, estimatedOK := totalsForUsageEvents([]store.GrokUsageEvent{event})
		turnTotals.TurnCount = known(1, basequery.NumericCount)
		status := usagecost.SessionTurnPricingUnpriced
		var version *string
		if estimatedOK && turnTotals.EstimatedUSDMicros.Value != nil {
			status = usagecost.SessionTurnPricingPriced
			value := pricing.GrokPricingVersion
			version = &value
		}
		turns = append(turns, usagecost.SessionTurnItem{
			TimelineKey: event.EventID, State: usagecost.SessionTurnComplete,
			Model: modelAttribution(event.ModelKey), StartedAt: known(event.OccurredAtMS, basequery.NumericMilliseconds),
			CompletedAt: known(event.OccurredAtMS, basequery.NumericMilliseconds),
			ObservedAt:  known(event.OccurredAtMS, basequery.NumericMilliseconds), Totals: turnTotals,
			PricingStatus: status, PricingVersion: version,
		})
	}
	item := sessionItem(*session, events)
	models := usageModels(events, rangeValue, usagecost.TrendDay)
	trend := usageTrend(events, rangeValue, usagecost.TrendDay)
	var pricingSource, currency *string
	versions := []string{}
	if item.Totals.EstimatedUSDMicros.Value != nil {
		source, currencyValue := pricing.GrokPricingSourceURL, "USD"
		pricingSource, currency = &source, &currencyValue
		versions = []string{pricing.GrokPricingVersion}
	}
	return usagecost.SessionDetailResponse{
		ProviderContext: contextFor(snapshot), Meta: completeMeta(nil),
		PricingSource: pricingSource, Currency: currency, PricingVersions: versions,
		UnpricedReasons: []usagecost.ReasonCount{}, Item: item,
		TurnPage: *pageInfo(offset, limit, len(events), snapshot.Generation, digestString("session-turns:" + session.ExternalSessionID)[:16]),
		Turns:    turns, ReportingTimeZone: timezone, TrendGranularity: usagecost.TrendDay,
		Trend: trend, Models: models,
	}, nil
}

func (service *QueryService) ListProjects(ctx context.Context, request basequery.Request) (usagecost.ProjectListResponse, error) {
	validated, err := service.projects.Validate(ctx, request)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	if validated.TimeRange == nil {
		return usagecost.ProjectListResponse{}, basequery.NewValidationFailure("timeRange", nil)
	}
	sessions, err := grokSessionsForProjects(snapshot, validated)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	rangeEvents := usageEventsInRange(snapshot.UsageEvents, *validated.TimeRange)
	groups := projectGroups(sessions, rangeEvents)
	sortGrokProjects(groups, validated.Sort[0])
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	items := make([]usagecost.ProjectItem, 0)
	for _, group := range slicePage(groups, offset, limit) {
		items = append(items, projectItem(group))
	}
	pageEvents := make([]store.GrokUsageEvent, 0)
	for _, group := range slicePage(groups, offset, limit) {
		pageEvents = append(pageEvents, group.events...)
	}
	globalTotals, _ := totalsForUsageEvents(rangeEvents)
	pageTotals, _ := totalsForUsageEvents(pageEvents)
	return usagecost.ProjectListResponse{
		ProviderContext: contextFor(snapshot), Meta: completeMeta(pageInfo(offset, limit, len(groups), snapshot.Generation, queryFingerprint(validated))),
		Range: *validated.TimeRange, ReportingTimeZone: validated.TimeRange.TimeZone, PricingVersions: []string{},
		Items: items, MatchedCount: known(int64(len(groups)), basequery.NumericCount),
		GlobalTotals: globalTotals, MatchedTotals: globalTotals, PageTotals: pageTotals,
	}, nil
}

func (service *QueryService) ProjectDetail(ctx context.Context, request usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	rangeValue, err := usageRange(request.Range, request.ExactRange)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	rangeEvents := usageEventsInRange(snapshot.UsageEvents, *rangeValue)
	groups := projectGroups(filterSessions(snapshot.Sessions, rangeValue), rangeEvents)
	var selected *grokProjectGroup
	for index := range groups {
		if groups[index].key == request.DimensionKey {
			selected = &groups[index]
			break
		}
	}
	if selected == nil {
		return usagecost.ProjectDetailResponse{}, basequery.NewNotFoundFailure(nil)
	}
	sessionOffset, sessionLimit, err := page(request.SessionPage, snapshot.Generation, digestString("project-sessions:" + request.DimensionKey)[:16])
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	modelOffset, modelLimit, err := page(request.ModelPage, snapshot.Generation, digestString("project-models:" + request.DimensionKey)[:16])
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	usageBySession := groupUsageBySession(selected.events)
	sessionItems := make([]usagecost.ProjectSessionItem, 0)
	for _, session := range slicePage(selected.sessions, sessionOffset, sessionLimit) {
		item := sessionItem(session, usageBySession[session.ExternalSessionID])
		sessionItems = append(sessionItems, usagecost.ProjectSessionItem{
			SessionID: item.SessionID, DisplayTitle: item.DisplayTitle, TitleConfidence: item.TitleConfidence,
			TitleSource: item.TitleSource, TitleReason: item.TitleReason, Model: item.Model,
			Activity: item.Activity, LastActivityAt: item.LastActivityAt, Totals: item.Totals,
		})
	}
	models := usageModels(selected.events, *rangeValue, usagecost.TrendDay)
	modelItems := make([]usagecost.ProjectModelItem, 0)
	for _, item := range slicePage(models, modelOffset, modelLimit) {
		modelItems = append(modelItems, usagecost.ProjectModelItem{DimensionKey: item.DimensionKey, Model: item.Model, Totals: item.Totals})
	}
	globalTotals, _ := totalsForUsageEvents(rangeEvents)
	return usagecost.ProjectDetailResponse{
		ProviderContext: contextFor(snapshot), Meta: completeMeta(nil), Range: *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone, PricingVersions: []string{}, Item: projectItem(*selected),
		Daily:       projectDaily(selected.events, *rangeValue),
		SessionPage: *pageInfo(sessionOffset, sessionLimit, len(selected.sessions), snapshot.Generation, digestString("project-sessions:" + request.DimensionKey)[:16]),
		Sessions:    sessionItems,
		ModelPage:   *pageInfo(modelOffset, modelLimit, len(models), snapshot.Generation, digestString("project-models:" + request.DimensionKey)[:16]),
		Models:      modelItems, GlobalTotals: globalTotals,
	}, nil
}

func (service *QueryService) InvocationUsage(ctx context.Context, request invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error) {
	rangeValue, err := basequery.NormalizeExactTimeRange(request.Range, maxRangeDays)
	if err != nil || (request.Granularity != invocationusage.GranularityHour && request.Granularity != invocationusage.GranularityDay) ||
		(request.SourceClass != invocationusage.SourceClassAll && request.SourceClass != invocationusage.SourceClassStructured && request.SourceClass != invocationusage.SourceClassDetected) {
		return invocationusage.InvocationUsageResponse{}, basequery.NewValidationFailure("invocationUsage", nil)
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return invocationusage.InvocationUsageResponse{}, err
	}
	tools := make([]store.GrokToolEvent, 0)
	if request.SourceClass != invocationusage.SourceClassDetected {
		for _, event := range snapshot.ToolEvents {
			if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
				tools = append(tools, event)
			}
		}
	}
	return invocationResponse(snapshot, *rangeValue, request, tools), nil
}

func contextFor(snapshot store.GrokSnapshot) agentprovider.Context {
	context := agentprovider.Context{
		EffectiveProvider: agentprovider.Grok,
		Capabilities:      []string{"sessions", "projects", "models", "tools", "tokens", "account"},
		Coverage:          []agentprovider.Coverage{},
	}
	for _, source := range snapshot.Sources {
		context.Sources = append(context.Sources, source.SourceKey)
		context.Coverage = append(context.Coverage, agentprovider.Coverage{
			Capability: sourceCapability(source.SourceKey), State: source.State, Source: source.SourceKey,
			Reason: source.CoverageState, ItemCount: &source.RowCount,
		})
		if source.SourceKey == SourceBilling && source.LastSuccessAtMS != nil {
			if !containsString(context.Capabilities, "quota") {
				context.Capabilities = append(context.Capabilities, "quota")
			}
		}
	}
	if hasReportedCost(snapshot.UsageEvents) && !containsString(context.Capabilities, "reported_cost") {
		context.Capabilities = append(context.Capabilities, "reported_cost")
	}
	if hasEstimatedCost(snapshot.UsageEvents) && !containsString(context.Capabilities, "estimated_cost") {
		context.Capabilities = append(context.Capabilities, "estimated_cost")
	}
	return context
}

func sourceCapability(source string) string {
	switch source {
	case SourceSummary:
		return "sessions"
	case SourceUpdates:
		return "tokens"
	case SourceBilling:
		return "quota"
	case SourceSessionSearch:
		return "search"
	default:
		return "unknown"
	}
}
