package agyprovider

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
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const maxRangeDays = 366

// SnapshotReader 读取 AGY 已提交的本地轨迹与来源状态快照。
type SnapshotReader interface {
	AGYSnapshot(context.Context) (store.AGYSnapshot, error)
}

// QueryService 提供 Antigravity 的 UsageCost、Sessions 与 Projects 查询闭环。
type QueryService struct {
	disabled        bool
	collector       *Collector
	reader          SnapshotReader
	sessions        basequery.Specification
	projects        basequery.Specification
	snapshotMu      sync.RWMutex
	snapshotLoadMu  sync.Mutex
	snapshotCache   *store.AGYSnapshot
	snapshotEpoch   uint64
	refreshMu       sync.Mutex
	localRefreshing bool
	onRefresh       func()
	beginner        providercontrol.Beginner
}

type discardSnapshotWriter struct{}

func (discardSnapshotWriter) ReplaceAGYSnapshot(context.Context, store.AGYSnapshot) error {
	return nil
}

type emptySnapshotReader struct{}

func (emptySnapshotReader) AGYSnapshot(context.Context) (store.AGYSnapshot, error) {
	return store.AGYSnapshot{}, nil
}

func NewDisabledQueryService() (*QueryService, error) {
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		BaseRoot:       os.TempDir(),
		MinimumRefresh: time.Hour,
		Now:            time.Now,
	})
	if err != nil {
		return nil, err
	}
	service, err := NewQueryService(collector, emptySnapshotReader{})
	if err == nil {
		service.disabled = true
	}
	return service, err
}

