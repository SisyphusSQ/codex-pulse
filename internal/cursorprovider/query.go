package cursorprovider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

const (
	activityBucketTargetCount  = 36
	activityBucketMinimumCount = 24
	activityBucketMaximumCount = 48
)

var activityBucketMinuteOptions = [...]int{60, 120, 180, 360, 720, 1_440}

type SnapshotReader interface {
	CursorSnapshot(context.Context) (store.CursorSnapshot, error)
}

type Refresher interface {
	Refresh(context.Context) error
}

type conditionalRefresher interface {
	RefreshIfDue(context.Context) (bool, error)
}

type QueryService struct {
	collector       *Collector
	dashboard       Refresher
	grokBot         Refresher
	reader          SnapshotReader
	sessions        basequery.Specification
	projects        basequery.Specification
	snapshotMu      sync.RWMutex
	snapshotLoadMu  sync.Mutex
	snapshotCache   *store.CursorSnapshot
	snapshotEpoch   uint64
	refreshMu       sync.Mutex
	localRefreshing bool
	refreshing      bool
	grokRefreshing  bool
	onRefresh       func()
}

func NewQueryService(collector *Collector, reader SnapshotReader, dashboard ...Refresher) (*QueryService, error) {
	if collector == nil || reader == nil || len(dashboard) > 1 || len(dashboard) == 1 && dashboard[0] == nil {
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
	if len(dashboard) == 1 {
		service.dashboard = dashboard[0]
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
	service.scheduleDashboardRefresh()
	service.scheduleGrokBotRefresh()
	return nil
}

func (service *QueryService) RefreshQuota(ctx context.Context) error {
	if service == nil || service.dashboard == nil || ctx == nil {
		return ErrCollector
	}
	if err := service.dashboard.Refresh(ctx); err != nil {
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

func (service *QueryService) snapshot(ctx context.Context) (store.CursorSnapshot, error) {
	if service == nil || service.collector == nil || service.reader == nil {
		return store.CursorSnapshot{}, ErrCollector
	}
	snapshot, err := service.loadSnapshot(ctx)
	if err != nil {
		return store.CursorSnapshot{}, err
	}
	service.scheduleLocalRefresh()
	service.scheduleDashboardRefresh()
	service.scheduleGrokBotRefresh()
	return snapshot, nil
}

func (service *QueryService) loadSnapshot(ctx context.Context) (store.CursorSnapshot, error) {
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

	snapshot, err := service.reader.CursorSnapshot(ctx)
	if errors.Is(err, store.ErrNotFound) {
		if refreshErr := service.collector.Refresh(ctx); refreshErr != nil {
			return store.CursorSnapshot{}, refreshErr
		}
		snapshot, err = service.reader.CursorSnapshot(ctx)
	}
	if err != nil {
		return store.CursorSnapshot{}, err
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

func (service *QueryService) SetGrokBotRefresher(refresher Refresher) {
	if service == nil {
		return
	}
	service.refreshMu.Lock()
	service.grokBot = refresher
	service.refreshMu.Unlock()
}

// SetRefreshNotifier registers a lightweight invalidation callback. Source
// credentials and results stay inside the Helper; the callback only tells
// clients to re-query the committed local snapshot.
func (service *QueryService) SetRefreshNotifier(notifier func()) {
	if service == nil {
		return
	}
	service.refreshMu.Lock()
	service.onRefresh = notifier
	service.refreshMu.Unlock()
}

func (service *QueryService) scheduleDashboardRefresh() {
	if service == nil || service.dashboard == nil {
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
		if refresher, ok := service.dashboard.(conditionalRefresher); ok {
			performed, err = refresher.RefreshIfDue(ctx)
		} else {
			err = service.dashboard.Refresh(ctx)
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

func (service *QueryService) scheduleGrokBotRefresh() {
	if service == nil {
		return
	}
	service.refreshMu.Lock()
	if service.grokBot == nil || service.grokRefreshing {
		service.refreshMu.Unlock()
		return
	}
	service.grokRefreshing = true
	refresher := service.grokBot
	service.refreshMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		performed := true
		var err error
		if conditional, ok := refresher.(conditionalRefresher); ok {
			performed, err = conditional.RefreshIfDue(ctx)
		} else {
			err = refresher.Refresh(ctx)
		}
		service.refreshMu.Lock()
		service.grokRefreshing = false
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
		return usagecost.UsageCostResponse{}, fmt.Errorf("%w: cursor usage request", basequery.ErrValidation)
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	events := usageEventsInRange(snapshot.UsageEvents, *rangeValue)
	requests := requestEventsInRange(snapshot.RequestEvents, *rangeValue)
	exactUsage := exactUsageAvailable(snapshot)
	dashboardEvents, dashboardAvailable, dashboardStale := dashboardEventsInRange(snapshot, *rangeValue)
	response := usagecost.UsageCostResponse{
		ProviderContext:   contextFor(snapshot),
		Meta:              completeMeta(nil),
		Range:             *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone,
		PricingVersions:   []string{},
		Trend:             []usagecost.TrendPoint{},
		Models:            []usagecost.UsageModelItem{},
		UnpricedReasons:   []usagecost.ReasonCount{},
		CursorUsagePools:  []usagecost.CursorUsagePoolSummary{},
	}
	if dashboardAvailable {
		response.Totals = totalsForDashboardEvents(dashboardEvents)
		response.CursorUsagePools = cursorUsagePoolSummaries(dashboardEvents)
		response.Trend = dashboardUsageTrend(dashboardEvents, *rangeValue, request.Granularity)
		if request.IncludeActivityDistribution {
			response.ActivityDistribution = dashboardActivityDistribution(dashboardEvents, *rangeValue)
		}
		reported := known(reportedTotalForDashboardEvents(dashboardEvents), basequery.NumericMicroUSD)
		cursorTokenFee := known(cursorTokenFeeTotalForDashboardEvents(dashboardEvents), basequery.NumericMicroUSD)
		dataAsOf := known(snapshot.DashboardWindowEndMS, basequery.NumericMilliseconds)
		reportedSource := SourceDashboard
		response.ReportedUSDMicros = &reported
		response.CursorTokenFeeUSDMicros = &cursorTokenFee
		response.ReportedCostSource = &reportedSource
		response.DataAsOfMS = &dataAsOf
		if snapshot.DashboardPlanUsage != nil {
			response.CursorBilling = cursorBillingSummary(snapshot)
		}
		if dashboardStale {
			response.Meta = partialMeta(nil)
		}
		currency := "USD"
		response.Currency = &currency
		if response.Totals.EstimatedUSDMicros.Value != nil {
			pricingSource := cursorPricingSourceURL
			response.PricingSource = &pricingSource
			response.PricingVersions = []string{cursorPricingVersion}
		}
	} else {
		response.Totals = totalsForUsageEvents(events, exactUsage)
		response.Totals.TurnCount = known(int64(len(requests)), basequery.NumericCount)
		response.Trend = usageTrend(events, *rangeValue, request.Granularity, exactUsage)
		if request.IncludeActivityDistribution && exactUsage {
			response.ActivityDistribution = localActivityDistribution(events, *rangeValue)
		}
	}
	if !request.TokenTotalsOnly {
		if dashboardAvailable {
			response.Models = dashboardUsageModels(dashboardEvents, *rangeValue, request.Granularity)
		} else {
			response.Models = usageModels(events, *rangeValue, request.Granularity, exactUsage)
		}
		if !dashboardAvailable && len(events) > 0 {
			response.UnpricedReasons = []usagecost.ReasonCount{{
				Reason: pricing.CostReasonMissingPriceComponent,
				Count:  known(int64(len(events)), basequery.NumericCount),
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
	sessions, err := cursorSessions(snapshot, validated)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	usageBySession := groupUsageBySession(snapshot.UsageEvents)
	exactUsage := exactUsageAvailable(snapshot)
	dashboardRange := dereferenceRange(validated.TimeRange, snapshot)
	dashboardEvents, dashboardAvailable, dashboardPartial := dashboardEventsInRange(snapshot, dashboardRange)
	dashboardBySession, dashboardAttributable := attributableDashboardEvents(snapshot.Sessions, dashboardEvents)
	useDashboard := dashboardAvailable && dashboardAttributable
	sortCursorSessions(sessions, validated.Sort[0], usageBySession, exactUsage, dashboardBySession, useDashboard)
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	items := make([]usagecost.SessionItem, 0, minInt(limit, maxInt(0, len(sessions)-offset)))
	for _, session := range slicePage(sessions, offset, limit) {
		if dashboardAvailable && len(dashboardBySession[session.ExternalSessionID]) > 0 {
			items = append(items, dashboardSessionItem(session, dashboardBySession[session.ExternalSessionID]))
		} else {
			items = append(items, sessionItem(session, usageBySession[session.ExternalSessionID], exactUsage))
		}
	}
	next := pageInfo(offset, limit, len(sessions), snapshot.Generation, queryFingerprint(validated))
	allEvents := make([]store.CursorUsageEvent, 0)
	pageEvents := make([]store.CursorUsageEvent, 0)
	for _, session := range sessions {
		allEvents = append(allEvents, usageBySession[session.ExternalSessionID]...)
	}
	for _, item := range slicePage(sessions, offset, limit) {
		pageEvents = append(pageEvents, usageBySession[item.ExternalSessionID]...)
	}
	pageSessions := slicePage(sessions, offset, limit)
	matchedTotals := totalsForUsageEvents(allEvents, exactUsage)
	pageTotals := totalsForUsageEvents(pageEvents, exactUsage)
	if useDashboard {
		matchedTotals = totalsForDashboardEvents(dashboardEventsForSessions(dashboardBySession, sessions))
		pageTotals = totalsForDashboardEvents(dashboardEventsForSessions(dashboardBySession, pageSessions))
	} else {
		matchedTotals.TurnCount = known(cursorSessionRequestCount(sessions), basequery.NumericCount)
		pageTotals.TurnCount = known(cursorSessionRequestCount(pageSessions), basequery.NumericCount)
	}
	meta := completeMeta(next)
	if dashboardAvailable && (dashboardPartial || !dashboardAttributable) {
		meta = partialMeta(next)
	}
	return usagecost.SessionListResponse{
		ProviderContext: contextFor(snapshot), Meta: meta, Items: items,
		MatchedCount:  known(int64(len(sessions)), basequery.NumericCount),
		MatchedTotals: matchedTotals, PageTotals: pageTotals,
	}, nil
}

func (service *QueryService) SessionDetail(ctx context.Context, request usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	var session *store.CursorSession
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
	dashboardRange := rangeValue
	if snapshot.DashboardGeneration > 0 && snapshot.DashboardWindowEndMS > snapshot.DashboardWindowStartMS {
		dashboardRange = basequery.UTCTimeRange{
			StartAtMS: snapshot.DashboardWindowStartMS, EndAtMS: snapshot.DashboardWindowEndMS,
			TimeZone: timezone,
		}
	}
	dashboardEvents, dashboardAvailable, _ := dashboardEventsInRange(snapshot, dashboardRange)
	dashboardPartial := dashboardAvailable && (snapshot.DashboardWindowStartMS > session.CreatedAtMS ||
		snapshot.DashboardWindowEndMS <= session.LastActivityAtMS)
	dashboardBySession, dashboardAttributable := attributableDashboardEvents(snapshot.Sessions, dashboardEvents)
	sessionDashboardEvents := dashboardBySession[session.ExternalSessionID]
	useDashboard := dashboardAvailable && len(sessionDashboardEvents) > 0
	if useDashboard {
		for _, event := range sessionDashboardEvents {
			rangeValue.StartAtMS = minPositive(rangeValue.StartAtMS, event.OccurredAtMS)
			rangeValue.EndAtMS = maxInt64(rangeValue.EndAtMS, event.OccurredAtMS+1)
		}
	}
	events := groupUsageBySession(snapshot.UsageEvents)[session.ExternalSessionID]
	exactUsage := exactUsageAvailable(snapshot)
	sort.Slice(events, func(i, j int) bool { return events[i].OccurredAtMS < events[j].OccurredAtMS })
	requests := requestEventsForSession(snapshot.RequestEvents, session.ExternalSessionID)
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].OccurredAtMS != requests[j].OccurredAtMS {
			return requests[i].OccurredAtMS < requests[j].OccurredAtMS
		}
		return requests[i].EventID < requests[j].EventID
	})
	offset, limit, err := page(
		request.TurnPage,
		snapshot.Generation,
		digestString("session-turns:" + session.ExternalSessionID)[:16],
	)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	turns := make([]usagecost.SessionTurnItem, 0)
	usageByID := make(map[string]store.CursorUsageEvent, len(events))
	for _, event := range events {
		usageByID[event.EventID] = event
	}
	for _, requestEvent := range slicePage(requests, offset, limit) {
		usageEvent, hasUsage := usageByID[requestEvent.EventID]
		turnTotals := unavailableUsageTotals()
		model := modelAttribution(nil)
		if hasUsage {
			turnTotals = totalsForUsageEvents([]store.CursorUsageEvent{usageEvent}, exactUsage)
			model = modelAttribution(usageEvent.ModelKey)
		}
		turnTotals.TurnCount = known(1, basequery.NumericCount)
		turns = append(turns, usagecost.SessionTurnItem{
			TimelineKey: requestEvent.EventID, State: usagecost.SessionTurnComplete,
			Model: model, StartedAt: known(requestEvent.OccurredAtMS, basequery.NumericMilliseconds),
			CompletedAt: known(requestEvent.OccurredAtMS, basequery.NumericMilliseconds),
			ObservedAt:  known(requestEvent.OccurredAtMS, basequery.NumericMilliseconds), Totals: turnTotals,
			PricingStatus:  usagecost.SessionTurnPricingUnpriced,
			UnpricedReason: pointerCostReason(pricing.CostReasonMissingPriceComponent),
		})
	}
	turnPage := pageInfo(
		offset,
		limit,
		len(requests),
		snapshot.Generation,
		digestString("session-turns:" + session.ExternalSessionID)[:16],
	)
	item := sessionItem(*session, events, exactUsage)
	models := usageModels(events, rangeValue, usagecost.TrendDay, exactUsage)
	trend := usageTrend(events, rangeValue, usagecost.TrendDay, exactUsage)
	meta := completeMeta(nil)
	pricingVersions := []string{}
	var pricingSource, currency *string
	if useDashboard {
		item = dashboardSessionItem(*session, sessionDashboardEvents)
		models = dashboardUsageModels(sessionDashboardEvents, rangeValue, usagecost.TrendDay)
		trend = dashboardUsageTrend(sessionDashboardEvents, rangeValue, usagecost.TrendDay)
		if dashboardPartial || !dashboardAttributable {
			meta = partialMeta(nil)
		}
		if item.Totals.EstimatedUSDMicros.Value != nil {
			source, currencyValue := cursorPricingSourceURL, "USD"
			pricingSource, currency = &source, &currencyValue
			pricingVersions = []string{cursorPricingVersion}
		}
	}
	return usagecost.SessionDetailResponse{
		ProviderContext: contextFor(snapshot), Meta: meta, PricingSource: pricingSource, Currency: currency,
		PricingVersions: pricingVersions,
		UnpricedReasons: []usagecost.ReasonCount{}, Item: item, TurnPage: *turnPage,
		Turns: turns, ReportingTimeZone: timezone, TrendGranularity: usagecost.TrendDay,
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
	sessions, err := cursorSessionsForProjects(snapshot, validated)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	responseRange := *validated.TimeRange
	rangeEvents := usageEventsInRange(snapshot.UsageEvents, responseRange)
	rangeRequests := requestEventsInRange(snapshot.RequestEvents, responseRange)
	dashboardEvents, dashboardAvailable, dashboardPartial := dashboardEventsInRange(snapshot, responseRange)
	dashboardBySession, dashboardAttributable := attributableDashboardEvents(snapshot.Sessions, dashboardEvents)
	useDashboard := dashboardAvailable && dashboardAttributable
	groups := projectGroups(sessions, rangeEvents, rangeRequests, dashboardBySession)
	exactUsage := exactUsageAvailable(snapshot)
	sortCursorProjects(groups, validated.Sort[0], exactUsage, useDashboard)
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	items := make([]usagecost.ProjectItem, 0)
	for _, group := range slicePage(groups, offset, limit) {
		items = append(items, projectItem(group, exactUsage, useDashboard))
	}
	globalEvents := rangeEvents
	globalRequests := rangeRequests
	pageEvents := make([]store.CursorUsageEvent, 0)
	for _, group := range slicePage(groups, offset, limit) {
		pageEvents = append(pageEvents, group.events...)
	}
	pageRequests := make([]store.CursorRequestEvent, 0)
	for _, group := range slicePage(groups, offset, limit) {
		pageRequests = append(pageRequests, group.requests...)
	}
	globalTotals := totalsForUsageEvents(globalEvents, exactUsage)
	pageTotals := totalsForUsageEvents(pageEvents, exactUsage)
	if useDashboard {
		globalTotals = totalsForDashboardEvents(dashboardEventsForProjectGroups(groups))
		pageTotals = totalsForDashboardEvents(dashboardEventsForProjectGroups(slicePage(groups, offset, limit)))
	} else {
		globalTotals.TurnCount = known(int64(len(globalRequests)), basequery.NumericCount)
		pageTotals.TurnCount = known(int64(len(pageRequests)), basequery.NumericCount)
	}
	meta := completeMeta(pageInfo(offset, limit, len(groups), snapshot.Generation, queryFingerprint(validated)))
	if useDashboard && dashboardPartial {
		meta = partialMeta(meta.Page)
	}
	return usagecost.ProjectListResponse{
		ProviderContext: contextFor(snapshot), Meta: meta,
		Range: responseRange, ReportingTimeZone: responseRange.TimeZone, PricingVersions: []string{}, Items: items,
		MatchedCount: known(int64(len(groups)), basequery.NumericCount), GlobalTotals: globalTotals,
		MatchedTotals: globalTotals, PageTotals: pageTotals,
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
	exactUsage := exactUsageAvailable(snapshot)
	rangeRequests := requestEventsInRange(snapshot.RequestEvents, *rangeValue)
	dashboardEvents, dashboardAvailable, dashboardPartial := dashboardEventsInRange(snapshot, *rangeValue)
	dashboardBySession, dashboardAttributable := attributableDashboardEvents(snapshot.Sessions, dashboardEvents)
	useDashboard := dashboardAvailable && dashboardAttributable
	groups := projectGroups(filterSessions(snapshot.Sessions, rangeValue), rangeEvents, rangeRequests, dashboardBySession)
	var selected *cursorProjectGroup
	for index := range groups {
		if groups[index].key == request.DimensionKey {
			selected = &groups[index]
			break
		}
	}
	if selected == nil {
		return usagecost.ProjectDetailResponse{}, basequery.NewNotFoundFailure(nil)
	}
	sessionOffset, sessionLimit, err := page(
		request.SessionPage,
		snapshot.Generation,
		digestString("project-sessions:" + request.DimensionKey)[:16],
	)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	modelOffset, modelLimit, err := page(
		request.ModelPage,
		snapshot.Generation,
		digestString("project-models:" + request.DimensionKey)[:16],
	)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	usageBySession := groupUsageBySession(selected.events)
	sessionItems := make([]usagecost.ProjectSessionItem, 0)
	for _, session := range slicePage(selected.sessions, sessionOffset, sessionLimit) {
		item := sessionItem(session, usageBySession[session.ExternalSessionID], exactUsage)
		if useDashboard {
			item = dashboardSessionItem(session, dashboardBySession[session.ExternalSessionID])
		}
		sessionItems = append(sessionItems, usagecost.ProjectSessionItem{
			SessionID: item.SessionID, DisplayTitle: item.DisplayTitle, TitleConfidence: item.TitleConfidence,
			TitleSource: item.TitleSource, TitleReason: item.TitleReason, Model: item.Model,
			Activity: item.Activity, LastActivityAt: item.LastActivityAt, Totals: item.Totals,
		})
	}
	models := usageModels(selected.events, *rangeValue, usagecost.TrendDay, exactUsage)
	if useDashboard {
		models = dashboardUsageModels(selected.dashboardEvents, *rangeValue, usagecost.TrendDay)
	}
	modelItems := make([]usagecost.ProjectModelItem, 0)
	for _, item := range slicePage(models, modelOffset, modelLimit) {
		modelItems = append(modelItems, usagecost.ProjectModelItem{DimensionKey: item.DimensionKey, Model: item.Model, Totals: item.Totals})
	}
	globalTotals := totalsForUsageEvents(usageEventsInRange(snapshot.UsageEvents, *rangeValue), exactUsage)
	if useDashboard {
		globalTotals = totalsForDashboardEvents(dashboardEvents)
	} else {
		globalTotals.TurnCount = known(int64(len(rangeRequests)), basequery.NumericCount)
	}
	meta := completeMeta(nil)
	if useDashboard && dashboardPartial {
		meta = partialMeta(nil)
	}
	daily := projectDaily(selected.events, *rangeValue, exactUsage)
	if useDashboard {
		daily = dashboardProjectDaily(selected.dashboardEvents, *rangeValue)
	}
	return usagecost.ProjectDetailResponse{
		ProviderContext: contextFor(snapshot), Meta: meta, Range: *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone, PricingVersions: []string{}, Item: projectItem(*selected, exactUsage, useDashboard),
		Daily:       daily,
		SessionPage: *pageInfo(sessionOffset, sessionLimit, len(selected.sessions), snapshot.Generation, digestString("project-sessions:" + request.DimensionKey)[:16]), Sessions: sessionItems,
		ModelPage: *pageInfo(modelOffset, modelLimit, len(models), snapshot.Generation, digestString("project-models:" + request.DimensionKey)[:16]), Models: modelItems,
		GlobalTotals: globalTotals,
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
	tools := make([]store.CursorToolEvent, 0)
	if request.SourceClass != invocationusage.SourceClassDetected {
		for _, event := range snapshot.ToolEvents {
			if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
				tools = append(tools, event)
			}
		}
	}
	return invocationResponse(snapshot, *rangeValue, request, tools), nil
}

func contextFor(snapshot store.CursorSnapshot) agentprovider.Context {
	context := agentprovider.Context{
		EffectiveProvider: agentprovider.Cursor,
		Capabilities:      []string{"sessions", "projects", "models", "tools", "ai_edits", "requests"},
		Coverage:          []agentprovider.Coverage{},
	}
	tokensAvailable := false
	if len(snapshot.DashboardQuotaObservations) > 0 {
		context.Capabilities = append(context.Capabilities, "quota")
	}
	for _, source := range snapshot.Sources {
		context.Sources = append(context.Sources, source.SourceKey)
		capability := sourceCapability(source.SourceKey)
		context.Coverage = append(context.Coverage, agentprovider.Coverage{
			Capability: capability, State: source.State, Source: source.SourceKey, Reason: source.CoverageState,
			ItemCount: &source.RowCount,
		})
		if source.SourceKey == SourceState && source.State == "available" {
			tokensAvailable = true
		}
		if source.SourceKey == SourceDashboardGrokBot && !containsString(context.Capabilities, "quota") {
			context.Capabilities = append(context.Capabilities, "quota")
		}
		if source.SourceKey == SourceDashboard && source.LastSuccessAtMS != nil {
			tokensAvailable = true
			if !containsString(context.Capabilities, "reported_cost") {
				context.Capabilities = append(context.Capabilities, "reported_cost")
			}
			if !containsString(context.Capabilities, "estimated_cost") {
				context.Capabilities = append(context.Capabilities, "estimated_cost")
			}
		}
	}
	if tokensAvailable {
		context.Capabilities = append(context.Capabilities, "tokens")
	}
	return context
}

func sourceCapability(source string) string {
	switch source {
	case SourceTranscripts, SourceConversationSearch:
		return "sessions"
	case SourceState:
		return "tokens"
	case SourceAITracking:
		return "ai_edits"
	case SourceHooks:
		return "realtime"
	case SourceDashboard:
		return "reported_cost"
	case SourceDashboardGrokBot:
		return "quota"
	default:
		return "unknown"
	}
}

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

func usageEventsInRange(events []store.CursorUsageEvent, rangeValue basequery.UTCTimeRange) []store.CursorUsageEvent {
	result := make([]store.CursorUsageEvent, 0)
	for _, event := range events {
		if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
			result = append(result, event)
		}
	}
	return result
}

func dashboardEventsInRange(snapshot store.CursorSnapshot, rangeValue basequery.UTCTimeRange) ([]store.CursorDashboardUsageEvent, bool, bool) {
	if snapshot.DashboardGeneration <= 0 || snapshot.DashboardWindowStartMS >= rangeValue.EndAtMS ||
		snapshot.DashboardWindowEndMS <= rangeValue.StartAtMS {
		return nil, false, false
	}
	result := make([]store.CursorDashboardUsageEvent, 0)
	for _, event := range snapshot.DashboardUsageEvents {
		if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
			result = append(result, event)
		}
	}
	partial := snapshot.DashboardWindowStartMS > rangeValue.StartAtMS ||
		snapshot.DashboardWindowEndMS < rangeValue.EndAtMS
	return result, true, partial
}

func reportedTotalForDashboardEvents(events []store.CursorDashboardUsageEvent) int64 {
	var total int64
	for _, event := range events {
		total += event.ReportedChargeMicros * event.OccurrenceCount
	}
	return total
}

func cursorTokenFeeTotalForDashboardEvents(events []store.CursorDashboardUsageEvent) int64 {
	var total int64
	for _, event := range events {
		total += event.CursorTokenFeeMicros * event.OccurrenceCount
	}
	return total
}

func cursorUsagePoolSummaries(events []store.CursorDashboardUsageEvent) []usagecost.CursorUsagePoolSummary {
	eventsByPool := map[string][]store.CursorDashboardUsageEvent{
		pricing.CursorUsagePoolModels:      {},
		pricing.CursorUsagePoolOtherModels: {},
	}
	for _, event := range events {
		poolID := pricing.CursorUsagePoolUnknown
		if event.ModelKey != nil {
			poolID = pricing.CursorUsagePoolForModel(*event.ModelKey, event.OccurredAtMS)
		}
		eventsByPool[poolID] = append(eventsByPool[poolID], event)
	}
	poolIDs := []string{pricing.CursorUsagePoolModels, pricing.CursorUsagePoolOtherModels}
	if len(eventsByPool[pricing.CursorUsagePoolUnknown]) > 0 {
		poolIDs = append(poolIDs, pricing.CursorUsagePoolUnknown)
	}
	result := make([]usagecost.CursorUsagePoolSummary, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		poolEvents := eventsByPool[poolID]
		result = append(result, usagecost.CursorUsagePoolSummary{
			PoolID:                  poolID,
			Totals:                  totalsForDashboardEvents(poolEvents),
			ReportedUSDMicros:       known(reportedTotalForDashboardEvents(poolEvents), basequery.NumericMicroUSD),
			CursorTokenFeeUSDMicros: known(cursorTokenFeeTotalForDashboardEvents(poolEvents), basequery.NumericMicroUSD),
		})
	}
	return result
}

func cursorBillingSummary(snapshot store.CursorSnapshot) *usagecost.CursorBillingSummary {
	usage := snapshot.DashboardPlanUsage
	if usage == nil {
		return nil
	}
	return &usagecost.CursorBillingSummary{
		BillingCycleStartAtMS: known(snapshot.DashboardWindowStartMS, basequery.NumericMilliseconds),
		BillingCycleEndAtMS:   known(snapshot.DashboardBillingCycleEndMS, basequery.NumericMilliseconds),
		TotalSpendUSDMicros:   known(usage.TotalSpendMicros, basequery.NumericMicroUSD),
		IncludedUSDMicros:     known(usage.IncludedSpendMicros, basequery.NumericMicroUSD),
		BonusUSDMicros:        known(usage.BonusSpendMicros, basequery.NumericMicroUSD),
		RemainingUSDMicros:    known(usage.RemainingMicros, basequery.NumericMicroUSD),
		LimitUSDMicros:        known(usage.LimitMicros, basequery.NumericMicroUSD),
	}
}

func totalsForDashboardEvents(events []store.CursorDashboardUsageEvent) usagecost.UsageTotals {
	var turns, input, output, cacheWrite, cacheRead, first, last int64
	for _, event := range events {
		turns += event.OccurrenceCount
		input += event.InputTokens * event.OccurrenceCount
		output += event.OutputTokens * event.OccurrenceCount
		cacheWrite += event.CacheWriteTokens * event.OccurrenceCount
		cacheRead += event.CacheReadTokens * event.OccurrenceCount
		first = minPositive(first, event.OccurredAtMS)
		last = maxInt64(last, event.OccurredAtMS)
	}
	estimated := unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	pricedTurns, unpricedTurns := int64(0), turns
	if value, _, ok := estimateCursorDashboardCost(events); ok {
		estimated = known(value, basequery.NumericMicroUSD)
		pricedTurns, unpricedTurns = turns, 0
	}
	firstActivity, lastActivity := activityBoundaries(turns, first, last)
	return usagecost.UsageTotals{
		TurnCount: known(turns, basequery.NumericCount), InputTokens: known(input+cacheWrite, basequery.NumericTokens),
		CachedInputTokens: known(cacheRead, basequery.NumericTokens), OutputTokens: known(output, basequery.NumericTokens),
		ReasoningTokens:    unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		TotalTokens:        known(input+output+cacheWrite+cacheRead, basequery.NumericTokens),
		EstimatedUSDMicros: estimated,
		PricedTurnCount:    known(pricedTurns, basequery.NumericCount), UnpricedTurnCount: known(unpricedTurns, basequery.NumericCount),
		FirstActivityAtMS: firstActivity, LastActivityAtMS: lastActivity,
	}
}

func dashboardUsageTrend(events []store.CursorDashboardUsageEvent, rangeValue basequery.UTCTimeRange, granularity usagecost.TrendGranularity) []usagecost.TrendPoint {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return []usagecost.TrendPoint{}
	}
	type dashboardBucket struct {
		start, end time.Time
		events     []store.CursorDashboardUsageEvent
	}
	groups := make(map[int64]*dashboardBucket)
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
		end := start.Add(time.Hour)
		switch granularity {
		case usagecost.TrendDay:
			end = start.AddDate(0, 0, 1)
		case usagecost.TrendWeek:
			end = start.AddDate(0, 0, 7)
		case usagecost.TrendMonth:
			end = start.AddDate(0, 1, 0)
		}
		key := start.UnixMilli()
		if groups[key] == nil {
			groups[key] = &dashboardBucket{start: start, end: end}
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
		result = append(result, usagecost.TrendPoint{
			Key: cursorTrendKey(value.start, granularity), StartAtMS: known(value.start.UnixMilli(), basequery.NumericMilliseconds),
			EndAtMS: known(value.end.UnixMilli(), basequery.NumericMilliseconds), Totals: totalsForDashboardEvents(value.events),
		})
	}
	return result
}

type cursorActivityEvent struct {
	occurredAtMS int64
	totalTokens  int64
	sessionID    string
	hasSessionID bool
}

type cursorActivityBucket struct {
	startAtMS       int64
	endAtMS         int64
	totalTokens     int64
	sessionIDs      map[string]struct{}
	sessionsUnknown bool
}

func dashboardActivityDistribution(
	events []store.CursorDashboardUsageEvent,
	rangeValue basequery.UTCTimeRange,
) *usagecost.ActivityDistribution {
	activityEvents := make([]cursorActivityEvent, 0, len(events))
	for _, event := range events {
		sessionID := ""
		if event.ExternalSessionID != nil {
			sessionID = strings.TrimSpace(*event.ExternalSessionID)
		}
		activityEvents = append(activityEvents, cursorActivityEvent{
			occurredAtMS: event.OccurredAtMS,
			totalTokens:  (event.InputTokens + event.OutputTokens + event.CacheWriteTokens + event.CacheReadTokens) * event.OccurrenceCount,
			sessionID:    sessionID, hasSessionID: sessionID != "",
		})
	}
	return buildCursorActivityDistribution(activityEvents, rangeValue)
}

func localActivityDistribution(
	events []store.CursorUsageEvent,
	rangeValue basequery.UTCTimeRange,
) *usagecost.ActivityDistribution {
	activityEvents := make([]cursorActivityEvent, 0, len(events))
	for _, event := range events {
		sessionID := strings.TrimSpace(event.ExternalSessionID)
		activityEvents = append(activityEvents, cursorActivityEvent{
			occurredAtMS: event.OccurredAtMS,
			totalTokens:  event.InputTokens + event.OutputTokens,
			sessionID:    sessionID, hasSessionID: sessionID != "",
		})
	}
	return buildCursorActivityDistribution(activityEvents, rangeValue)
}

func buildCursorActivityDistribution(
	events []cursorActivityEvent,
	rangeValue basequery.UTCTimeRange,
) *usagecost.ActivityDistribution {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return nil
	}
	bucketMinutes := adaptiveCursorActivityBucketMinutes(rangeValue)
	timelineBuckets := make(map[int64]*cursorActivityBucket)
	weekdayHourBuckets := make(map[int]*cursorActivityBucket)
	for _, event := range events {
		at := time.UnixMilli(event.occurredAtMS).In(location)
		bucketStart := cursorActivityBucketStart(at, bucketMinutes)
		startAtMS := maxInt64(bucketStart.UnixMilli(), rangeValue.StartAtMS)
		endAtMS := cursorActivityBucketEnd(bucketStart, bucketMinutes).UnixMilli()
		if endAtMS > rangeValue.EndAtMS {
			endAtMS = rangeValue.EndAtMS
		}
		bucket := timelineBuckets[startAtMS]
		if bucket == nil {
			bucket = &cursorActivityBucket{
				startAtMS: startAtMS, endAtMS: endAtMS, sessionIDs: make(map[string]struct{}),
			}
			timelineBuckets[startAtMS] = bucket
		}
		addCursorActivityEvent(bucket, event)

		weekday := int(at.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		cellKey := weekday*24 + at.Hour()
		cell := weekdayHourBuckets[cellKey]
		if cell == nil {
			cell = &cursorActivityBucket{sessionIDs: make(map[string]struct{})}
			weekdayHourBuckets[cellKey] = cell
		}
		addCursorActivityEvent(cell, event)
	}

	result := &usagecost.ActivityDistribution{
		TimelineGranularity: usagecost.TrendHour, TimelineBucketMinutes: int32(bucketMinutes),
		Timeline: []usagecost.ActivityTimelinePoint{}, WeekdayHours: []usagecost.ActivityWeekdayHourPoint{},
	}
	if bucketMinutes == 1_440 {
		result.TimelineGranularity = usagecost.TrendDay
	}
	timelineKeys := make([]int64, 0, len(timelineBuckets))
	for key := range timelineBuckets {
		timelineKeys = append(timelineKeys, key)
	}
	sort.Slice(timelineKeys, func(left, right int) bool { return timelineKeys[left] < timelineKeys[right] })
	for _, key := range timelineKeys {
		bucket := timelineBuckets[key]
		result.Timeline = append(result.Timeline, usagecost.ActivityTimelinePoint{
			StartAtMS: known(bucket.startAtMS, basequery.NumericMilliseconds),
			EndAtMS:   known(bucket.endAtMS, basequery.NumericMilliseconds),
			Metrics:   cursorActivityMetrics(bucket),
		})
	}
	weekdayHourKeys := make([]int, 0, len(weekdayHourBuckets))
	for key := range weekdayHourBuckets {
		weekdayHourKeys = append(weekdayHourKeys, key)
	}
	sort.Ints(weekdayHourKeys)
	for _, key := range weekdayHourKeys {
		bucket := weekdayHourBuckets[key]
		result.WeekdayHours = append(result.WeekdayHours, usagecost.ActivityWeekdayHourPoint{
			Weekday: key / 24, Hour: key % 24, Metrics: cursorActivityMetrics(bucket),
		})
	}
	return result
}

func addCursorActivityEvent(bucket *cursorActivityBucket, event cursorActivityEvent) {
	bucket.totalTokens += event.totalTokens
	if event.hasSessionID {
		bucket.sessionIDs[event.sessionID] = struct{}{}
	} else {
		bucket.sessionsUnknown = true
	}
}

func cursorActivityMetrics(bucket *cursorActivityBucket) usagecost.ActivityMetrics {
	sessions := known(int64(len(bucket.sessionIDs)), basequery.NumericCount)
	if bucket.sessionsUnknown {
		sessions = unknown(basequery.NumericCount, basequery.UnknownUnavailable)
	}
	return usagecost.ActivityMetrics{
		TotalTokens: known(bucket.totalTokens, basequery.NumericTokens), SessionCount: sessions,
	}
}

func cursorActivityBucketStart(at time.Time, bucketMinutes int) time.Time {
	if bucketMinutes == 1_440 {
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	}
	minutesSinceMidnight := at.Hour()*60 + at.Minute()
	startMinutes := minutesSinceMidnight - minutesSinceMidnight%bucketMinutes
	return time.Date(at.Year(), at.Month(), at.Day(), startMinutes/60, startMinutes%60, 0, 0, at.Location())
}

func cursorActivityBucketEnd(start time.Time, bucketMinutes int) time.Time {
	if bucketMinutes == 1_440 {
		return start.AddDate(0, 0, 1)
	}
	return start.Add(time.Duration(bucketMinutes) * time.Minute)
}

func adaptiveCursorActivityBucketMinutes(rangeValue basequery.UTCTimeRange) int {
	durationMS := rangeValue.EndAtMS - rangeValue.StartAtMS
	selected := activityBucketMinuteOptions[0]
	bestDistance := int(^uint(0) >> 1)
	found := false
	for _, minutes := range activityBucketMinuteOptions {
		bucketMS := int64(minutes) * int64(time.Minute/time.Millisecond)
		count := int((durationMS + bucketMS - 1) / bucketMS)
		if count < activityBucketMinimumCount || count > activityBucketMaximumCount {
			continue
		}
		distance := count - activityBucketTargetCount
		if distance < 0 {
			distance = -distance
		}
		if !found || distance < bestDistance {
			selected = minutes
			bestDistance = distance
			found = true
		}
	}
	if found {
		return selected
	}
	finestBucketMS := int64(activityBucketMinuteOptions[0]) * int64(time.Minute/time.Millisecond)
	if (durationMS+finestBucketMS-1)/finestBucketMS < activityBucketMinimumCount {
		return activityBucketMinuteOptions[0]
	}
	return activityBucketMinuteOptions[len(activityBucketMinuteOptions)-1]
}

func dashboardUsageModels(
	events []store.CursorDashboardUsageEvent,
	rangeValue basequery.UTCTimeRange,
	granularity usagecost.TrendGranularity,
) []usagecost.UsageModelItem {
	groups := make(map[string][]store.CursorDashboardUsageEvent)
	for _, event := range events {
		key := "unknown"
		if event.ModelKey != nil {
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
		result = append(result, usagecost.UsageModelItem{
			DimensionKey: key, Model: modelAttribution(model), Totals: totalsForDashboardEvents(groups[key]),
			Trend: dashboardUsageTrend(groups[key], rangeValue, granularity),
		})
	}
	return result
}

func requestEventsInRange(events []store.CursorRequestEvent, rangeValue basequery.UTCTimeRange) []store.CursorRequestEvent {
	result := make([]store.CursorRequestEvent, 0)
	for _, event := range events {
		if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
			result = append(result, event)
		}
	}
	return result
}

func totalsForUsageEvents(events []store.CursorUsageEvent, exact ...bool) usagecost.UsageTotals {
	if len(exact) > 0 && !exact[0] {
		return unavailableUsageTotals()
	}
	var input, output int64
	first, last := int64(0), int64(0)
	for _, event := range events {
		input += event.InputTokens
		output += event.OutputTokens
		first = minPositive(first, event.OccurredAtMS)
		last = maxInt64(last, event.OccurredAtMS)
	}
	count := int64(len(events))
	total := input + output
	firstActivity, lastActivity := activityBoundaries(count, first, last)
	return usagecost.UsageTotals{
		TurnCount: known(count, basequery.NumericCount), InputTokens: known(input, basequery.NumericTokens),
		CachedInputTokens: unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		OutputTokens:      known(output, basequery.NumericTokens), ReasoningTokens: unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		TotalTokens: known(total, basequery.NumericTokens), EstimatedUSDMicros: unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable),
		PricedTurnCount: known(0, basequery.NumericCount), UnpricedTurnCount: known(count, basequery.NumericCount),
		FirstActivityAtMS: firstActivity, LastActivityAtMS: lastActivity,
	}
}

func activityBoundaries(count, first, last int64) (basequery.NumericValue, basequery.NumericValue) {
	if count == 0 {
		return unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable),
			unknown(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	}
	return known(first, basequery.NumericMilliseconds), known(last, basequery.NumericMilliseconds)
}

func unavailableUsageTotals() usagecost.UsageTotals {
	return usagecost.UsageTotals{
		TurnCount:          unknown(basequery.NumericCount, basequery.UnknownUnavailable),
		InputTokens:        unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		CachedInputTokens:  unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		OutputTokens:       unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		ReasoningTokens:    unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		TotalTokens:        unknown(basequery.NumericTokens, basequery.UnknownUnavailable),
		EstimatedUSDMicros: unknown(basequery.NumericMicroUSD, basequery.UnknownUnavailable),
		PricedTurnCount:    unknown(basequery.NumericCount, basequery.UnknownUnavailable),
		UnpricedTurnCount:  unknown(basequery.NumericCount, basequery.UnknownUnavailable),
		FirstActivityAtMS:  unknown(basequery.NumericMilliseconds, basequery.UnknownUnavailable),
		LastActivityAtMS:   unknown(basequery.NumericMilliseconds, basequery.UnknownUnavailable),
	}
}

func exactUsageAvailable(snapshot store.CursorSnapshot) bool {
	for _, source := range snapshot.Sources {
		if source.SourceKey == SourceState && source.State == "available" && source.CoverageState == "exact" {
			return true
		}
	}
	return false
}

func usageTrend(
	events []store.CursorUsageEvent,
	rangeValue basequery.UTCTimeRange,
	granularity usagecost.TrendGranularity,
	exact ...bool,
) []usagecost.TrendPoint {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return []usagecost.TrendPoint{}
	}
	type bucket struct {
		start, end time.Time
		events     []store.CursorUsageEvent
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
		end := start.Add(time.Hour)
		switch granularity {
		case usagecost.TrendDay:
			end = start.AddDate(0, 0, 1)
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
		result = append(result, usagecost.TrendPoint{Key: cursorTrendKey(value.start, granularity), StartAtMS: known(value.start.UnixMilli(), basequery.NumericMilliseconds), EndAtMS: known(value.end.UnixMilli(), basequery.NumericMilliseconds), Totals: totalsForUsageEvents(value.events, exact...)})
	}
	return result
}

func cursorTrendKey(value time.Time, granularity usagecost.TrendGranularity) string {
	switch granularity {
	case usagecost.TrendHour:
		return value.Format("2006-01-02T15:00-07:00")
	case usagecost.TrendDay:
		return value.Format("2006-01-02")
	case usagecost.TrendWeek:
		year, week := value.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	default:
		return value.Format("2006-01")
	}
}

func usageModels(
	events []store.CursorUsageEvent,
	rangeValue basequery.UTCTimeRange,
	granularity usagecost.TrendGranularity,
	exact ...bool,
) []usagecost.UsageModelItem {
	groups := make(map[string][]store.CursorUsageEvent)
	for _, event := range events {
		key := "unknown"
		if event.ModelKey != nil {
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
		result = append(result, usagecost.UsageModelItem{
			DimensionKey: key, Model: modelAttribution(model), Totals: totalsForUsageEvents(groups[key], exact...),
			Trend: usageTrend(groups[key], rangeValue, granularity, exact...),
		})
	}
	return result
}

func sessionItem(session store.CursorSession, events []store.CursorUsageEvent, exact ...bool) usagecost.SessionItem {
	totals := totalsForUsageEvents(events, exact...)
	totals.TurnCount = known(session.RequestCount, basequery.NumericCount)
	return sessionItemWithTotals(session, totals)
}

func dashboardSessionItem(session store.CursorSession, events []store.CursorDashboardUsageEvent) usagecost.SessionItem {
	item := sessionItemWithTotals(session, totalsForDashboardEvents(events))
	item.Model = dashboardModelAttribution(events, session.ModelKey)
	return item
}

func dashboardModelAttribution(
	events []store.CursorDashboardUsageEvent,
	fallback *string,
) usagecost.AttributionValue {
	var selected *store.CursorDashboardUsageEvent
	for index := range events {
		event := &events[index]
		if event.ModelKey == nil || strings.TrimSpace(*event.ModelKey) == "" {
			continue
		}
		if selected == nil || event.OccurredAtMS > selected.OccurredAtMS ||
			(event.OccurredAtMS == selected.OccurredAtMS && event.EventFingerprint < selected.EventFingerprint) {
			selected = event
		}
	}
	if selected == nil {
		return modelAttribution(fallback)
	}
	value := *selected.ModelKey
	return usagecost.AttributionValue{
		ID: &value, DisplayName: &value, Confidence: "high",
		Source: SourceDashboard, Reason: "official_usage_event",
	}
}

func sessionItemWithTotals(session store.CursorSession, totals usagecost.UsageTotals) usagecost.SessionItem {
	displayTitle, titleSource, titleConfidence, titleReason := cursorSessionTitlePresentation(session)
	return usagecost.SessionItem{
		SessionID: session.ExternalSessionID, DisplayTitle: displayTitle,
		TitleConfidence: titleConfidence, TitleSource: titleSource, TitleReason: titleReason,
		Project: usagecost.AttributionValue{ID: &session.ProjectKey, DisplayName: &session.ProjectDisplayName, Confidence: "high", Source: "cursor_workspace", Reason: "path_redacted"},
		Model:   modelAttribution(session.ModelKey), Activity: "idle",
		LastActivityAt: known(session.LastActivityAtMS, basequery.NumericMilliseconds), Totals: totals,
	}
}

func cursorSessionTitlePresentation(session store.CursorSession) (string, string, string, string) {
	if session.DisplayTitle == "" || session.TitleSource == "fallback" {
		title := session.DisplayTitle
		if title == "" {
			title = fallbackSessionTitle(session.LastActivityAtMS)
		}
		return title, "fallback", "low", "data_source_not_provided"
	}
	return session.DisplayTitle, session.TitleSource, "high", "provider_metadata"
}

func modelAttribution(model *string) usagecost.AttributionValue {
	if model == nil || *model == "" {
		return usagecost.AttributionValue{Confidence: "unknown", Source: "missing", Reason: "data_source_not_provided"}
	}
	value := *model
	return usagecost.AttributionValue{ID: &value, DisplayName: &value, Confidence: "high", Source: "cursor_state", Reason: "provider_metadata"}
}

func filterSessions(sessions []store.CursorSession, rangeValue *basequery.UTCTimeRange) []store.CursorSession {
	result := make([]store.CursorSession, 0)
	for _, session := range sessions {
		if rangeValue == nil || (session.LastActivityAtMS >= rangeValue.StartAtMS && session.LastActivityAtMS < rangeValue.EndAtMS) {
			result = append(result, session)
		}
	}
	return result
}

func cursorSessions(
	snapshot store.CursorSnapshot,
	request basequery.ValidatedRequest,
) ([]store.CursorSession, error) {
	result := filterSessions(snapshot.Sessions, request.TimeRange)
	seen := make(map[string]struct{}, len(request.Filters))
	for _, filter := range request.Filters {
		if _, exists := seen[filter.Field]; exists || duplicateStrings(filter.Values) {
			return nil, basequery.NewValidationFailure("filters", nil)
		}
		seen[filter.Field] = struct{}{}
		switch filter.Field {
		case "projectId":
			result = filterCursorSessions(result, func(session store.CursorSession) bool {
				return containsString(filter.Values, session.ProjectKey) ||
					containsString(filter.Values, session.ProjectDisplayName)
			})
		case "modelKey":
			result = filterCursorSessions(result, func(session store.CursorSession) bool {
				return session.ModelKey != nil && containsString(filter.Values, *session.ModelKey)
			})
		case "activity":
			if len(filter.Values) != 1 || (filter.Values[0] != "active" && filter.Values[0] != "idle") {
				return nil, basequery.NewValidationFailure("filters.values", nil)
			}
			if filter.Values[0] == "active" {
				result = []store.CursorSession{}
			}
		default:
			return nil, basequery.NewValidationFailure("filters.field", nil)
		}
	}
	return result, nil
}

func cursorSessionsForProjects(
	snapshot store.CursorSnapshot,
	request basequery.ValidatedRequest,
) ([]store.CursorSession, error) {
	result := filterSessions(snapshot.Sessions, request.TimeRange)
	seen := make(map[string]struct{}, len(request.Filters))
	for _, filter := range request.Filters {
		if _, exists := seen[filter.Field]; exists || duplicateStrings(filter.Values) {
			return nil, basequery.NewValidationFailure("filters", nil)
		}
		seen[filter.Field] = struct{}{}
		switch filter.Field {
		case "projectId":
			result = filterCursorSessions(result, func(session store.CursorSession) bool {
				projectKey, projectName, _, _, _ := cursorProjectGroupIdentity(session)
				return containsString(filter.Values, projectKey) || containsString(filter.Values, projectName)
			})
		case "confidence":
			for _, value := range filter.Values {
				if value != "high" && value != "medium" && value != "low" && value != "unknown" {
					return nil, basequery.NewValidationFailure("filters.values", nil)
				}
			}
			result = filterCursorSessions(result, func(session store.CursorSession) bool {
				_, _, confidence, _, _ := cursorProjectGroupIdentity(session)
				return containsString(filter.Values, confidence)
			})
		default:
			return nil, basequery.NewValidationFailure("filters.field", nil)
		}
	}
	return result, nil
}

func filterCursorSessions(
	sessions []store.CursorSession,
	keep func(store.CursorSession) bool,
) []store.CursorSession {
	result := make([]store.CursorSession, 0, len(sessions))
	for _, session := range sessions {
		if keep(session) {
			result = append(result, session)
		}
	}
	return result
}

func sortCursorSessions(
	sessions []store.CursorSession,
	primary basequery.SortTerm,
	usage map[string][]store.CursorUsageEvent,
	exactUsage bool,
	dashboard map[string][]store.CursorDashboardUsageEvent,
	useDashboard bool,
) {
	sort.SliceStable(sessions, func(i, j int) bool {
		left, right := sessions[i], sessions[j]
		comparison := 0
		switch primary.Field {
		case "lastActivityAt":
			comparison = compareInt64(left.LastActivityAtMS, right.LastActivityAtMS)
		case "totalTokens":
			if useDashboard {
				comparison = compareInt64(dashboardTokenTotal(dashboard[left.ExternalSessionID]), dashboardTokenTotal(dashboard[right.ExternalSessionID]))
			} else if exactUsage {
				comparison = compareInt64(cursorTokenTotal(usage[left.ExternalSessionID]), cursorTokenTotal(usage[right.ExternalSessionID]))
			}
		case "estimatedCost":
			// Session-scoped Dashboard cost attribution is not yet projected onto local
			// session rows, so only the stable identity tie-breaker applies.
		default:
			comparison = compareString(left.ExternalSessionID, right.ExternalSessionID)
		}
		if comparison == 0 {
			comparison = compareString(left.ExternalSessionID, right.ExternalSessionID)
			return comparison > 0
		}
		if primary.Direction == basequery.SortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func sortCursorProjects(
	groups []cursorProjectGroup,
	primary basequery.SortTerm,
	exactUsage bool,
	useDashboard bool,
) {
	sort.SliceStable(groups, func(i, j int) bool {
		left, right := groups[i], groups[j]
		comparison := 0
		switch primary.Field {
		case "lastActivityAt":
			comparison = compareInt64(projectLastActivity(left), projectLastActivity(right))
		case "totalTokens":
			if useDashboard {
				comparison = compareInt64(dashboardTokenTotal(left.dashboardEvents), dashboardTokenTotal(right.dashboardEvents))
			} else if exactUsage {
				comparison = compareInt64(cursorTokenTotal(left.events), cursorTokenTotal(right.events))
			}
		case "displayName":
			comparison = compareString(left.name, right.name)
		case "estimatedCost":
			// Project-scoped Dashboard cost attribution is not yet projected onto local
			// project rows, so only the stable identity tie-breaker applies.
		default:
			comparison = compareString(left.key, right.key)
		}
		if comparison == 0 {
			comparison = compareString(left.key, right.key)
			return comparison > 0
		}
		if primary.Direction == basequery.SortAscending {
			return comparison < 0
		}
		return comparison > 0
	})
}

func cursorTokenTotal(events []store.CursorUsageEvent) int64 {
	var total int64
	for _, event := range events {
		total += event.InputTokens + event.OutputTokens
	}
	return total
}

func dashboardTokenTotal(events []store.CursorDashboardUsageEvent) int64 {
	var total int64
	for _, event := range events {
		total += (event.InputTokens + event.OutputTokens + event.CacheWriteTokens + event.CacheReadTokens) * event.OccurrenceCount
	}
	return total
}

func cursorSessionRequestCount(sessions []store.CursorSession) int64 {
	var total int64
	for _, session := range sessions {
		total += session.RequestCount
	}
	return total
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

func compareString(left, right string) int { return strings.Compare(left, right) }

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
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

type cursorProjectGroup struct {
	key, name       string
	confidence      string
	source          string
	reason          string
	sessions        []store.CursorSession
	events          []store.CursorUsageEvent
	requests        []store.CursorRequestEvent
	dashboardEvents []store.CursorDashboardUsageEvent
}

var cursorOtherProjectKey = digestString("cursor:project:other")

func projectGroups(
	sessions []store.CursorSession,
	events []store.CursorUsageEvent,
	requests []store.CursorRequestEvent,
	dashboardBySession map[string][]store.CursorDashboardUsageEvent,
) []cursorProjectGroup {
	usageBySession := groupUsageBySession(events)
	requestsBySession := groupRequestsBySession(requests)
	groups := map[string]*cursorProjectGroup{}
	for _, session := range sessions {
		projectKey, projectName, confidence, source, reason := cursorProjectGroupIdentity(session)
		group := groups[projectKey]
		if group == nil {
			group = &cursorProjectGroup{
				key: projectKey, name: projectName, confidence: confidence,
				source: source, reason: reason,
			}
			groups[projectKey] = group
		}
		group.sessions = append(group.sessions, session)
		group.events = append(group.events, usageBySession[session.ExternalSessionID]...)
		group.requests = append(group.requests, requestsBySession[session.ExternalSessionID]...)
		group.dashboardEvents = append(group.dashboardEvents, dashboardBySession[session.ExternalSessionID]...)
	}
	result := make([]cursorProjectGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool { return projectLastActivity(result[i]) > projectLastActivity(result[j]) })
	return result
}

func cursorProjectGroupIdentity(session store.CursorSession) (string, string, string, string, string) {
	if name := strings.TrimSpace(session.ProjectDisplayName); name != "" && name != "未识别项目" {
		return session.ProjectKey, name, "high", "cursor_workspace", "path_redacted"
	}
	return cursorOtherProjectKey, "其他", "low", "cursor_local", "workspace_unavailable"
}

func projectItem(group cursorProjectGroup, exactUsage, useDashboard bool) usagecost.ProjectItem {
	totals := totalsForUsageEvents(group.events, exactUsage)
	if useDashboard {
		totals = totalsForDashboardEvents(group.dashboardEvents)
	} else {
		totals.TurnCount = known(int64(len(group.requests)), basequery.NumericCount)
	}
	return usagecost.ProjectItem{
		DimensionKey: group.key,
		Project: usagecost.AttributionValue{
			ID: &group.key, DisplayName: &group.name, Confidence: group.confidence,
			Source: group.source, Reason: group.reason,
		},
		SessionCount: known(int64(len(group.sessions)), basequery.NumericCount),
		Trend:        []usagecost.ProjectDailyPoint{}, Totals: totals,
	}
}

func attributableDashboardEvents(
	sessions []store.CursorSession,
	events []store.CursorDashboardUsageEvent,
) (map[string][]store.CursorDashboardUsageEvent, bool) {
	sessionIDs := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		sessionIDs[session.ExternalSessionID] = struct{}{}
	}
	result := make(map[string][]store.CursorDashboardUsageEvent)
	complete := true
	for _, event := range events {
		if event.ExternalSessionID == nil {
			complete = false
			continue
		}
		if _, ok := sessionIDs[*event.ExternalSessionID]; !ok {
			complete = false
			continue
		}
		result[*event.ExternalSessionID] = append(result[*event.ExternalSessionID], event)
	}
	return result, complete
}

func dashboardEventsForSessions(
	bySession map[string][]store.CursorDashboardUsageEvent,
	sessions []store.CursorSession,
) []store.CursorDashboardUsageEvent {
	result := make([]store.CursorDashboardUsageEvent, 0)
	for _, session := range sessions {
		result = append(result, bySession[session.ExternalSessionID]...)
	}
	return result
}

func dashboardEventsForProjectGroups(groups []cursorProjectGroup) []store.CursorDashboardUsageEvent {
	result := make([]store.CursorDashboardUsageEvent, 0)
	for _, group := range groups {
		result = append(result, group.dashboardEvents...)
	}
	return result
}

func projectDaily(
	events []store.CursorUsageEvent,
	rangeValue basequery.UTCTimeRange,
	exact ...bool,
) []usagecost.ProjectDailyPoint {
	trend := usageTrend(events, rangeValue, usagecost.TrendDay, exact...)
	result := make([]usagecost.ProjectDailyPoint, 0, len(trend))
	for _, point := range trend {
		result = append(result, usagecost.ProjectDailyPoint{BucketStartAt: point.StartAtMS, Confidence: "high", Source: "cursor_state_usage", Reason: "exact_usage_event", Totals: point.Totals})
	}
	return result
}

func dashboardProjectDaily(
	events []store.CursorDashboardUsageEvent,
	rangeValue basequery.UTCTimeRange,
) []usagecost.ProjectDailyPoint {
	trend := dashboardUsageTrend(events, rangeValue, usagecost.TrendDay)
	result := make([]usagecost.ProjectDailyPoint, 0, len(trend))
	for _, point := range trend {
		result = append(result, usagecost.ProjectDailyPoint{
			BucketStartAt: point.StartAtMS, Confidence: "high", Source: SourceDashboard,
			Reason: "official_usage_event", Totals: point.Totals,
		})
	}
	return result
}

func groupUsageBySession(events []store.CursorUsageEvent) map[string][]store.CursorUsageEvent {
	result := make(map[string][]store.CursorUsageEvent)
	for _, event := range events {
		result[event.ExternalSessionID] = append(result[event.ExternalSessionID], event)
	}
	return result
}

func groupRequestsBySession(events []store.CursorRequestEvent) map[string][]store.CursorRequestEvent {
	result := make(map[string][]store.CursorRequestEvent)
	for _, event := range events {
		result[event.ExternalSessionID] = append(result[event.ExternalSessionID], event)
	}
	return result
}

func requestEventsForSession(
	events []store.CursorRequestEvent,
	externalSessionID string,
) []store.CursorRequestEvent {
	return append([]store.CursorRequestEvent(nil), groupRequestsBySession(events)[externalSessionID]...)
}
func projectLastActivity(group cursorProjectGroup) int64 {
	var value int64
	for _, session := range group.sessions {
		value = maxInt64(value, session.LastActivityAtMS)
	}
	return value
}

func invocationResponse(snapshot store.CursorSnapshot, rangeValue basequery.UTCTimeRange, request invocationusage.InvocationUsageRequest, tools []store.CursorToolEvent) invocationusage.InvocationUsageResponse {
	type toolStats struct {
		count, succeeded, failed, unknown, last int64
		sessions                                map[string]struct{}
		sources                                 map[string]struct{}
	}
	stats := map[string]*toolStats{}
	sessions := map[string]struct{}{}
	failures := int64(0)
	for _, event := range tools {
		item := stats[event.ToolName]
		if item == nil {
			item = &toolStats{sessions: map[string]struct{}{}, sources: map[string]struct{}{}}
			stats[event.ToolName] = item
		}
		item.count++
		item.last = maxInt64(item.last, event.OccurredAtMS)
		item.sessions[event.ExternalSessionID] = struct{}{}
		item.sources[event.Provenance] = struct{}{}
		sessions[event.ExternalSessionID] = struct{}{}
		switch event.Outcome {
		case "succeeded":
			item.succeeded++
		case "failed":
			item.failed++
			failures++
		default:
			item.unknown++
		}
	}
	names := make([]string, 0, len(stats))
	for name := range stats {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return stats[names[i]].count > stats[names[j]].count })
	limit := request.TopLimit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(names) > limit {
		names = names[:limit]
	}
	items := make([]invocationusage.ToolUsageItem, 0, len(names))
	for _, name := range names {
		item := stats[name]
		sources := make([]string, 0, len(item.sources))
		for source := range item.sources {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		items = append(items, invocationusage.ToolUsageItem{Name: name, CallCount: known(item.count, basequery.NumericCount), SessionCount: known(int64(len(item.sessions)), basequery.NumericCount), SucceededCount: known(item.succeeded, basequery.NumericCount), FailedCount: known(item.failed, basequery.NumericCount), UnknownCount: known(item.unknown, basequery.NumericCount), AverageDurationMS: unknown(basequery.NumericMilliseconds, basequery.UnknownUnavailable), LastSeenAtMS: known(item.last, basequery.NumericMilliseconds), Sources: sources})
	}
	trend := invocationTrend(tools, rangeValue, request.Granularity)
	return invocationusage.InvocationUsageResponse{ProviderContext: contextFor(snapshot), Meta: completeMeta(nil), Range: rangeValue, Granularity: request.Granularity, SourceClass: request.SourceClass, Totals: invocationusage.InvocationTotals{ToolCallCount: known(int64(len(tools)), basequery.NumericCount), DistinctToolCount: known(int64(len(stats)), basequery.NumericCount), SkillActivityCount: unknown(basequery.NumericCount, basequery.UnknownUnavailable), DistinctSkillCount: unknown(basequery.NumericCount, basequery.UnknownUnavailable), ToolFailureCount: known(failures, basequery.NumericCount), SessionCount: known(int64(len(sessions)), basequery.NumericCount), AIEditCount: cursorAIEditCount(snapshot, rangeValue)}, Trend: trend, Tools: items, Skills: []invocationusage.SkillUsageItem{}, Coverage: invocationusage.InvocationCoverage{StructuredEventCount: known(int64(len(tools)), basequery.NumericCount), DetectedEventCount: known(0, basequery.NumericCount)}}
}

func cursorAIEditCount(snapshot store.CursorSnapshot, rangeValue basequery.UTCTimeRange) basequery.NumericValue {
	available := false
	for _, source := range snapshot.Sources {
		if source.SourceKey == SourceAITracking && source.State == "available" {
			available = true
			break
		}
	}
	if !available {
		return unknown(basequery.NumericCount, basequery.UnknownUnavailable)
	}
	count := int64(0)
	for _, event := range snapshot.AIEditEvents {
		if event.OccurredAtMS >= rangeValue.StartAtMS && event.OccurredAtMS < rangeValue.EndAtMS {
			count += event.EditCount
		}
	}
	return known(count, basequery.NumericCount)
}

func invocationTrend(events []store.CursorToolEvent, rangeValue basequery.UTCTimeRange, granularity invocationusage.Granularity) []invocationusage.InvocationTrendPoint {
	location, err := time.LoadLocation(rangeValue.TimeZone)
	if err != nil {
		return []invocationusage.InvocationTrendPoint{}
	}
	counts := map[int64]int64{}
	ends := map[int64]int64{}
	for _, event := range events {
		at := time.UnixMilli(event.OccurredAtMS).In(location)
		start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, location)
		end := start.AddDate(0, 0, 1)
		if granularity == invocationusage.GranularityHour {
			start = time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), 0, 0, 0, location)
			end = start.Add(time.Hour)
		}
		counts[start.UnixMilli()]++
		ends[start.UnixMilli()] = end.UnixMilli()
	}
	keys := make([]int64, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := make([]invocationusage.InvocationTrendPoint, 0, len(keys))
	for _, key := range keys {
		result = append(result, invocationusage.InvocationTrendPoint{Key: strconv.FormatInt(key, 10), StartAtMS: known(key, basequery.NumericMilliseconds), EndAtMS: known(ends[key], basequery.NumericMilliseconds), ToolCallCount: known(counts[key], basequery.NumericCount), SkillActivityCount: unknown(basequery.NumericCount, basequery.UnknownUnavailable)})
	}
	return result
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
func completeMeta(page *basequery.PageInfo) basequery.ResponseMeta {
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, page, nil)
	if err != nil {
		panic(err)
	}
	return meta
}
func partialMeta(page *basequery.PageInfo) basequery.ResponseMeta {
	meta, err := basequery.NewResponseMeta(basequery.ResponsePartial, page, []basequery.ErrorCode{basequery.ErrorPartial})
	if err != nil {
		panic(err)
	}
	return meta
}
func known(value int64, unit basequery.NumericUnit) basequery.NumericValue {
	result, err := basequery.KnownNumeric(maxInt64(value, 0), unit)
	if err != nil {
		panic(err)
	}
	return result
}
func unknown(unit basequery.NumericUnit, reason basequery.UnknownReason) basequery.NumericValue {
	result, err := basequery.UnknownNumeric(unit, reason)
	if err != nil {
		panic(err)
	}
	return result
}
func pointerCostReason(value pricing.CostReason) *pricing.CostReason { return &value }
func sessionRange(session store.CursorSession, timezone string) basequery.UTCTimeRange {
	start := session.CreatedAtMS
	end := session.LastActivityAtMS + 1
	if end <= start {
		end = start + 1
	}
	return basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: timezone}
}
func dereferenceRange(value *basequery.UTCTimeRange, snapshot store.CursorSnapshot) basequery.UTCTimeRange {
	if value != nil {
		return *value
	}
	start := int64(0)
	end := snapshot.CollectedAtMS + 1
	for _, session := range snapshot.Sessions {
		start = minPositive(start, session.CreatedAtMS)
	}
	return basequery.UTCTimeRange{StartAtMS: start, EndAtMS: end, TimeZone: "UTC"}
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func slicePage[T any](values []T, offset, limit int) []T {
	if offset >= len(values) {
		return []T{}
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	return values[offset:end]
}
