package usagecost

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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
	snapshot, err := service.sessionReader.SessionAnalytics(ctx, store.SessionAnalyticsDetailFilter{
		SessionID: request.SessionID, ReportingTimezone: cloneStringPointer(request.ReportingTimezone),
	})
	if err != nil {
		return SessionDetailResponse{}, mapSessionReaderError(err)
	}
	response, err := mapSessionDetailResponse(snapshot)
	if err != nil {
		return SessionDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
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
) (SessionDetailResponse, error) {
	if err := validateSessionSnapshotShape(snapshot); err != nil {
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
	partial := snapshot.Mode != store.AnalyticsReadActiveRollup || usageTotalsArePartial(item.Totals)
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
	if (page.Mode != store.AnalyticsReadDetailFallback &&
		page.Mode != store.AnalyticsReadAmbiguousFallback) || page.Generation != nil ||
		page.MatchedTotals != nil || page.PageTotals != nil {
		return errors.New("stored fallback session page shape is invalid")
	}
	return nil
}

func validateSessionSnapshotShape(snapshot store.SessionAnalyticsSnapshot) error {
	if snapshot.PricingVersions == nil || snapshot.UnpricedReasons == nil {
		return errors.New("stored session detail evidence shape is invalid")
	}
	if snapshot.Mode == store.AnalyticsReadActiveRollup {
		if snapshot.Generation == nil {
			return errors.New("stored active session detail shape is invalid")
		}
		return validateSessionGeneration(*snapshot.Generation)
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
