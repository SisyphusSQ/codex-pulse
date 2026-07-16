package runtimeinfo

import (
	"context"
	"errors"
	"strings"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const sourceCursorEndpoint = "runtime-sources"

func (service *Service) ListSources(
	ctx context.Context,
	request basequery.Request,
) (SourceListResponse, error) {
	if service == nil || service.runtime == nil {
		return SourceListResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return SourceListResponse{}, err
	}
	validated, err := validateRuntimeRequest(ctx, service.sourceSpec, request, "updatedAt")
	if err != nil {
		return SourceListResponse{}, err
	}
	primary := validated.Sort[0]
	filter := store.RuntimeSourceQuery{
		Limit: validated.Page.Limit, Direction: runtimeDirection(primary.Direction),
	}
	if validated.Page.Cursor != nil {
		value, identity, err := decodeCursor(
			*validated.Page.Cursor, sourceCursorEndpoint, primary.Field, primary.Direction,
		)
		if err != nil {
			return SourceListResponse{}, err
		}
		filter.After = &store.RuntimeSourceCursor{UpdatedAtMS: value, SourceKey: identity}
	}
	if err := applySourceFilters(validated.Filters, &filter); err != nil {
		return SourceListResponse{}, err
	}
	page, err := service.runtime.RuntimeSourcePage(ctx, filter)
	if err != nil {
		return SourceListResponse{}, runtimeReadFailure(err)
	}
	return mapSourceList(page, validated.Page.Limit, primary)
}

func (service *Service) Source(
	ctx context.Context,
	request SourceDetailRequest,
) (SourceDetailResponse, error) {
	if service == nil || service.runtime == nil {
		return SourceDetailResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return SourceDetailResponse{}, err
	}
	if !validSourceKey(request.SourceKey) {
		return SourceDetailResponse{}, basequery.NewValidationFailure("sourceKey", nil)
	}
	record, err := service.runtime.RuntimeSource(ctx, request.SourceKey)
	if err != nil {
		return SourceDetailResponse{}, runtimeReadFailure(err)
	}
	item, attention, err := mapSourceItem(record)
	if err != nil {
		return SourceDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	status, issues := responseCompleteness(attention)
	meta, err := basequery.NewResponseMeta(status, nil, issues)
	if err != nil {
		return SourceDetailResponse{}, err
	}
	return SourceDetailResponse{Meta: meta, Item: item}, nil
}

func applySourceFilters(filters []basequery.FilterTerm, target *store.RuntimeSourceQuery) error {
	seenFields := make(map[string]struct{}, len(filters))
	seenKinds := make(map[store.RuntimeSourceKind]struct{})
	seenStates := make(map[string]struct{})
	for _, filter := range filters {
		if _, exists := seenFields[filter.Field]; exists {
			return basequery.NewValidationFailure("filters", nil)
		}
		seenFields[filter.Field] = struct{}{}
		switch filter.Field {
		case "kind":
			for _, value := range filter.Values {
				switch SourceKind(value) {
				case SourceLocalFile:
					if _, exists := seenKinds[store.RuntimeSourceLocalFile]; exists {
						return basequery.NewValidationFailure("filters.values", nil)
					}
					seenKinds[store.RuntimeSourceLocalFile] = struct{}{}
					target.Kinds = append(target.Kinds, store.RuntimeSourceLocalFile)
				case SourceOnline:
					if _, exists := seenKinds[store.RuntimeSourceOnline]; exists {
						return basequery.NewValidationFailure("filters.values", nil)
					}
					seenKinds[store.RuntimeSourceOnline] = struct{}{}
					target.Kinds = append(target.Kinds, store.RuntimeSourceOnline)
				default:
					return basequery.NewValidationFailure("filters.values", nil)
				}
			}
		case "state":
			for _, value := range filter.Values {
				if !validSourceState(value) {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				if _, exists := seenStates[value]; exists {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				seenStates[value] = struct{}{}
				target.States = append(target.States, value)
			}
		default:
			return basequery.NewValidationFailure("filters.field", nil)
		}
	}
	return nil
}

func validSourceState(value string) bool {
	switch value {
	case string(store.SourceFileDiscovered), string(store.SourceFileActive),
		string(store.SourceFileCompleted), string(store.SourceFileUnavailable),
		string(store.SourceFileFailed), string(store.SourceFreshnessUnknown),
		string(store.SourceFreshnessCurrent), string(store.SourceFreshnessStale):
		return true
	default:
		return false
	}
}

func validSourceKey(value string) bool {
	if len(value) < 2 || len(value) > 512 {
		return false
	}
	for _, prefix := range []string{"local_file:", "online:"} {
		if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
			return validOpaqueIdentity(strings.TrimPrefix(value, prefix))
		}
	}
	return false
}

func mapSourceList(
	page store.RuntimeSourcePage,
	limit int,
	primary basequery.SortTerm,
) (SourceListResponse, error) {
	if page.MatchedCount < 0 || page.MatchedCount != page.Summary.Total ||
		page.Summary.LocalFiles < 0 || page.Summary.OnlineSources < 0 ||
		page.Summary.Attention < 0 || page.Summary.Attention > page.Summary.Total ||
		page.Summary.LocalFiles+page.Summary.OnlineSources != page.Summary.Total ||
		len(page.Records) > limit || (page.NextCursor != nil && len(page.Records) == 0) ||
		len(page.UnavailableKinds) > 2 {
		return SourceListResponse{}, basequery.NewUnavailableFailure(errors.New("source page is inconsistent"))
	}
	unavailableKinds, err := mapUnavailableSourceKinds(page.UnavailableKinds)
	if err != nil {
		return SourceListResponse{}, basequery.NewUnavailableFailure(err)
	}
	items := make([]SourceItem, len(page.Records))
	for index, record := range page.Records {
		item, _, err := mapSourceItem(record)
		if err != nil {
			return SourceListResponse{}, basequery.NewUnavailableFailure(err)
		}
		items[index] = item
	}
	var nextCursor *string
	if page.NextCursor != nil {
		nextCursor, err = encodeCursor(
			sourceCursorEndpoint, primary.Field, primary.Direction,
			page.NextCursor.UpdatedAtMS, page.NextCursor.SourceKey,
		)
		if err != nil {
			return SourceListResponse{}, basequery.NewUnavailableFailure(err)
		}
	}
	pageInfo := &basequery.PageInfo{
		Limit: limit, HasMore: nextCursor != nil, NextCursor: nextCursor,
	}
	status, issues := responseCompleteness(
		page.Summary.Attention > 0 || len(unavailableKinds) > 0,
	)
	meta, err := basequery.NewResponseMeta(status, pageInfo, issues)
	if err != nil {
		return SourceListResponse{}, err
	}
	matched, err := knownNumeric(page.MatchedCount, basequery.NumericCount)
	if err != nil {
		return SourceListResponse{}, basequery.NewUnavailableFailure(err)
	}
	summary, err := mapSourceSummary(page.Summary)
	if err != nil {
		return SourceListResponse{}, basequery.NewUnavailableFailure(err)
	}
	return SourceListResponse{
		Meta: meta, Items: items, MatchedCount: matched, Summary: summary,
		UnavailableKinds: unavailableKinds,
	}, nil
}

func mapUnavailableSourceKinds(values []store.RuntimeSourceKind) ([]SourceKind, error) {
	result := make([]SourceKind, 0, len(values))
	seen := make(map[SourceKind]struct{}, len(values))
	for _, value := range values {
		var mapped SourceKind
		switch value {
		case store.RuntimeSourceLocalFile:
			mapped = SourceLocalFile
		case store.RuntimeSourceOnline:
			mapped = SourceOnline
		default:
			return nil, errors.New("source unavailable kind is invalid")
		}
		if _, exists := seen[mapped]; exists {
			return nil, errors.New("source unavailable kind is duplicated")
		}
		seen[mapped] = struct{}{}
		result = append(result, mapped)
	}
	return result, nil
}

func mapSourceSummary(value store.RuntimeSourceSummary) (SourceSummary, error) {
	total, err := knownNumeric(value.Total, basequery.NumericCount)
	if err != nil {
		return SourceSummary{}, err
	}
	local, err := knownNumeric(value.LocalFiles, basequery.NumericCount)
	if err != nil {
		return SourceSummary{}, err
	}
	online, err := knownNumeric(value.OnlineSources, basequery.NumericCount)
	if err != nil {
		return SourceSummary{}, err
	}
	attention, err := knownNumeric(value.Attention, basequery.NumericCount)
	return SourceSummary{
		Total: total, LocalFiles: local, OnlineSources: online, Attention: attention,
	}, err
}

func mapSourceItem(record store.RuntimeSourceRecord) (SourceItem, bool, error) {
	if !validSourceKey(record.SourceKey) || record.UpdatedAtMS < 0 {
		return SourceItem{}, false, errors.New("source record identity is invalid")
	}
	updated, err := knownNumeric(record.UpdatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return SourceItem{}, false, err
	}
	notApplicableCount, err := unknownNumeric(basequery.NumericCount, basequery.UnknownNotApplicable)
	if err != nil {
		return SourceItem{}, false, err
	}
	notApplicableBytes, err := unknownNumeric(basequery.NumericBytes, basequery.UnknownNotApplicable)
	if err != nil {
		return SourceItem{}, false, err
	}
	notApplicableMS, err := unknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	if err != nil {
		return SourceItem{}, false, err
	}
	item := SourceItem{
		SourceKey: record.SourceKey, UpdatedAtMS: updated,
		SizeBytes: notApplicableBytes, ParsedBytes: notApplicableBytes,
		LastScannedAtMS: notApplicableMS, LastAttemptAtMS: notApplicableMS,
		LastSuccessAtMS: notApplicableMS, NextDueAtMS: notApplicableMS,
		ConsecutiveFailures: notApplicableCount, RecoveryAction: noRecovery(),
	}
	switch record.Kind {
	case store.RuntimeSourceLocalFile:
		if record.Local == nil || record.Online != nil ||
			record.Local.SourceFileID == "" || record.Local.Provider == "" ||
			record.SourceKey != "local_file:"+record.Local.SourceFileID ||
			record.Local.UpdatedAtMS != record.UpdatedAtMS || record.Local.SizeBytes < 0 ||
			record.Local.ParsedOffset < 0 || record.Local.ParsedOffset > record.Local.SizeBytes {
			return SourceItem{}, false, errors.New("local source record is inconsistent")
		}
		item.Kind = SourceLocalFile
		item.Provider = cloneString(record.Local.Provider)
		item.State = string(record.Local.State)
		if !validLocalSourceState(record.Local.State) ||
			!validRuntimeErrorPointer(record.Local.LastErrorClass) ||
			!validPublicToken(record.Local.Provider, 128) {
			return SourceItem{}, false, errors.New("local source public facts are invalid")
		}
		item.SizeBytes, err = knownNumeric(record.Local.SizeBytes, basequery.NumericBytes)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.ParsedBytes, err = knownNumeric(record.Local.ParsedOffset, basequery.NumericBytes)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.LastScannedAtMS, err = optionalNumeric(
			record.Local.LastScannedAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
		)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.ErrorClass = runtimeErrorString(record.Local.LastErrorClass)
		attention := record.Local.State == store.SourceFileFailed ||
			record.Local.State == store.SourceFileUnavailable || record.Local.LastErrorClass != nil
		item.RecoveryAction = sourceRecovery(record.Local.LastErrorClass, nil, attention)
		return item, attention, nil
	case store.RuntimeSourceOnline:
		if record.Online == nil || record.Local != nil || record.Online.SourceInstanceID == "" ||
			record.Online.SourceType == "" || record.SourceKey != "online:"+record.Online.SourceInstanceID ||
			record.Online.UpdatedAtMS != record.UpdatedAtMS || record.Online.ConsecutiveFailures < 0 {
			return SourceItem{}, false, errors.New("online source record is inconsistent")
		}
		if !validOnlineSourceState(record.Online.FreshnessState) ||
			!validRuntimeErrorPointer(record.Online.LastErrorClass) ||
			!validSourceFailurePointer(record.Online.LastFailureCode) ||
			!validPublicToken(record.Online.SourceType, 128) {
			return SourceItem{}, false, errors.New("online source public facts are invalid")
		}
		item.Kind = SourceOnline
		item.SourceType = cloneString(record.Online.SourceType)
		item.State = string(record.Online.FreshnessState)
		item.LastAttemptAtMS, err = optionalNumeric(
			record.Online.LastAttemptAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
		)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.LastSuccessAtMS, err = optionalNumeric(
			record.Online.LastSuccessAtMS, basequery.NumericMilliseconds, basequery.UnknownNeverLoaded,
		)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.NextDueAtMS, err = optionalNumeric(
			record.Online.NextDueAtMS, basequery.NumericMilliseconds, basequery.UnknownNotComputed,
		)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.ConsecutiveFailures, err = knownNumeric(
			record.Online.ConsecutiveFailures, basequery.NumericCount,
		)
		if err != nil {
			return SourceItem{}, false, err
		}
		item.ErrorClass = runtimeErrorString(record.Online.LastErrorClass)
		item.FailureCode = sourceFailureString(record.Online.LastFailureCode)
		attention := record.Online.FreshnessState == store.SourceFreshnessStale ||
			record.Online.FreshnessState == store.SourceFreshnessUnavailable ||
			record.Online.ConsecutiveFailures > 0 || record.Online.LastErrorClass != nil ||
			record.Online.LastFailureCode != nil
		item.RecoveryAction = sourceRecovery(
			record.Online.LastErrorClass, record.Online.LastFailureCode, attention,
		)
		return item, attention, nil
	default:
		return SourceItem{}, false, errors.New("source kind is invalid")
	}
}

func validLocalSourceState(value store.SourceFileState) bool {
	switch value {
	case store.SourceFileDiscovered, store.SourceFileActive, store.SourceFileCompleted,
		store.SourceFileUnavailable, store.SourceFileFailed:
		return true
	default:
		return false
	}
}

func validOnlineSourceState(value store.SourceFreshness) bool {
	switch value {
	case store.SourceFreshnessUnknown, store.SourceFreshnessCurrent,
		store.SourceFreshnessStale, store.SourceFreshnessUnavailable:
		return true
	default:
		return false
	}
}