func NewQueryService(collector *Collector, reader SnapshotReader) (*QueryService, error) {
	if collector == nil || reader == nil {
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
	return &QueryService{
		collector: collector,
		reader:    reader,
		sessions:  sessions,
		projects:  projects,
	}, nil
}

func (service *QueryService) BindBeginner(beginner providercontrol.Beginner) {
	if service == nil {
		return
	}
	service.beginner = beginner
}

func (service *QueryService) SetRefreshNotifier(notifier func()) {
	if service == nil {
		return
	}
	service.refreshMu.Lock()
	service.onRefresh = notifier
	service.refreshMu.Unlock()
}

func (service *QueryService) withOperation(
	ctx context.Context,
	timeout time.Duration,
	fn func(context.Context) error,
) error {
	if service == nil {
		return ErrCollector
	}
	if service.disabled {
		return basequery.NewUnavailableFailure(nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if service.beginner == nil {
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return fn(ctx)
	}
	operation, err := service.beginner.Begin(ctx, agentprovider.AGY)
	if err != nil {
		return err
	}
	defer operation.Finish()
	opCtx := operation.Context()
	if timeout > 0 {
		var cancel context.CancelFunc
		opCtx, cancel = context.WithTimeout(opCtx, timeout)
		defer cancel()
	}
	return fn(opCtx)
}

func (service *QueryService) Refresh(ctx context.Context) error {
	_, err := service.RefreshIfDue(ctx)
	return err
}

func (service *QueryService) RefreshIfDue(ctx context.Context) (bool, error) {
	if service == nil || service.collector == nil || service.reader == nil {
		return false, ErrCollector
	}
	performed := false
	err := service.withOperation(ctx, 0, func(opCtx context.Context) error {
		var refreshErr error
		performed, refreshErr = service.collector.RefreshIfDue(opCtx)
		if performed && refreshErr == nil {
			service.invalidateSnapshot()
		}
		return refreshErr
	})
	return performed, err
}

func (service *QueryService) snapshot(ctx context.Context) (store.AGYSnapshot, error) {
	if service == nil || service.collector == nil || service.reader == nil {
		return store.AGYSnapshot{}, ErrCollector
	}
	if service.disabled {
		return store.AGYSnapshot{}, basequery.NewUnavailableFailure(nil)
	}
	snap, err := service.loadSnapshot(ctx)
	if err != nil {
		return store.AGYSnapshot{}, err
	}
	service.scheduleLocalRefresh()
	return snap, nil
}

func (service *QueryService) loadSnapshot(ctx context.Context) (store.AGYSnapshot, error) {
	service.snapshotMu.RLock()
	if service.snapshotCache != nil {
		snap := *service.snapshotCache
		service.snapshotMu.RUnlock()
		return snap, nil
	}
	service.snapshotMu.RUnlock()

	service.snapshotLoadMu.Lock()
	defer service.snapshotLoadMu.Unlock()
	service.snapshotMu.RLock()
	if service.snapshotCache != nil {
		snap := *service.snapshotCache
		service.snapshotMu.RUnlock()
		return snap, nil
	}
	epoch := service.snapshotEpoch
	service.snapshotMu.RUnlock()

	snap, err := service.reader.AGYSnapshot(ctx)
	if errors.Is(err, store.ErrNotFound) {
		refreshErr := service.withOperation(ctx, 0, func(opCtx context.Context) error {
			return service.collector.Refresh(opCtx)
		})
		if refreshErr != nil {
			return store.AGYSnapshot{}, refreshErr
		}
		snap, err = service.reader.AGYSnapshot(ctx)
	}
	if err != nil {
		return store.AGYSnapshot{}, err
	}
	service.snapshotMu.Lock()
	if service.snapshotEpoch == epoch {
		service.snapshotCache = &snap
	}
	service.snapshotMu.Unlock()
	return snap, nil
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
		performed := false
		err := service.withOperation(context.Background(), time.Minute, func(ctx context.Context) error {
			var refreshErr error
			performed, refreshErr = service.collector.RefreshIfDue(ctx)
			if performed {
				performed = refreshErr == nil
			}
			return refreshErr
		})
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

func (service *QueryService) UsageCost(ctx context.Context, request usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	rangeValue, err := usageRange(request.Range, request.ExactRange)
	if err != nil || !validTrendGranularity(request.Granularity) {
		return usagecost.UsageCostResponse{}, fmt.Errorf("%w: agy usage request", basequery.ErrValidation)
	}
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	events := usageEventsInRange(snapshot.UsageEvents, *rangeValue)
	response := usagecost.UsageCostResponse{
		ProviderContext:   contextFor(snapshot),
		Meta:              snapshotMeta(snapshot, nil),
		Range:             *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone,
		PricingVersions:   []string{},
		Trend:             []usagecost.TrendPoint{},
		Models:            []usagecost.UsageModelItem{},
		UnpricedReasons:   []usagecost.ReasonCount{},
	}
	totals, _ := totalsForUsageEvents(events)
	response.Totals = totals
	response.Trend = usageTrend(events, *rangeValue, request.Granularity)
	if request.IncludeActivityDistribution {
		response.ActivityDistribution = activityDistribution(events, *rangeValue)
	}
	response.PricingSource, response.PricingVersions = pricingMetadata(events)
	if len(response.PricingVersions) > 0 {
		currency := "USD"
		response.Currency = &currency
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
	sessions, err := agySessions(snapshot, validated)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	usageBySession := groupUsageBySession(snapshot.UsageEvents)
	sortAGYSessions(sessions, validated.Sort[0], usageBySession)
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	pageSessions := slicePage(sessions, offset, limit)
	items := make([]usagecost.SessionItem, 0, len(pageSessions))
	for _, session := range pageSessions {
		items = append(items, sessionItem(session, usageBySession[session.ExternalSessionID]))
	}
	allEvents := make([]store.AGYUsageEvent, 0)
	pageEvents := make([]store.AGYUsageEvent, 0)
	for _, session := range sessions {
		allEvents = append(allEvents, usageBySession[session.ExternalSessionID]...)
	}
	for _, session := range pageSessions {
		pageEvents = append(pageEvents, usageBySession[session.ExternalSessionID]...)
	}
	matchedTotals, _ := totalsForUsageEvents(allEvents)
	pageTotals, _ := totalsForUsageEvents(pageEvents)
	matchedTotals.TurnCount = known(sessionRequestCount(sessions), basequery.NumericCount)
	pageTotals.TurnCount = known(sessionRequestCount(pageSessions), basequery.NumericCount)
	return usagecost.SessionListResponse{
		ProviderContext: contextFor(snapshot),
		Meta:            snapshotMeta(snapshot, pageInfo(offset, limit, len(sessions), snapshot.Generation, queryFingerprint(validated))),
		Items:           items,
		MatchedCount:    known(int64(len(sessions)), basequery.NumericCount),
		MatchedTotals:   matchedTotals,
		PageTotals:      pageTotals,
	}, nil
}

func (service *QueryService) SessionDetail(ctx context.Context, request usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	var session *store.AGYSession
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
	sort.Slice(events, func(i, j int) bool {
		if events[i].OccurredAtMS == events[j].OccurredAtMS {
			return events[i].StepIndex < events[j].StepIndex
		}
		return events[i].OccurredAtMS < events[j].OccurredAtMS
	})
	turnFP := digestString("agy-session-turns:" + session.ExternalSessionID)[:16]
	offset, limit, err := page(request.TurnPage, snapshot.Generation, turnFP)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	turns := make([]usagecost.SessionTurnItem, 0)
	for _, event := range slicePage(events, offset, limit) {
		turnTotals, estimatedOK := totalsForUsageEvents([]store.AGYUsageEvent{event})
		turnTotals.TurnCount = known(1, basequery.NumericCount)
		status := usagecost.SessionTurnPricingUnpriced
		var version *string
		var unpricedReason *pricing.CostReason
		if estimatedOK && turnTotals.EstimatedUSDMicros.Value != nil {
			status = usagecost.SessionTurnPricingPriced
			_, rate, _ := priceEvent(event)
			v := rate.PricingVersion()
			version = &v
		} else {
			r := pricing.CostReasonMissingPriceComponent
			unpricedReason = &r
		}
		startedMS := event.OccurredAtMS
		if event.StartedAtMS != nil {
			startedMS = *event.StartedAtMS
		}
		endedMS := event.OccurredAtMS
		if event.EndedAtMS != nil {
			endedMS = *event.EndedAtMS
		}
		turns = append(turns, usagecost.SessionTurnItem{
			TimelineKey:    event.EventID,
			State:          usagecost.SessionTurnComplete,
			Model:          modelAttribution(event.ModelKey),
			StartedAt:      known(startedMS, basequery.NumericMilliseconds),
			CompletedAt:    known(endedMS, basequery.NumericMilliseconds),
			ObservedAt:     known(event.OccurredAtMS, basequery.NumericMilliseconds),
			Totals:         turnTotals,
			PricingStatus:  status,
			PricingVersion: version,
			UnpricedReason: unpricedReason,
		})
	}
	item := sessionItem(*session, events)
	models := usageModels(events, rangeValue, usagecost.TrendDay)
	trend := usageTrend(events, rangeValue, usagecost.TrendDay)
	pricingSource, versions := pricingMetadata(events)
	var currency *string
	if len(versions) > 0 {
		c := "USD"
		currency = &c
	}
	var unpricedReasons []usagecost.ReasonCount
	if unpriced := unpricedCount(events); unpriced > 0 {
		unpricedReasons = []usagecost.ReasonCount{{
			Reason: pricing.CostReasonMissingPriceComponent,
			Count:  known(unpriced, basequery.NumericCount),
		}}
	} else {
		unpricedReasons = []usagecost.ReasonCount{}
	}
	return usagecost.SessionDetailResponse{
		ProviderContext:   contextFor(snapshot),
		Meta:              snapshotMeta(snapshot, nil),
		PricingSource:     pricingSource,
		Currency:          currency,
		PricingVersions:   versions,
		UnpricedReasons:   unpricedReasons,
		Item:              item,
		TurnPage:          *pageInfo(offset, limit, len(events), snapshot.Generation, turnFP),
		ThroughputTurns:   throughputTurns(session.Throughput),
		Turns:             turns,
		ReportingTimeZone: timezone,
		TrendGranularity:  usagecost.TrendDay,
		Trend:             trend,
		Models:            models,
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
	sessions, err := agySessionsForProjects(snapshot, validated)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	rangeEvents := usageEventsInRange(snapshot.UsageEvents, *validated.TimeRange)
	groups := projectGroups(sessions, rangeEvents)
	sortAGYProjects(groups, validated.Sort[0])
	offset, limit, err := page(validated.Page, snapshot.Generation, queryFingerprint(validated))
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	pageGroups := slicePage(groups, offset, limit)
	items := make([]usagecost.ProjectItem, 0, len(pageGroups))
	for _, group := range pageGroups {
		items = append(items, projectItem(group))
	}
	matchedEvents := make([]store.AGYUsageEvent, 0)
	for _, group := range groups {
		matchedEvents = append(matchedEvents, group.events...)
	}
	pageEvents := make([]store.AGYUsageEvent, 0)
	for _, group := range pageGroups {
		pageEvents = append(pageEvents, group.events...)
	}
	globalTotals, _ := totalsForUsageEvents(rangeEvents)
	matchedTotals, _ := totalsForUsageEvents(matchedEvents)
	pageTotals, _ := totalsForUsageEvents(pageEvents)
	pricingSource, versions := pricingMetadata(rangeEvents)
	var currency *string
	if len(versions) > 0 {
		c := "USD"
		currency = &c
	}
	return usagecost.ProjectListResponse{
		ProviderContext:   contextFor(snapshot),
		Meta:              snapshotMeta(snapshot, pageInfo(offset, limit, len(groups), snapshot.Generation, queryFingerprint(validated))),
		Range:             *validated.TimeRange,
		ReportingTimeZone: validated.TimeRange.TimeZone,
		PricingSource:     pricingSource,
		Currency:          currency,
		PricingVersions:   versions,
		Items:             items,
		MatchedCount:      known(int64(len(groups)), basequery.NumericCount),
		GlobalTotals:      globalTotals,
		MatchedTotals:     matchedTotals,
		PageTotals:        pageTotals,
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
	var selected *agyProjectGroup
	for index := range groups {
		if groups[index].key == request.DimensionKey {
			selected = &groups[index]
			break
		}
	}
	if selected == nil {
		return usagecost.ProjectDetailResponse{}, basequery.NewNotFoundFailure(nil)
	}
	sessionFP := digestString("agy-project-sessions:" + request.DimensionKey)[:16]
	modelFP := digestString("agy-project-models:" + request.DimensionKey)[:16]
	sessionOffset, sessionLimit, err := page(request.SessionPage, snapshot.Generation, sessionFP)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	modelOffset, modelLimit, err := page(request.ModelPage, snapshot.Generation, modelFP)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	usageBySession := groupUsageBySession(selected.events)
	sessionItems := make([]usagecost.ProjectSessionItem, 0)
	for _, session := range slicePage(selected.sessions, sessionOffset, sessionLimit) {
		session.RequestCount = int64(len(usageBySession[session.ExternalSessionID]))
		item := sessionItem(session, usageBySession[session.ExternalSessionID])
		sessionItems = append(sessionItems, usagecost.ProjectSessionItem{
			SessionID:       item.SessionID,
			DisplayTitle:    item.DisplayTitle,
			TitleConfidence: item.TitleConfidence,
			TitleSource:     item.TitleSource,
			TitleReason:     item.TitleReason,
			Model:           item.Model,
			Activity:        item.Activity,
			LastActivityAt:  item.LastActivityAt,
			Totals:          item.Totals,
		})
	}
	models := usageModels(selected.events, *rangeValue, usagecost.TrendDay)
	modelItems := make([]usagecost.ProjectModelItem, 0)
	for _, item := range slicePage(models, modelOffset, modelLimit) {
		modelItems = append(modelItems, usagecost.ProjectModelItem{
			DimensionKey: item.DimensionKey,
			Model:        item.Model,
			Totals:       item.Totals,
		})
	}
	globalTotals, _ := totalsForUsageEvents(rangeEvents)
	pricingSource, versions := pricingMetadata(selected.events)
	var currency *string
	if len(versions) > 0 {
		c := "USD"
		currency = &c
	}
	return usagecost.ProjectDetailResponse{
		ProviderContext:   contextFor(snapshot),
		Meta:              snapshotMeta(snapshot, nil),
		Range:             *rangeValue,
		ReportingTimeZone: rangeValue.TimeZone,
		PricingSource:     pricingSource,
		Currency:          currency,
		PricingVersions:   versions,
		Item:              projectItem(*selected),
		Daily:             projectDaily(selected.events, *rangeValue),
		SessionPage:       *pageInfo(sessionOffset, sessionLimit, len(selected.sessions), snapshot.Generation, sessionFP),
		Sessions:          sessionItems,
		ModelPage:         *pageInfo(modelOffset, modelLimit, len(models), snapshot.Generation, modelFP),
		Models:            modelItems,
		GlobalTotals:      globalTotals,
	}, nil
}

func contextFor(snapshot store.AGYSnapshot) agentprovider.Context {
	ctx := agentprovider.Context{
		EffectiveProvider: agentprovider.AGY,
		Capabilities:      []string{"sessions", "projects", "models", "tokens"},
		Coverage:          []agentprovider.Coverage{},
	}
	for _, source := range snapshot.Sources {
		ctx.Sources = append(ctx.Sources, source.SourceKey)
		ctx.Coverage = append(ctx.Coverage, agentprovider.Coverage{
			Capability: sourceCapability(source.SourceKey),
			State:      source.State,
			Source:     source.SourceKey,
			Reason:     source.CoverageState,
			ItemCount:  &source.RowCount,
		})
	}
	if hasEstimatedCost(snapshot.UsageEvents) && !containsString(ctx.Capabilities, "estimated_cost") {
		ctx.Capabilities = append(ctx.Capabilities, "estimated_cost")
	}
	return ctx
}

func sourceCapability(source string) string {
	switch source {
	case SourceSummaries:
		return "sessions"
	case SourceTrajectories, SourceTranscripts:
		return "tokens"
	case SourceLSQuota:
		return "quota"
	default:
		return "unknown"
	}
}
