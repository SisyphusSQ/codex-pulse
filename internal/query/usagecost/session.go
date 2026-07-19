package usagecost

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func (service *Service) ListSessions(
	ctx context.Context,
	request basequery.Request,
) (SessionListResponse, error) {
	if service == nil || service.sessionReader == nil {
		return SessionListResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SessionListResponse{}, err
	}
	validated, err := service.sessionSpec.Validate(ctx, request)
	if err != nil {
		return SessionListResponse{}, err
	}
	storeFilter, primarySort, err := sessionStoreFilter(validated)
	if err != nil {
		return SessionListResponse{}, err
	}
	if validated.Page.Cursor != nil {
		storeFilter.Cursor, err = decodeSessionCursor(
			*validated.Page.Cursor, primarySort.Field, primarySort.Direction,
		)
		if err != nil {
			return SessionListResponse{}, err
		}
	}
	page, err := service.sessionReader.ListSessionAnalytics(ctx, storeFilter)
	if err != nil {
		return SessionListResponse{}, mapSessionReaderError(err)
	}
	response, err := mapSessionListResponse(page, validated.Page.Limit, primarySort)
	if err != nil {
		return SessionListResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func (service *Service) SessionDetail(
	ctx context.Context,
	request SessionDetailRequest,
) (SessionDetailResponse, error) {
	if service == nil || service.sessionReader == nil {
		return SessionDetailResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SessionDetailResponse{}, err
	}
	if !validOpaqueIdentity(request.SessionID) {
		return SessionDetailResponse{}, basequery.NewValidationFailure("sessionId", nil)
	}
	if request.ReportingTimezone != nil {
		if *request.ReportingTimezone == "" || *request.ReportingTimezone == "Local" {
			return SessionDetailResponse{}, basequery.NewValidationFailure("reportingTimezone", nil)
		}
		if _, err := time.LoadLocation(*request.ReportingTimezone); err != nil {
			return SessionDetailResponse{}, basequery.NewValidationFailure("reportingTimezone", err)
		}
	}
	turnPage, err := normalizeSessionTurnPage(request.TurnPage)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	var turnCursor *store.SessionTurnAnalyticsCursor
	if turnPage.Cursor != nil {
		turnCursor, err = decodeSessionTurnCursor(
			service.sessionTurnCursorKey, *turnPage.Cursor, request.SessionID,
		)
		if err != nil {
			return SessionDetailResponse{}, err
		}
	}
	snapshot, err := service.sessionReader.SessionAnalytics(ctx, store.SessionAnalyticsDetailFilter{
		SessionID: request.SessionID, ReportingTimezone: cloneStringPointer(request.ReportingTimezone),
		TurnLimit: turnPage.Limit, TurnCursor: turnCursor,
	})
	if err != nil {
		return SessionDetailResponse{}, mapSessionReaderError(err)
	}
	response, err := mapSessionDetailResponse(
		snapshot, turnPage.Limit, turnPage.Cursor == nil, service.sessionTurnCursorKey,
	)
	if err != nil {
		return SessionDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func normalizeSessionTurnPage(page basequery.PageRequest) (basequery.PageRequest, error) {
	limit := page.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return basequery.PageRequest{}, basequery.NewValidationFailure("turnPage.limit", nil)
	}
	normalized := basequery.PageRequest{Limit: limit}
	if page.Cursor != nil {
		cursor := *page.Cursor
		if cursor == "" {
			return basequery.PageRequest{}, basequery.NewValidationFailure("turnPage.cursor", nil)
		}
		normalized.Cursor = &cursor
	}
	return normalized, nil
}

func sessionStoreFilter(
	request basequery.ValidatedRequest,
) (store.SessionAnalyticsFilter, basequery.SortTerm, error) {
	if len(request.Sort) != 2 || request.Sort[0].Field == "sessionId" ||
		request.Sort[1].Field != "sessionId" || request.Sort[1].Direction != basequery.SortDescending {
		return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
			basequery.NewValidationFailure("sort", nil)
	}
	primary := request.Sort[0]
	filter := store.SessionAnalyticsFilter{Limit: request.Page.Limit}
	switch primary.Field {
	case "lastActivityAt":
		filter.SortField = store.SessionAnalyticsSortLastActivity
	case "totalTokens":
		filter.SortField = store.SessionAnalyticsSortTotalTokens
	case "estimatedCost":
		filter.SortField = store.SessionAnalyticsSortEstimatedCost
	default:
		return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
			basequery.NewValidationFailure("sort.field", nil)
	}
	if primary.Direction == basequery.SortAscending {
		filter.SortDirection = store.AnalyticsSortAscending
	} else {
		filter.SortDirection = store.AnalyticsSortDescending
	}
	seenFields := make(map[string]struct{}, len(request.Filters))
	for _, term := range request.Filters {
		if _, found := seenFields[term.Field]; found {
			return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters", nil)
		}
		seenFields[term.Field] = struct{}{}
		if hasDuplicateStrings(term.Values) {
			return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters.values", nil)
		}
		switch term.Field {
		case "projectId":
			filter.ProjectIDs = append([]string(nil), term.Values...)
		case "modelKey":
			filter.ModelKeys = append([]string(nil), term.Values...)
		case "activity":
			activity := store.SessionActivity(term.Values[0])
			if activity != store.SessionActivityActive && activity != store.SessionActivityIdle {
				return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
					basequery.NewValidationFailure("filters.values", nil)
			}
			filter.Activity = &activity
		default:
			return store.SessionAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters.field", nil)
		}
	}
	if request.TimeRange != nil {
		filter.ReportingTimezone = cloneStringPointer(&request.TimeRange.TimeZone)
		filter.LastActivityAtOrAfterMS = cloneInt64(&request.TimeRange.StartAtMS)
		filter.LastActivityBeforeMS = cloneInt64(&request.TimeRange.EndAtMS)
	}
	if filter.ProjectIDs == nil {
		filter.ProjectIDs = make([]string, 0)
	}
	if filter.ModelKeys == nil {
		filter.ModelKeys = make([]string, 0)
	}
	return filter, primary, nil
}

func mapSessionListResponse(
	page store.SessionAnalyticsPage,
	limit int,
	primarySort basequery.SortTerm,
) (SessionListResponse, error) {
	if err := validateSessionPageShape(page, limit, primarySort.Field); err != nil {
		return SessionListResponse{}, err
	}
	items := make([]SessionItem, 0, len(page.Records))
	partial := page.Mode != store.AnalyticsReadActiveRollup
	for _, record := range page.Records {
		item, err := mapSessionItem(record, page.Mode)
		if err != nil {
			return SessionListResponse{}, err
		}
		if usageTotalsArePartial(item.Totals) {
			partial = true
		}
		items = append(items, item)
	}
	matchedCount, err := basequery.KnownNumeric(page.MatchedCount, basequery.NumericCount)
	if err != nil {
		return SessionListResponse{}, err
	}
	matchedTotals, err := mapSessionTotals(page.MatchedTotals, page.Mode)
	if err != nil {
		return SessionListResponse{}, err
	}
	pageTotals, err := mapSessionTotals(page.PageTotals, page.Mode)
	if err != nil {
		return SessionListResponse{}, err
	}
	if usageTotalsArePartial(matchedTotals) || usageTotalsArePartial(pageTotals) {
		partial = true
	}
	nextCursor, err := encodeSessionCursor(page.NextCursor, primarySort.Field, primarySort.Direction)
	if err != nil {
		return SessionListResponse{}, err
	}
	status := basequery.ResponseComplete
	var issues []basequery.ErrorCode
	if partial {
		status = basequery.ResponsePartial
		issues = []basequery.ErrorCode{basequery.ErrorPartial}
	}
	meta, err := basequery.NewResponseMeta(status, &basequery.PageInfo{
		Limit: limit, HasMore: nextCursor != nil, NextCursor: nextCursor,
	}, issues)
	if err != nil {
		return SessionListResponse{}, err
	}
	response := SessionListResponse{
		Meta: meta, Items: items, MatchedCount: matchedCount,
		MatchedTotals: matchedTotals, PageTotals: pageTotals,
	}
	if page.Generation != nil {
		response.PricingSource = cloneString(page.Generation.PricingSource)
		response.Currency = cloneString(page.Generation.Currency)
	} else {
		reason := sessionDegradedReason(page.Mode)
		response.DegradedReason = &reason
	}
	return response, nil
}

func mapSessionDetailResponse(
	snapshot store.SessionAnalyticsSnapshot,
	turnLimit int,
	isFirstTurnPage bool,
	cursorKey sessionTurnCursorKey,
) (SessionDetailResponse, error) {
	if err := validateSessionSnapshotShape(snapshot, turnLimit); err != nil {
		return SessionDetailResponse{}, err
	}
	item, err := mapSessionItem(snapshot.Record, snapshot.Mode)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	evidenceTotals := store.RollupTotals{}
	if snapshot.Record.Rollup != nil {
		evidenceTotals = *snapshot.Record.Rollup
	}
	if err := validatePricingEvidence(
		snapshot.Mode, evidenceTotals, snapshot.PricingVersions, snapshot.UnpricedReasons,
	); err != nil {
		return SessionDetailResponse{}, err
	}
	versions, err := normalizedPricingVersions(snapshot.PricingVersions)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	reasons, err := normalizedReasonCounts(snapshot.UnpricedReasons)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	if err := validateSessionTurnReconciliation(snapshot, isFirstTurnPage); err != nil {
		return SessionDetailResponse{}, err
	}
	turns := make([]SessionTurnItem, 0, len(snapshot.Turns))
	partial := snapshot.Mode != store.AnalyticsReadActiveRollup || usageTotalsArePartial(item.Totals)
	for _, record := range snapshot.Turns {
		turn, turnPartial, err := mapSessionTurn(record, snapshot.Mode)
		if err != nil {
			return SessionDetailResponse{}, err
		}
		partial = partial || turnPartial
		turns = append(turns, turn)
	}
	nextTurnCursor, err := encodeSessionTurnCursor(cursorKey, snapshot.NextTurnCursor)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	status := basequery.ResponseComplete
	var issues []basequery.ErrorCode
	if partial {
		status = basequery.ResponsePartial
		issues = []basequery.ErrorCode{basequery.ErrorPartial}
	}
	meta, err := basequery.NewResponseMeta(status, nil, issues)
	if err != nil {
		return SessionDetailResponse{}, err
	}
	response := SessionDetailResponse{
		Meta: meta, Item: item, PricingVersions: versions, UnpricedReasons: reasons,
		TurnPage: basequery.PageInfo{
			Limit: turnLimit, HasMore: nextTurnCursor != nil, NextCursor: nextTurnCursor,
		},
		Turns: turns,
	}
	if snapshot.Generation != nil {
		response.PricingSource = cloneString(snapshot.Generation.PricingSource)
		response.Currency = cloneString(snapshot.Generation.Currency)
	} else {
		reason := sessionDegradedReason(snapshot.Mode)
		response.DegradedReason = &reason
	}
	return response, nil
}

func validateSessionTurnReconciliation(
	snapshot store.SessionAnalyticsSnapshot,
	isFirstTurnPage bool,
) error {
	if snapshot.Mode != store.AnalyticsReadActiveRollup {
		return nil
	}
	aggregate := knownEmptySessionRollup()
	if snapshot.Record.Rollup != nil {
		aggregate = *snapshot.Record.Rollup
	}
	page, versions, reasons, err := aggregateSessionTurnPage(snapshot.Turns)
	if err != nil {
		return err
	}
	if aggregate.TurnCount < page.TurnCount ||
		aggregate.PricedTurnCount < page.PricedTurnCount ||
		aggregate.UnpricedTurnCount < page.UnpricedTurnCount ||
		!sessionTurnPageNumericLowerBound(aggregate.InputTokens, page.InputTokens) ||
		!sessionTurnPageNumericLowerBound(aggregate.CachedInputTokens, page.CachedInputTokens) ||
		!sessionTurnPageNumericLowerBound(aggregate.OutputTokens, page.OutputTokens) ||
		!sessionTurnPageNumericLowerBound(aggregate.ReasoningTokens, page.ReasoningTokens) ||
		!sessionTurnPageNumericLowerBound(aggregate.TotalTokens, page.TotalTokens) ||
		!sessionTurnPageNumericLowerBound(
			aggregate.EstimatedUSDMicros, page.EstimatedUSDMicros,
		) {
		return errors.New("stored session turn page exceeds session aggregate")
	}
	versionSet := make(map[string]struct{}, len(snapshot.PricingVersions))
	for _, version := range snapshot.PricingVersions {
		versionSet[version] = struct{}{}
	}
	for version := range versions {
		if _, found := versionSet[version]; !found {
			return errors.New("stored session turn pricing version is not reconciled")
		}
	}
	reasonSet := make(map[pricing.CostReason]int64, len(snapshot.UnpricedReasons))
	for _, reason := range snapshot.UnpricedReasons {
		reasonSet[reason.Reason] = reason.Count
	}
	for reason, count := range reasons {
		if reasonSet[reason] < count {
			return errors.New("stored session turn unpriced reason is not reconciled")
		}
	}
	if !isFirstTurnPage || snapshot.NextTurnCursor != nil {
		return nil
	}
	if !equalSessionTurnRollup(aggregate, page) ||
		!equalSessionTurnVersionSets(versionSet, versions) ||
		!equalSessionTurnReasonCounts(reasonSet, reasons) {
		return errors.New("stored complete session turn page is inconsistent with aggregate")
	}
	return nil
}

func aggregateSessionTurnPage(
	records []store.SessionTurnAnalyticsRecord,
) (store.RollupTotals, map[string]struct{}, map[pricing.CostReason]int64, error) {
	accumulator := newTotalsAccumulator()
	versions := make(map[string]struct{})
	reasons := make(map[pricing.CostReason]int64)
	for _, record := range records {
		if record.CompletedAtMS == nil {
			continue
		}
		if record.Usage == nil || !record.Usage.IsFinal || record.Cost == nil {
			return store.RollupTotals{}, nil, nil,
				errors.New("stored completed session turn evidence is incomplete")
		}
		row := store.RollupTotals{
			TurnCount: 1, InputTokens: cloneInt64(record.Usage.InputTokens),
			CachedInputTokens: cloneInt64(record.Usage.CachedInputTokens),
			OutputTokens:      cloneInt64(record.Usage.OutputTokens),
			ReasoningTokens:   cloneInt64(record.Usage.ReasoningTokens),
			FirstActivityAtMS: record.Usage.ObservedAtMS,
			LastActivityAtMS:  record.Usage.ObservedAtMS,
		}
		if row.InputTokens != nil && row.CachedInputTokens != nil &&
			row.OutputTokens != nil && row.ReasoningTokens != nil {
			total := int64(0)
			var err error
			for _, component := range []int64{
				*row.InputTokens, *row.CachedInputTokens,
				*row.OutputTokens, *row.ReasoningTokens,
			} {
				total, err = checkedAdd(total, component)
				if err != nil {
					return store.RollupTotals{}, nil, nil, err
				}
			}
			row.TotalTokens = &total
		}
		switch record.Cost.Status {
		case pricing.CostStatusPriced:
			if record.Cost.PricingVersion == nil || record.Cost.EstimatedUSDMicros == nil {
				return store.RollupTotals{}, nil, nil,
					errors.New("stored priced session turn evidence is incomplete")
			}
			row.PricedTurnCount = 1
			row.EstimatedUSDMicros = cloneInt64(record.Cost.EstimatedUSDMicros)
			versions[*record.Cost.PricingVersion] = struct{}{}
		case pricing.CostStatusUnpriced:
			row.UnpricedTurnCount = 1
			reasons[record.Cost.Reason]++
		default:
			return store.RollupTotals{}, nil, nil,
				errors.New("stored session turn pricing status is invalid")
		}
		if err := accumulator.add(row); err != nil {
			return store.RollupTotals{}, nil, nil, err
		}
	}
	page, err := accumulator.totals()
	return page, versions, reasons, err
}

func knownEmptySessionRollup() store.RollupTotals {
	zero := int64(0)
	return store.RollupTotals{
		InputTokens: &zero, CachedInputTokens: cloneInt64(&zero),
		OutputTokens: cloneInt64(&zero), ReasoningTokens: cloneInt64(&zero),
		TotalTokens: cloneInt64(&zero), EstimatedUSDMicros: cloneInt64(&zero),
	}
}

func sessionTurnPageNumericLowerBound(aggregate, page *int64) bool {
	return page == nil || aggregate == nil || *aggregate >= *page
}

func equalSessionTurnRollup(left, right store.RollupTotals) bool {
	return left.TurnCount == right.TurnCount &&
		left.PricedTurnCount == right.PricedTurnCount &&
		left.UnpricedTurnCount == right.UnpricedTurnCount &&
		equalInt64Pointers(left.InputTokens, right.InputTokens) &&
		equalInt64Pointers(left.CachedInputTokens, right.CachedInputTokens) &&
		equalInt64Pointers(left.OutputTokens, right.OutputTokens) &&
		equalInt64Pointers(left.ReasoningTokens, right.ReasoningTokens) &&
		equalInt64Pointers(left.TotalTokens, right.TotalTokens) &&
		equalInt64Pointers(left.EstimatedUSDMicros, right.EstimatedUSDMicros) &&
		left.FirstActivityAtMS == right.FirstActivityAtMS &&
		left.LastActivityAtMS == right.LastActivityAtMS
}

func equalSessionTurnVersionSets(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for version := range left {
		if _, found := right[version]; !found {
			return false
		}
	}
	return true
}

func equalSessionTurnReasonCounts(
	left, right map[pricing.CostReason]int64,
) bool {
	if len(left) != len(right) {
		return false
	}
	for reason, count := range left {
		if right[reason] != count {
			return false
		}
	}
	return true
}

func mapSessionTurn(
	record store.SessionTurnAnalyticsRecord,
	mode store.AnalyticsReadMode,
) (SessionTurnItem, bool, error) {
	if !validOpaqueIdentity(record.TurnID) || record.StartedAtMS < 0 ||
		!validAttributionTuple(record.Model.ModelKey, record.Model.DisplayName) ||
		!validAttributionEvidence(record.Model.Confidence, record.Model.Source, record.Model.Reason) {
		return SessionTurnItem{}, false, errors.New("stored session turn is invalid")
	}
	if (record.CompletedAtMS != nil && *record.CompletedAtMS < record.StartedAtMS) ||
		(record.Usage != nil &&
			(record.Usage.ObservedAtMS < record.StartedAtMS ||
				record.Usage.IsFinal != (record.CompletedAtMS != nil))) ||
		(record.Cost != nil &&
			(record.CompletedAtMS == nil || record.Usage == nil || !record.Usage.IsFinal)) {
		return SessionTurnItem{}, false, errors.New("stored session turn lifecycle is invalid")
	}
	started, err := basequery.KnownNumeric(record.StartedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return SessionTurnItem{}, false, err
	}
	state := SessionTurnActive
	completed, err := basequery.UnknownNumeric(
		basequery.NumericMilliseconds, basequery.UnknownNotApplicable,
	)
	if err != nil {
		return SessionTurnItem{}, false, err
	}
	if record.CompletedAtMS != nil {
		state = SessionTurnComplete
		completed, err = basequery.KnownNumeric(*record.CompletedAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return SessionTurnItem{}, false, err
		}
	}
	totals, observed, partial, err := mapSessionTurnUsage(record)
	if err != nil {
		return SessionTurnItem{}, false, err
	}
	pricingStatus := SessionTurnPricingUnknown
	var pricingVersion *string
	var unpricedReason *pricing.CostReason
	if record.Cost != nil {
		switch record.Cost.Status {
		case pricing.CostStatusPriced:
			if record.Cost.Reason != pricing.CostReasonPriced ||
				record.Cost.PricingVersion == nil ||
				!validOpaqueIdentity(*record.Cost.PricingVersion) ||
				record.Cost.EstimatedUSDMicros == nil {
				return SessionTurnItem{}, false, errors.New("stored priced session turn is invalid")
			}
			pricingStatus = SessionTurnPricingPriced
			pricingVersion = cloneStringPointer(record.Cost.PricingVersion)
			totals.EstimatedUSDMicros, err = basequery.KnownNumeric(
				*record.Cost.EstimatedUSDMicros, basequery.NumericMicroUSD,
			)
			if err != nil {
				return SessionTurnItem{}, false, err
			}
			totals.PricedTurnCount, _ = basequery.KnownNumeric(1, basequery.NumericCount)
			totals.UnpricedTurnCount, _ = basequery.KnownNumeric(0, basequery.NumericCount)
		case pricing.CostStatusUnpriced:
			if !validSessionTurnUnpricedReason(record.Cost.Reason) ||
				record.Cost.PricingVersion != nil || record.Cost.EstimatedUSDMicros != nil {
				return SessionTurnItem{}, false, errors.New("stored unpriced session turn is invalid")
			}
			pricingStatus = SessionTurnPricingUnpriced
			reason := record.Cost.Reason
			unpricedReason = &reason
			totals.EstimatedUSDMicros, _ = basequery.UnknownNumeric(
				basequery.NumericMicroUSD, basequery.UnknownNotComputed,
			)
			totals.PricedTurnCount, _ = basequery.KnownNumeric(0, basequery.NumericCount)
			totals.UnpricedTurnCount, _ = basequery.KnownNumeric(1, basequery.NumericCount)
			partial = true
		default:
			return SessionTurnItem{}, false, errors.New("stored session turn pricing status is invalid")
		}
	} else {
		if mode == store.AnalyticsReadActiveRollup && record.CompletedAtMS != nil &&
			record.Usage != nil && record.Usage.IsFinal {
			return SessionTurnItem{}, false, errors.New("stored final session turn cost is missing")
		}
		costReason := basequery.UnknownNotComputed
		if mode != store.AnalyticsReadActiveRollup {
			costReason = basequery.UnknownUnavailable
		}
		totals.EstimatedUSDMicros, _ = basequery.UnknownNumeric(
			basequery.NumericMicroUSD, costReason,
		)
		totals.PricedTurnCount, _ = basequery.UnknownNumeric(basequery.NumericCount, costReason)
		totals.UnpricedTurnCount, _ = basequery.UnknownNumeric(basequery.NumericCount, costReason)
		partial = true
	}
	return SessionTurnItem{
		TimelineKey: sessionTurnTimelineKey(record.TurnID), State: state,
		Model: AttributionValue{
			ID:          cloneStringPointer(record.Model.ModelKey),
			DisplayName: cloneStringPointer(record.Model.DisplayName),
			Confidence:  string(record.Model.Confidence), Source: string(record.Model.Source),
			Reason: string(record.Model.Reason),
		},
		StartedAt: started, CompletedAt: completed, ObservedAt: observed, Totals: totals,
		PricingStatus: pricingStatus, PricingVersion: pricingVersion,
		UnpricedReason: unpricedReason,
	}, partial, nil
}

func validSessionTurnUnpricedReason(reason pricing.CostReason) bool {
	switch reason {
	case pricing.CostReasonMissingAttribution, pricing.CostReasonMissingModel,
		pricing.CostReasonConflictModel, pricing.CostReasonInvalidModel,
		pricing.CostReasonCatalogNotEffective, pricing.CostReasonModelNotListed,
		pricing.CostReasonMissingToken, pricing.CostReasonMissingPriceComponent:
		return true
	default:
		return false
	}
}

func mapSessionTurnUsage(
	record store.SessionTurnAnalyticsRecord,
) (UsageTotals, basequery.NumericValue, bool, error) {
	turnCount, _ := basequery.KnownNumeric(1, basequery.NumericCount)
	first, err := basequery.KnownNumeric(record.StartedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return UsageTotals{}, basequery.NumericValue{}, false, err
	}
	last := first
	if record.CompletedAtMS != nil {
		last, err = basequery.KnownNumeric(*record.CompletedAtMS, basequery.NumericMilliseconds)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
	}
	unknownTokens := func() (basequery.NumericValue, error) {
		return basequery.UnknownNumeric(basequery.NumericTokens, basequery.UnknownUnavailable)
	}
	input, err := unknownTokens()
	if err != nil {
		return UsageTotals{}, basequery.NumericValue{}, false, err
	}
	cached, _ := unknownTokens()
	output, _ := unknownTokens()
	reasoning, _ := unknownTokens()
	total, _ := unknownTokens()
	observed, _ := basequery.UnknownNumeric(
		basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
	)
	partial := true
	if record.Usage != nil {
		observed, err = basequery.KnownNumeric(
			record.Usage.ObservedAtMS, basequery.NumericMilliseconds,
		)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
		input, err = numericOrUnknown(
			record.Usage.InputTokens, basequery.NumericTokens, basequery.UnknownUnavailable,
		)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
		cached, err = numericOrUnknown(
			record.Usage.CachedInputTokens, basequery.NumericTokens, basequery.UnknownUnavailable,
		)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
		output, err = numericOrUnknown(
			record.Usage.OutputTokens, basequery.NumericTokens, basequery.UnknownUnavailable,
		)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
		reasoning, err = numericOrUnknown(
			record.Usage.ReasoningTokens, basequery.NumericTokens, basequery.UnknownUnavailable,
		)
		if err != nil {
			return UsageTotals{}, basequery.NumericValue{}, false, err
		}
		if record.Usage.InputTokens != nil && record.Usage.CachedInputTokens != nil &&
			record.Usage.OutputTokens != nil && record.Usage.ReasoningTokens != nil {
			totalValue := *record.Usage.InputTokens + *record.Usage.CachedInputTokens +
				*record.Usage.OutputTokens + *record.Usage.ReasoningTokens
			total, err = basequery.KnownNumeric(totalValue, basequery.NumericTokens)
			if err != nil {
				return UsageTotals{}, basequery.NumericValue{}, false, err
			}
			partial = false
		}
		last = observed
	}
	cost, _ := basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
	priced, _ := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownNotComputed)
	unpriced, _ := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownNotComputed)
	return UsageTotals{
		TurnCount: turnCount, InputTokens: input, CachedInputTokens: cached,
		OutputTokens: output, ReasoningTokens: reasoning, TotalTokens: total,
		EstimatedUSDMicros: cost, PricedTurnCount: priced, UnpricedTurnCount: unpriced,
		FirstActivityAtMS: first, LastActivityAtMS: last,
	}, observed, partial, nil
}

func sessionTurnTimelineKey(turnID string) string {
	digest := sha256.Sum256([]byte("session-turn-timeline-v1\x00" + turnID))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validateSessionPageShape(page store.SessionAnalyticsPage, limit int, sortField string) error {
	if page.MatchedCount < 0 || len(page.Records) > limit || int64(len(page.Records)) > page.MatchedCount ||
		page.Records == nil {
		return errors.New("stored session page shape is invalid")
	}
	if page.NextCursor != nil {
		if len(page.Records) != limit || len(page.Records) == 0 {
			return errors.New("stored session next cursor cardinality is invalid")
		}
		expected := sessionCursorForStoreRecord(page.Records[len(page.Records)-1], sortField)
		if expected.SessionID != page.NextCursor.SessionID || expected.Null != page.NextCursor.Null ||
			!equalInt64Pointers(expected.Value, page.NextCursor.Value) {
			return errors.New("stored session next cursor is inconsistent")
		}
	}
	if page.Mode == store.AnalyticsReadActiveRollup {
		if page.Generation == nil || page.MatchedTotals == nil || page.PageTotals == nil {
			return errors.New("stored active session page shape is invalid")
		}
		return validateSessionGeneration(*page.Generation)
	}
	if page.Mode == store.AnalyticsReadLightIndex {
		if page.Generation != nil || page.MatchedTotals == nil || page.PageTotals == nil {
			return errors.New("stored light session page shape is invalid")
		}
		return nil
	}
	if (page.Mode != store.AnalyticsReadDetailFallback &&
		page.Mode != store.AnalyticsReadAmbiguousFallback) || page.Generation != nil ||
		page.MatchedTotals != nil || page.PageTotals != nil {
		return errors.New("stored fallback session page shape is invalid")
	}
	return nil
}

func validateSessionSnapshotShape(snapshot store.SessionAnalyticsSnapshot, turnLimit int) error {
	if turnLimit < 1 || turnLimit > 50 || snapshot.PricingVersions == nil ||
		snapshot.UnpricedReasons == nil || snapshot.Turns == nil || len(snapshot.Turns) > turnLimit {
		return errors.New("stored session detail evidence shape is invalid")
	}
	seen := make(map[string]struct{}, len(snapshot.Turns))
	for index, turn := range snapshot.Turns {
		if _, found := seen[turn.TurnID]; found ||
			(index > 0 && (snapshot.Turns[index-1].StartedAtMS < turn.StartedAtMS ||
				(snapshot.Turns[index-1].StartedAtMS == turn.StartedAtMS &&
					snapshot.Turns[index-1].TurnID <= turn.TurnID))) {
			return errors.New("stored session turn order is invalid")
		}
		seen[turn.TurnID] = struct{}{}
	}
	if snapshot.NextTurnCursor != nil {
		if len(snapshot.Turns) != turnLimit || len(snapshot.Turns) == 0 {
			return errors.New("stored session turn cursor cardinality is invalid")
		}
		last := snapshot.Turns[len(snapshot.Turns)-1]
		if snapshot.NextTurnCursor.SessionID != snapshot.Record.SessionID ||
			snapshot.NextTurnCursor.TurnID != last.TurnID ||
			snapshot.NextTurnCursor.StartedAtMS != last.StartedAtMS {
			return errors.New("stored session turn cursor is inconsistent")
		}
	}
	if snapshot.Mode == store.AnalyticsReadActiveRollup {
		if snapshot.Generation == nil {
			return errors.New("stored active session detail shape is invalid")
		}
		return validateSessionGeneration(*snapshot.Generation)
	}
	if snapshot.Mode == store.AnalyticsReadLightIndex {
		if snapshot.Generation != nil || len(snapshot.PricingVersions) != 0 || len(snapshot.UnpricedReasons) != 0 {
			return errors.New("stored light session detail shape is invalid")
		}
		return nil
	}
	if (snapshot.Mode != store.AnalyticsReadDetailFallback &&
		snapshot.Mode != store.AnalyticsReadAmbiguousFallback) || snapshot.Generation != nil ||
		len(snapshot.PricingVersions) != 0 || len(snapshot.UnpricedReasons) != 0 {
		return errors.New("stored fallback session detail shape is invalid")
	}
	return nil
}

func validateSessionGeneration(generation store.CostRollupGeneration) error {
	if generation.GenerationID == "" || generation.ReportingTimezone == "" ||
		generation.PricingSource == "" || generation.Currency == "" || generation.RollupVersion <= 0 {
		return errors.New("stored session pricing evidence is invalid")
	}
	return nil
}

func mapSessionItem(
	record store.SessionAnalyticsRecord,
	mode store.AnalyticsReadMode,
) (SessionItem, error) {
	if !validOpaqueIdentity(record.SessionID) || record.DisplayTitle == "" ||
		(record.Activity != store.SessionActivityActive && record.Activity != store.SessionActivityIdle) ||
		!validAttributionTuple(record.Project.ProjectID, record.Project.DisplayName) ||
		!validAttributionTuple(record.Model.ModelKey, record.Model.DisplayName) ||
		!validAttributionEvidence(record.TitleConfidence, record.TitleSource, record.TitleReason) ||
		!validAttributionEvidence(record.Project.Confidence, record.Project.Source, record.Project.Reason) ||
		!validAttributionEvidence(record.Model.Confidence, record.Model.Source, record.Model.Reason) {
		return SessionItem{}, errors.New("stored session item is invalid")
	}
	lastActivity, err := numericOrUnknown(
		record.LastActivityAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
	)
	if err != nil {
		return SessionItem{}, err
	}
	totals, err := mapSessionTotals(record.Rollup, mode)
	if err != nil {
		return SessionItem{}, err
	}
	return SessionItem{
		SessionID: record.SessionID, DisplayTitle: record.DisplayTitle,
		TitleConfidence: string(record.TitleConfidence), TitleSource: string(record.TitleSource),
		TitleReason: string(record.TitleReason), Activity: string(record.Activity),
		Project: AttributionValue{
			ID:          cloneStringPointer(record.Project.ProjectID),
			DisplayName: cloneStringPointer(record.Project.DisplayName),
			Confidence:  string(record.Project.Confidence), Source: string(record.Project.Source),
			Reason: string(record.Project.Reason),
		},
		Model: AttributionValue{
			ID:          cloneStringPointer(record.Model.ModelKey),
			DisplayName: cloneStringPointer(record.Model.DisplayName),
			Confidence:  string(record.Model.Confidence), Source: string(record.Model.Source),
			Reason: string(record.Model.Reason),
		},
		LastActivityAt: lastActivity, Totals: totals,
	}, nil
}

func mapSessionTotals(
	value *store.RollupTotals,
	mode store.AnalyticsReadMode,
) (UsageTotals, error) {
	if mode == store.AnalyticsReadLightIndex {
		return mapLightIndexSessionTotals(value)
	}
	if mode != store.AnalyticsReadActiveRollup {
		if value != nil {
			return UsageTotals{}, errors.New("fallback session totals must be absent")
		}
		return unknownUsageTotals()
	}
	if value == nil || value.TurnCount == 0 {
		return knownZeroUsageTotals()
	}
	return mapUsageTotals(*value, mode)
}

func mapLightIndexSessionTotals(value *store.RollupTotals) (UsageTotals, error) {
	if value == nil {
		return unknownUsageTotals()
	}
	result := UsageTotals{}
	var err error
	result.InputTokens, err = numericOrUnknown(value.InputTokens, basequery.NumericTokens, basequery.UnknownNeverLoaded)
	if err != nil {
		return UsageTotals{}, err
	}
	result.CachedInputTokens, err = numericOrUnknown(value.CachedInputTokens, basequery.NumericTokens, basequery.UnknownNeverLoaded)
	if err != nil {
		return UsageTotals{}, err
	}
	result.OutputTokens, err = numericOrUnknown(value.OutputTokens, basequery.NumericTokens, basequery.UnknownNeverLoaded)
	if err != nil {
		return UsageTotals{}, err
	}
	result.ReasoningTokens, err = numericOrUnknown(value.ReasoningTokens, basequery.NumericTokens, basequery.UnknownNeverLoaded)
	if err != nil {
		return UsageTotals{}, err
	}
	result.TotalTokens, err = numericOrUnknown(value.TotalTokens, basequery.NumericTokens, basequery.UnknownNeverLoaded)
	if err != nil {
		return UsageTotals{}, err
	}
	for _, target := range []struct {
		value  *basequery.NumericValue
		unit   basequery.NumericUnit
		reason basequery.UnknownReason
	}{
		{&result.TurnCount, basequery.NumericCount, basequery.UnknownUnavailable},
		{&result.EstimatedUSDMicros, basequery.NumericMicroUSD, basequery.UnknownNotComputed},
		{&result.PricedTurnCount, basequery.NumericCount, basequery.UnknownNotComputed},
		{&result.UnpricedTurnCount, basequery.NumericCount, basequery.UnknownNotComputed},
		{&result.FirstActivityAtMS, basequery.NumericMilliseconds, basequery.UnknownUnavailable},
		{&result.LastActivityAtMS, basequery.NumericMilliseconds, basequery.UnknownUnavailable},
	} {
		*target.value, err = basequery.UnknownNumeric(target.unit, target.reason)
		if err != nil {
			return UsageTotals{}, err
		}
	}
	return result, nil
}

func sessionDegradedReason(mode store.AnalyticsReadMode) DegradedReason {
	if mode == store.AnalyticsReadAmbiguousFallback {
		return DegradedRollupAmbiguous
	}
	return DegradedRollupMissing
}

func unknownUsageTotals() (UsageTotals, error) {
	result := UsageTotals{}
	var err error
	for _, target := range []struct {
		value *basequery.NumericValue
		unit  basequery.NumericUnit
	}{
		{value: &result.TurnCount, unit: basequery.NumericCount},
		{value: &result.InputTokens, unit: basequery.NumericTokens},
		{value: &result.CachedInputTokens, unit: basequery.NumericTokens},
		{value: &result.OutputTokens, unit: basequery.NumericTokens},
		{value: &result.ReasoningTokens, unit: basequery.NumericTokens},
		{value: &result.TotalTokens, unit: basequery.NumericTokens},
		{value: &result.EstimatedUSDMicros, unit: basequery.NumericMicroUSD},
		{value: &result.PricedTurnCount, unit: basequery.NumericCount},
		{value: &result.UnpricedTurnCount, unit: basequery.NumericCount},
		{value: &result.FirstActivityAtMS, unit: basequery.NumericMilliseconds},
		{value: &result.LastActivityAtMS, unit: basequery.NumericMilliseconds},
	} {
		*target.value, err = basequery.UnknownNumeric(target.unit, basequery.UnknownUnavailable)
		if err != nil {
			return UsageTotals{}, err
		}
	}
	return result, nil
}

func knownZeroUsageTotals() (UsageTotals, error) {
	zero := int64(0)
	return mapUsageTotals(store.RollupTotals{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero,
		ReasoningTokens: &zero, TotalTokens: &zero, EstimatedUSDMicros: &zero,
	}, store.AnalyticsReadActiveRollup)
}

func usageTotalsArePartial(value UsageTotals) bool {
	if value.TurnCount.Value == nil || value.InputTokens.Value == nil ||
		value.CachedInputTokens.Value == nil || value.OutputTokens.Value == nil ||
		value.ReasoningTokens.Value == nil || value.TotalTokens.Value == nil ||
		value.EstimatedUSDMicros.Value == nil || value.PricedTurnCount.Value == nil ||
		value.UnpricedTurnCount.Value == nil {
		return true
	}
	return *value.UnpricedTurnCount.Value > 0
}

func mapSessionReaderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, store.ErrNotFound) {
		return basequery.NewNotFoundFailure(err)
	}
	return basequery.NewUnavailableFailure(err)
}

