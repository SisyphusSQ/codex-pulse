package usagecost

import (
	"context"
	"errors"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func (service *Service) ListProjects(
	ctx context.Context,
	request basequery.Request,
) (ProjectListResponse, error) {
	if service == nil || service.projectReader == nil {
		return ProjectListResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProjectListResponse{}, err
	}
	validated, err := service.projectSpec.Validate(ctx, request)
	if err != nil {
		return ProjectListResponse{}, err
	}
	storeFilter, primarySort, err := projectStoreFilter(validated)
	if err != nil {
		return ProjectListResponse{}, err
	}
	if validated.Page.Cursor != nil {
		storeFilter.Cursor, err = decodeProjectCursor(
			*validated.Page.Cursor, primarySort.Field, primarySort.Direction,
		)
		if err != nil {
			return ProjectListResponse{}, err
		}
	}
	page, err := service.projectReader.ListProjectAnalytics(ctx, storeFilter)
	if err != nil {
		return ProjectListResponse{}, mapProjectReaderError(err)
	}
	response, err := mapProjectListResponse(page, *validated.TimeRange, validated.Page.Limit, primarySort)
	if err != nil {
		return ProjectListResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func (service *Service) ProjectDetail(
	ctx context.Context,
	request ProjectDetailRequest,
) (ProjectDetailResponse, error) {
	if service == nil || service.projectReader == nil {
		return ProjectDetailResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProjectDetailResponse{}, err
	}
	if !validOpaqueIdentity(request.DimensionKey) {
		return ProjectDetailResponse{}, basequery.NewValidationFailure("projectKey", nil)
	}
	validated, err := service.projectSpec.Validate(ctx, basequery.Request{TimeRange: &request.Range})
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	if validated.TimeRange == nil {
		return ProjectDetailResponse{}, basequery.NewValidationFailure("timeRange", nil)
	}
	sessionPage, err := normalizeProjectDetailPage(request.SessionPage, "sessionPage")
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	modelPage, err := normalizeProjectDetailPage(request.ModelPage, "modelPage")
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	var sessionCursor *store.ProjectSessionAnalyticsCursor
	if sessionPage.Cursor != nil {
		sessionCursor, err = decodeProjectSessionCursor(
			service.projectDetailCursorKey, *sessionPage.Cursor,
			request.DimensionKey, *validated.TimeRange,
		)
		if err != nil {
			return ProjectDetailResponse{}, err
		}
	}
	var modelCursor *store.ProjectModelAnalyticsCursor
	if modelPage.Cursor != nil {
		modelCursor, err = decodeProjectModelCursor(
			service.projectDetailCursorKey, *modelPage.Cursor,
			request.DimensionKey, *validated.TimeRange,
		)
		if err != nil {
			return ProjectDetailResponse{}, err
		}
	}
	snapshot, err := service.projectReader.ProjectAnalytics(ctx, store.ProjectAnalyticsDetailFilter{
		Range: store.AnalyticsRange{
			ReportingTimezone: validated.TimeRange.TimeZone,
			StartAtMS:         validated.TimeRange.StartAtMS, EndAtMS: validated.TimeRange.EndAtMS,
		},
		DimensionKey: request.DimensionKey, SessionLimit: sessionPage.Limit,
		SessionCursor: sessionCursor, ModelLimit: modelPage.Limit, ModelCursor: modelCursor,
	})
	if err != nil {
		return ProjectDetailResponse{}, mapProjectReaderError(err)
	}
	response, err := mapProjectDetailResponse(
		snapshot, *validated.TimeRange, request.DimensionKey,
		sessionPage.Limit, sessionPage.Cursor == nil, modelPage.Limit,
		service.projectDetailCursorKey,
	)
	if err != nil {
		return ProjectDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	return response, nil
}

func normalizeProjectDetailPage(
	page basequery.PageRequest,
	field string,
) (basequery.PageRequest, error) {
	limit := page.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return basequery.PageRequest{}, basequery.NewValidationFailure(field+".limit", nil)
	}
	normalized := basequery.PageRequest{Limit: limit}
	if page.Cursor != nil {
		cursor := *page.Cursor
		if cursor == "" {
			return basequery.PageRequest{}, basequery.NewValidationFailure(field+".cursor", nil)
		}
		normalized.Cursor = &cursor
	}
	return normalized, nil
}

func projectStoreFilter(
	request basequery.ValidatedRequest,
) (store.ProjectAnalyticsFilter, basequery.SortTerm, error) {
	if request.TimeRange == nil {
		return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
			basequery.NewValidationFailure("timeRange", nil)
	}
	if len(request.Sort) != 2 || request.Sort[0].Field == "projectKey" ||
		request.Sort[1].Field != "projectKey" || request.Sort[1].Direction != basequery.SortDescending {
		return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
			basequery.NewValidationFailure("sort", nil)
	}
	primary := request.Sort[0]
	filter := store.ProjectAnalyticsFilter{
		Range: store.AnalyticsRange{
			ReportingTimezone: request.TimeRange.TimeZone,
			StartAtMS:         request.TimeRange.StartAtMS, EndAtMS: request.TimeRange.EndAtMS,
		},
		Limit: request.Page.Limit,
	}
	switch primary.Field {
	case "lastActivityAt":
		filter.SortField = store.ProjectAnalyticsSortLastActivity
	case "totalTokens":
		filter.SortField = store.ProjectAnalyticsSortTotalTokens
	case "estimatedCost":
		filter.SortField = store.ProjectAnalyticsSortEstimatedCost
	case "displayName":
		filter.SortField = store.ProjectAnalyticsSortDisplayName
	default:
		return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
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
			return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters", nil)
		}
		seenFields[term.Field] = struct{}{}
		if hasDuplicateStrings(term.Values) {
			return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters.values", nil)
		}
		switch term.Field {
		case "projectId":
			filter.ProjectIDs = append([]string(nil), term.Values...)
		case "confidence":
			for _, value := range term.Values {
				if !validProjectConfidenceDTO(value) {
					return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
						basequery.NewValidationFailure("filters.values", nil)
				}
			}
			filter.Confidences = append([]string(nil), term.Values...)
		default:
			return store.ProjectAnalyticsFilter{}, basequery.SortTerm{},
				basequery.NewValidationFailure("filters.field", nil)
		}
	}
	if filter.ProjectIDs == nil {
		filter.ProjectIDs = make([]string, 0)
	}
	if filter.Confidences == nil {
		filter.Confidences = make([]string, 0)
	}
	return filter, primary, nil
}

func mapProjectListResponse(
	page store.ProjectAnalyticsPage,
	rangeValue basequery.UTCTimeRange,
	limit int,
	primarySort basequery.SortTerm,
) (ProjectListResponse, error) {
	if err := validateProjectPageShape(page, rangeValue, limit, primarySort.Field); err != nil {
		return ProjectListResponse{}, err
	}
	items := make([]ProjectItem, 0, len(page.Records))
	partial := false
	for _, record := range page.Records {
		item, err := mapProjectItem(record)
		if err != nil {
			return ProjectListResponse{}, err
		}
		if usageTotalsArePartial(item.Totals) {
			partial = true
		}
		items = append(items, item)
	}
	matchedCount, err := basequery.KnownNumeric(page.MatchedCount, basequery.NumericCount)
	if err != nil {
		return ProjectListResponse{}, err
	}
	globalTotals, err := mapProjectTotals(page.GlobalTotals)
	if err != nil {
		return ProjectListResponse{}, err
	}
	matchedTotals, err := mapProjectTotals(page.MatchedTotals)
	if err != nil {
		return ProjectListResponse{}, err
	}
	pageTotals, err := mapProjectTotals(page.PageTotals)
	if err != nil {
		return ProjectListResponse{}, err
	}
	if usageTotalsArePartial(globalTotals) || usageTotalsArePartial(matchedTotals) ||
		usageTotalsArePartial(pageTotals) {
		partial = true
	}
	versions, err := normalizedPricingVersions(page.PricingVersions)
	if err != nil {
		return ProjectListResponse{}, err
	}
	nextCursor, err := encodeProjectCursor(page.NextCursor, primarySort.Field, primarySort.Direction)
	if err != nil {
		return ProjectListResponse{}, err
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
		return ProjectListResponse{}, err
	}
	return ProjectListResponse{
		Meta: meta, Range: rangeValue, ReportingTimeZone: rangeValue.TimeZone,
		PricingSource: cloneString(page.Generation.PricingSource),
		Currency:      cloneString(page.Generation.Currency), PricingVersions: versions,
		Items: items, MatchedCount: matchedCount, GlobalTotals: globalTotals,
		MatchedTotals: matchedTotals, PageTotals: pageTotals,
	}, nil
}

func mapProjectDetailResponse(
	snapshot store.ProjectAnalyticsSnapshot,
	rangeValue basequery.UTCTimeRange,
	dimensionKey string,
	sessionLimit int,
	sessionFirstPage bool,
	modelLimit int,
	cursorKey projectDetailCursorKey,
) (ProjectDetailResponse, error) {
	if err := validateProjectSnapshotShape(
		snapshot, rangeValue, dimensionKey, sessionLimit, sessionFirstPage, modelLimit,
	); err != nil {
		return ProjectDetailResponse{}, err
	}
	item, err := mapProjectItem(snapshot.Record)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	globalTotals, err := mapProjectTotals(snapshot.GlobalTotals)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	daily := make([]ProjectDailyPoint, 0, len(snapshot.Daily))
	partial := usageTotalsArePartial(item.Totals) || usageTotalsArePartial(globalTotals)
	for _, row := range snapshot.Daily {
		point, err := mapProjectDailyPoint(row)
		if err != nil {
			return ProjectDetailResponse{}, err
		}
		if usageTotalsArePartial(point.Totals) {
			partial = true
		}
		daily = append(daily, point)
	}
	sessions := make([]ProjectSessionItem, 0, len(snapshot.Sessions))
	for _, record := range snapshot.Sessions {
		mapped, err := mapProjectSessionItem(record)
		if err != nil {
			return ProjectDetailResponse{}, err
		}
		if usageTotalsArePartial(mapped.Totals) {
			partial = true
		}
		sessions = append(sessions, mapped)
	}
	models := make([]ProjectModelItem, 0, len(snapshot.Models))
	for _, record := range snapshot.Models {
		mapped, err := mapProjectModelItem(record)
		if err != nil {
			return ProjectDetailResponse{}, err
		}
		if usageTotalsArePartial(mapped.Totals) {
			partial = true
		}
		models = append(models, mapped)
	}
	nextSessionCursor, err := encodeProjectSessionCursor(
		cursorKey, snapshot.NextSessionCursor, rangeValue,
	)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	nextModelCursor, err := encodeProjectModelCursor(
		cursorKey, snapshot.NextModelCursor, rangeValue,
	)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	versions, err := normalizedPricingVersions(snapshot.PricingVersions)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	status := basequery.ResponseComplete
	var issues []basequery.ErrorCode
	if partial {
		status = basequery.ResponsePartial
		issues = []basequery.ErrorCode{basequery.ErrorPartial}
	}
	meta, err := basequery.NewResponseMeta(status, nil, issues)
	if err != nil {
		return ProjectDetailResponse{}, err
	}
	return ProjectDetailResponse{
		Meta: meta, Range: rangeValue, ReportingTimeZone: rangeValue.TimeZone,
		PricingSource: cloneString(snapshot.Generation.PricingSource),
		Currency:      cloneString(snapshot.Generation.Currency), PricingVersions: versions,
		Item: item, Daily: daily,
		SessionPage: basequery.PageInfo{
			Limit: sessionLimit, HasMore: nextSessionCursor != nil, NextCursor: nextSessionCursor,
		},
		Sessions: sessions,
		ModelPage: basequery.PageInfo{
			Limit: modelLimit, HasMore: nextModelCursor != nil, NextCursor: nextModelCursor,
		},
		Models: models, GlobalTotals: globalTotals,
	}, nil
}

func mapProjectDailyPoint(row store.ProjectUsageDaily) (ProjectDailyPoint, error) {
	bucket, err := basequery.KnownNumeric(row.BucketStartMS, basequery.NumericMilliseconds)
	if err != nil {
		return ProjectDailyPoint{}, err
	}
	totals, err := mapUsageTotals(row.RollupTotals, store.AnalyticsReadActiveRollup)
	if err != nil {
		return ProjectDailyPoint{}, err
	}
	return ProjectDailyPoint{
		BucketStartAt: bucket, Confidence: row.AttributionConfidence,
		Source: row.AttributionSource, Reason: row.AttributionReason, Totals: totals,
	}, nil
}

func mapProjectSessionItem(
	record store.ProjectSessionAnalyticsRecord,
) (ProjectSessionItem, error) {
	if !validOpaqueIdentity(record.SessionID) || record.DisplayTitle == "" ||
		!validProjectAttributionDTO(
			string(record.TitleConfidence), string(record.TitleSource), string(record.TitleReason),
		) || !validAttributionTuple(record.Model.ModelKey, record.Model.DisplayName) ||
		!validProjectAttributionDTO(
			string(record.Model.Confidence), string(record.Model.Source), string(record.Model.Reason),
		) || (record.Activity != store.SessionActivityActive && record.Activity != store.SessionActivityIdle) ||
		record.LastActivityAtMS < 0 || record.LastActivityAtMS != record.Totals.LastActivityAtMS {
		return ProjectSessionItem{}, errors.New("stored project session item is invalid")
	}
	lastActivity, err := basequery.KnownNumeric(
		record.LastActivityAtMS, basequery.NumericMilliseconds,
	)
	if err != nil {
		return ProjectSessionItem{}, err
	}
	totals, err := mapUsageTotals(record.Totals, store.AnalyticsReadActiveRollup)
	if err != nil {
		return ProjectSessionItem{}, err
	}
	return ProjectSessionItem{
		SessionID: record.SessionID, DisplayTitle: record.DisplayTitle,
		TitleConfidence: string(record.TitleConfidence), TitleSource: string(record.TitleSource),
		TitleReason: string(record.TitleReason),
		Model: AttributionValue{
			ID:          cloneStringPointer(record.Model.ModelKey),
			DisplayName: cloneStringPointer(record.Model.DisplayName),
			Confidence:  string(record.Model.Confidence), Source: string(record.Model.Source),
			Reason: string(record.Model.Reason),
		},
		Activity: string(record.Activity), LastActivityAt: lastActivity, Totals: totals,
	}, nil
}

func mapProjectModelItem(
	record store.ProjectModelAnalyticsRecord,
) (ProjectModelItem, error) {
	if !validOpaqueIdentity(record.DimensionKey) ||
		!validAttributionTuple(record.Model.ModelKey, record.Model.DisplayName) ||
		!validProjectAttributionDTO(
			string(record.Model.Confidence), string(record.Model.Source), string(record.Model.Reason),
		) || (record.Model.ModelKey != nil && *record.Model.ModelKey != record.DimensionKey) ||
		(record.Model.ModelKey == nil && record.DimensionKey != "unknown|"+
			string(record.Model.Confidence)+"|"+string(record.Model.Source)+"|"+
			string(record.Model.Reason)) {
		return ProjectModelItem{}, errors.New("stored project model item is invalid")
	}
	totals, err := mapUsageTotals(record.Totals, store.AnalyticsReadActiveRollup)
	if err != nil {
		return ProjectModelItem{}, err
	}
	return ProjectModelItem{
		DimensionKey: record.DimensionKey,
		Model: AttributionValue{
			ID:          cloneStringPointer(record.Model.ModelKey),
			DisplayName: cloneStringPointer(record.Model.DisplayName),
			Confidence:  string(record.Model.Confidence), Source: string(record.Model.Source),
			Reason: string(record.Model.Reason),
		},
		Totals: totals,
	}, nil
}

func validateProjectPageShape(
	page store.ProjectAnalyticsPage,
	rangeValue basequery.UTCTimeRange,
	limit int,
	sortField string,
) error {
	if err := validateSessionGeneration(page.Generation); err != nil {
		return err
	}
	if page.Generation.ReportingTimezone != rangeValue.TimeZone || page.Records == nil ||
		page.PricingVersions == nil || page.MatchedCount < 0 || len(page.Records) > limit ||
		int64(len(page.Records)) > page.MatchedCount {
		return errors.New("stored project page shape is invalid")
	}
	if page.GlobalTotals.PricedTurnCount > 0 && len(page.PricingVersions) == 0 {
		return errors.New("stored project pricing evidence is incomplete")
	}
	for _, record := range page.Records {
		if err := validateProjectRecordDecorations(record, page.Generation, rangeValue); err != nil {
			return err
		}
	}
	if page.NextCursor != nil {
		if len(page.Records) != limit || len(page.Records) == 0 {
			return errors.New("stored project next cursor cardinality is invalid")
		}
		expected := projectCursorForStoreRecord(page.Records[len(page.Records)-1], sortField)
		if !equalProjectStoreCursor(expected, page.NextCursor) {
			return errors.New("stored project next cursor is inconsistent")
		}
	}
	return nil
}

func validateProjectSnapshotShape(
	snapshot store.ProjectAnalyticsSnapshot,
	rangeValue basequery.UTCTimeRange,
	dimensionKey string,
	sessionLimit int,
	sessionFirstPage bool,
	modelLimit int,
) error {
	if err := validateSessionGeneration(snapshot.Generation); err != nil {
		return err
	}
	if snapshot.Generation.ReportingTimezone != rangeValue.TimeZone || snapshot.Daily == nil ||
		snapshot.Sessions == nil || snapshot.Models == nil || snapshot.PricingVersions == nil ||
		snapshot.Record.DimensionKey != dimensionKey || len(snapshot.Sessions) > sessionLimit ||
		len(snapshot.Models) > modelLimit || snapshot.Record.SessionCount < int64(len(snapshot.Sessions)) {
		return errors.New("stored project detail shape is invalid")
	}
	if err := validateProjectRecordDecorations(snapshot.Record, snapshot.Generation, rangeValue); err != nil {
		return err
	}
	if snapshot.Record.Totals.TurnCount > 0 && len(snapshot.Daily) == 0 {
		return errors.New("stored project detail daily rows are missing")
	}
	if sessionFirstPage && ((snapshot.NextSessionCursor == nil &&
		snapshot.Record.SessionCount != int64(len(snapshot.Sessions))) ||
		(snapshot.NextSessionCursor != nil &&
			snapshot.Record.SessionCount <= int64(len(snapshot.Sessions)))) {
		return errors.New("stored project first session page cardinality is inconsistent")
	}
	if snapshot.GlobalTotals.PricedTurnCount > 0 && len(snapshot.PricingVersions) == 0 {
		return errors.New("stored project detail pricing evidence is incomplete")
	}
	previousBucket := int64(-1)
	for _, row := range snapshot.Daily {
		if row.GenerationID != snapshot.Generation.GenerationID ||
			row.ReportingTimezone != rangeValue.TimeZone || row.DimensionKey != dimensionKey ||
			row.BucketStartMS < rangeValue.StartAtMS || row.BucketStartMS >= rangeValue.EndAtMS ||
			row.BucketStartMS <= previousBucket ||
			!validProjectAttributionDTO(row.AttributionConfidence, row.AttributionSource, row.AttributionReason) ||
			!validAttributionTuple(row.ProjectID, row.ProjectDisplayName) {
			return errors.New("stored project daily shape is invalid")
		}
		if (row.ProjectID != nil && *row.ProjectID != dimensionKey) ||
			(row.ProjectID == nil && dimensionKey != "unknown|"+row.AttributionConfidence+"|"+
				row.AttributionSource+"|"+row.AttributionReason) {
			return errors.New("stored project daily identity is inconsistent")
		}
		previousBucket = row.BucketStartMS
	}
	for index, record := range snapshot.Sessions {
		if _, err := mapProjectSessionItem(record); err != nil {
			return err
		}
		if index > 0 {
			previous := snapshot.Sessions[index-1]
			if previous.LastActivityAtMS < record.LastActivityAtMS ||
				(previous.LastActivityAtMS == record.LastActivityAtMS &&
					previous.SessionID <= record.SessionID) {
				return errors.New("stored project session page order is invalid")
			}
		}
	}
	for index, record := range snapshot.Models {
		if _, err := mapProjectModelItem(record); err != nil {
			return err
		}
		if index > 0 && !projectModelRecordComesBefore(snapshot.Models[index-1], record) {
			return errors.New("stored project model page order is invalid")
		}
	}
	if snapshot.NextSessionCursor != nil {
		if len(snapshot.Sessions) != sessionLimit || len(snapshot.Sessions) == 0 {
			return errors.New("stored project session next cursor cardinality is invalid")
		}
		last := snapshot.Sessions[len(snapshot.Sessions)-1]
		if snapshot.NextSessionCursor.DimensionKey != dimensionKey ||
			snapshot.NextSessionCursor.GenerationID != snapshot.Generation.GenerationID ||
			snapshot.NextSessionCursor.SessionID != last.SessionID ||
			snapshot.NextSessionCursor.LastActivityAtMS != last.LastActivityAtMS {
			return errors.New("stored project session next cursor is inconsistent")
		}
	}
	if snapshot.NextModelCursor != nil {
		if len(snapshot.Models) != modelLimit || len(snapshot.Models) == 0 {
			return errors.New("stored project model next cursor cardinality is invalid")
		}
		last := snapshot.Models[len(snapshot.Models)-1]
		if snapshot.NextModelCursor.DimensionKey != dimensionKey ||
			snapshot.NextModelCursor.GenerationID != snapshot.Generation.GenerationID ||
			snapshot.NextModelCursor.ModelDimensionKey != last.DimensionKey ||
			snapshot.NextModelCursor.Null != (last.Totals.TotalTokens == nil) ||
			!equalInt64Pointers(snapshot.NextModelCursor.TotalTokens, last.Totals.TotalTokens) {
			return errors.New("stored project model next cursor is inconsistent")
		}
	}
	return nil
}

func projectModelRecordComesBefore(
	left store.ProjectModelAnalyticsRecord,
	right store.ProjectModelAnalyticsRecord,
) bool {
	leftTokens, rightTokens := left.Totals.TotalTokens, right.Totals.TotalTokens
	if leftTokens == nil {
		return rightTokens == nil && left.DimensionKey > right.DimensionKey
	}
	if rightTokens == nil {
		return true
	}
	if *leftTokens != *rightTokens {
		return *leftTokens > *rightTokens
	}
	return left.DimensionKey > right.DimensionKey
}

func validateProjectRecordDecorations(
	record store.ProjectAnalyticsRecord,
	generation store.CostRollupGeneration,
	rangeValue basequery.UTCTimeRange,
) error {
	if record.SessionCount <= 0 || record.Trend == nil || len(record.Trend) == 0 ||
		len(record.Trend) > 30 {
		return errors.New("stored project decorations are invalid")
	}
	previousBucket := int64(-1)
	for _, row := range record.Trend {
		if row.GenerationID != generation.GenerationID ||
			row.ReportingTimezone != rangeValue.TimeZone || row.DimensionKey != record.DimensionKey ||
			row.BucketStartMS < rangeValue.StartAtMS || row.BucketStartMS >= rangeValue.EndAtMS ||
			row.BucketStartMS <= previousBucket ||
			!validProjectAttributionDTO(
				row.AttributionConfidence, row.AttributionSource, row.AttributionReason,
			) || !validAttributionTuple(row.ProjectID, row.ProjectDisplayName) {
			return errors.New("stored project trend is invalid")
		}
		if (row.ProjectID != nil && *row.ProjectID != record.DimensionKey) ||
			(row.ProjectID == nil && record.DimensionKey != "unknown|"+
				row.AttributionConfidence+"|"+row.AttributionSource+"|"+row.AttributionReason) {
			return errors.New("stored project trend identity is inconsistent")
		}
		previousBucket = row.BucketStartMS
	}
	return nil
}

func mapProjectItem(record store.ProjectAnalyticsRecord) (ProjectItem, error) {
	if !validOpaqueIdentity(record.DimensionKey) ||
		!validAttributionTuple(record.ProjectID, record.ProjectDisplayName) ||
		!validProjectAttributionDTO(
			record.AttributionConfidence, record.AttributionSource, record.AttributionReason,
		) {
		return ProjectItem{}, errors.New("stored project item is invalid")
	}
	if record.ProjectID == nil && record.DimensionKey != "unknown|"+record.AttributionConfidence+"|"+
		record.AttributionSource+"|"+record.AttributionReason {
		return ProjectItem{}, errors.New("stored unknown project identity is invalid")
	}
	if record.ProjectID != nil && *record.ProjectID != record.DimensionKey {
		return ProjectItem{}, errors.New("stored project identity is inconsistent")
	}
	sessionCount, err := basequery.KnownNumeric(record.SessionCount, basequery.NumericCount)
	if err != nil {
		return ProjectItem{}, err
	}
	trend := make([]ProjectDailyPoint, 0, len(record.Trend))
	for _, row := range record.Trend {
		point, err := mapProjectDailyPoint(row)
		if err != nil {
			return ProjectItem{}, err
		}
		trend = append(trend, point)
	}
	totals, err := mapUsageTotals(record.Totals, store.AnalyticsReadActiveRollup)
	if err != nil {
		return ProjectItem{}, err
	}
	return ProjectItem{
		DimensionKey: record.DimensionKey,
		Project: AttributionValue{
			ID: cloneStringPointer(record.ProjectID), DisplayName: cloneStringPointer(record.ProjectDisplayName),
			Confidence: record.AttributionConfidence, Source: record.AttributionSource,
			Reason: record.AttributionReason,
		},
		SessionCount: sessionCount, Trend: trend, Totals: totals,
	}, nil
}

func mapProjectTotals(value store.RollupTotals) (UsageTotals, error) {
	if value.TurnCount == 0 {
		return knownZeroUsageTotals()
	}
	return mapUsageTotals(value, store.AnalyticsReadActiveRollup)
}

func projectCursorForStoreRecord(
	record store.ProjectAnalyticsRecord,
	sortField string,
) *store.ProjectAnalyticsCursor {
	cursor := &store.ProjectAnalyticsCursor{DimensionKey: record.DimensionKey}
	switch sortField {
	case "totalTokens":
		cursor.NumericValue = cloneInt64(record.Totals.TotalTokens)
	case "estimatedCost":
		cursor.NumericValue = cloneInt64(record.Totals.EstimatedUSDMicros)
	case "displayName":
		cursor.TextValue = cloneStringPointer(record.ProjectDisplayName)
	default:
		cursor.NumericValue = cloneInt64(&record.Totals.LastActivityAtMS)
	}
	cursor.Null = cursor.NumericValue == nil && cursor.TextValue == nil
	return cursor
}

func equalProjectStoreCursor(left, right *store.ProjectAnalyticsCursor) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.DimensionKey == right.DimensionKey && left.Null == right.Null &&
		equalInt64Pointers(left.NumericValue, right.NumericValue) &&
		equalStringPointers(left.TextValue, right.TextValue)
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validProjectAttributionDTO(confidence, source, reason string) bool {
	validSource := source == string(store.AttributionSourceSessionIDFallback) ||
		source == string(store.AttributionSourceRegisteredRoot) ||
		source == string(store.AttributionSourceCWDPathDigest) ||
		source == string(store.AttributionSourceModelCanonical) ||
		source == string(store.AttributionSourceModelAlias) ||
		source == string(store.AttributionSourceConflict) ||
		source == string(store.AttributionSourceMissing) ||
		source == string(store.AttributionSourceInvalidPath) ||
		source == string(store.AttributionSourceInvalidModel) || source == "mixed"
	validReason := reason == string(store.AttributionReasonStableIdentity) ||
		reason == string(store.AttributionReasonRootMatched) ||
		reason == string(store.AttributionReasonPathDerived) ||
		reason == string(store.AttributionReasonObserved) ||
		reason == string(store.AttributionReasonConflict) ||
		reason == string(store.AttributionReasonMissing) ||
		reason == string(store.AttributionReasonInvalid) || reason == "mixed"
	return validProjectConfidenceDTO(confidence) && validSource && validReason
}

func validProjectConfidenceDTO(value string) bool {
	return value == string(store.AttributionConfidenceHigh) ||
		value == string(store.AttributionConfidenceMedium) ||
		value == string(store.AttributionConfidenceLow) ||
		value == string(store.AttributionConfidenceUnknown)
}

func mapProjectReaderError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, store.ErrNotFound) {
		return basequery.NewNotFoundFailure(err)
	}
	return basequery.NewUnavailableFailure(err)
}