func validOpaqueIdentity(value string) bool {
	return len(value) >= 1 && len(value) <= 512 && utf8.ValidString(value) &&
		strings.TrimSpace(value) != ""
}

func validAttributionTuple(identity, display *string) bool {
	if (identity == nil) != (display == nil) {
		return false
	}
	if identity == nil {
		return true
	}
	return *identity != "" && *display != "" && len(*identity) <= 512 && len(*display) <= 512
}

func validAttributionEvidence(
	confidence store.AttributionConfidence,
	source store.AttributionSource,
	reason store.AttributionReason,
) bool {
	validConfidence := confidence == store.AttributionConfidenceHigh ||
		confidence == store.AttributionConfidenceMedium ||
		confidence == store.AttributionConfidenceLow ||
		confidence == store.AttributionConfidenceUnknown
	validSource := source == store.AttributionSourceSessionIDFallback ||
		source == store.AttributionSourceAppServerName ||
		source == store.AttributionSourceRegisteredRoot ||
		source == store.AttributionSourceCWDPathDigest ||
		source == store.AttributionSourceModelCanonical ||
		source == store.AttributionSourceModelAlias ||
		source == store.AttributionSourceConflict ||
		source == store.AttributionSourceMissing ||
		source == store.AttributionSourceInvalidPath ||
		source == store.AttributionSourceInvalidModel
	validReason := reason == store.AttributionReasonStableIdentity ||
		reason == store.AttributionReasonRootMatched ||
		reason == store.AttributionReasonPathDerived ||
		reason == store.AttributionReasonObserved ||
		reason == store.AttributionReasonConflict ||
		reason == store.AttributionReasonMissing ||
		reason == store.AttributionReasonInvalid
	return validConfidence && validSource && validReason
}

func sessionCursorForStoreRecord(
	record store.SessionAnalyticsRecord,
	sortField string,
) store.SessionAnalyticsCursor {
	var value *int64
	switch sortField {
	case "totalTokens":
		if record.Rollup != nil {
			value = record.Rollup.TotalTokens
		}
	case "estimatedCost":
		if record.Rollup != nil {
			value = record.Rollup.EstimatedUSDMicros
		}
	default:
		value = record.LastActivityAtMS
	}
	return store.SessionAnalyticsCursor{
		SessionID: record.SessionID, Null: value == nil, Value: cloneInt64(value),
	}
}

func equalInt64Pointers(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
