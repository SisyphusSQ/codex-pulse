package runtimeinfo

import (
	"context"
	"errors"
	"strconv"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const healthCursorEndpoint = "runtime-health"

func (service *Service) ListHealth(
	ctx context.Context,
	request basequery.Request,
) (HealthListResponse, error) {
	if service == nil || service.runtime == nil {
		return HealthListResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return HealthListResponse{}, err
	}
	validated, err := validateRuntimeRequest(ctx, service.healthSpec, request, "lastSeenAt")
	if err != nil {
		return HealthListResponse{}, err
	}
	primary := validated.Sort[0]
	filter := store.RuntimeHealthQuery{
		Limit: validated.Page.Limit, Direction: runtimeDirection(primary.Direction),
	}
	if validated.Page.Cursor != nil {
		value, identity, err := decodeCursor(
			*validated.Page.Cursor, healthCursorEndpoint, primary.Field, primary.Direction,
		)
		if err != nil {
			return HealthListResponse{}, err
		}
		filter.After = &store.RuntimeHealthCursor{LastSeenAtMS: value, EventID: identity}
	}
	if err := applyHealthFilters(validated.Filters, &filter); err != nil {
		return HealthListResponse{}, err
	}
	page, err := service.runtime.RuntimeHealthPage(ctx, filter)
	if err != nil {
		return HealthListResponse{}, runtimeReadFailure(err)
	}
	return mapHealthList(page, validated.Page.Limit, primary)
}

func (service *Service) Health(
	ctx context.Context,
	request HealthDetailRequest,
) (HealthDetailResponse, error) {
	if service == nil || service.runtime == nil {
		return HealthDetailResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return HealthDetailResponse{}, err
	}
	if !validOpaqueIdentity(request.EventID) {
		return HealthDetailResponse{}, basequery.NewValidationFailure("eventId", nil)
	}
	event, err := service.runtime.RuntimeHealth(ctx, request.EventID)
	if err != nil {
		return HealthDetailResponse{}, runtimeReadFailure(err)
	}
	item, err := mapHealthItem(event)
	if err != nil {
		return HealthDetailResponse{}, basequery.NewUnavailableFailure(err)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return HealthDetailResponse{}, err
	}
	return HealthDetailResponse{Meta: meta, Item: item}, nil
}

func applyHealthFilters(filters []basequery.FilterTerm, target *store.RuntimeHealthQuery) error {
	seenFields := make(map[string]struct{}, len(filters))
	seenSeverities := make(map[store.HealthSeverity]struct{})
	seenDomains := make(map[store.HealthDomain]struct{})
	for _, filter := range filters {
		if _, exists := seenFields[filter.Field]; exists {
			return basequery.NewValidationFailure("filters", nil)
		}
		seenFields[filter.Field] = struct{}{}
		for _, value := range filter.Values {
			switch filter.Field {
			case "active":
				parsed, err := strconv.ParseBool(value)
				if err != nil || value != strconv.FormatBool(parsed) {
					return basequery.NewValidationFailure("filters.values", err)
				}
				target.Active = &parsed
			case "severity":
				severity := store.HealthSeverity(value)
				if !validHealthSeverity(severity) {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				if _, exists := seenSeverities[severity]; exists {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				seenSeverities[severity] = struct{}{}
				target.Severities = append(target.Severities, severity)
			case "domain":
				domain := store.HealthDomain(value)
				if !validHealthDomain(domain) {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				if _, exists := seenDomains[domain]; exists {
					return basequery.NewValidationFailure("filters.values", nil)
				}
				seenDomains[domain] = struct{}{}
				target.Domains = append(target.Domains, domain)
			default:
				return basequery.NewValidationFailure("filters.field", nil)
			}
		}
	}
	return nil
}

func mapHealthList(
	page store.RuntimeHealthPage,
	limit int,
	primary basequery.SortTerm,
) (HealthListResponse, error) {
	if page.MatchedCount < 0 || page.MatchedCount != page.Summary.Total ||
		len(page.Records) > limit || (page.NextCursor != nil && len(page.Records) == 0) {
		return HealthListResponse{}, basequery.NewUnavailableFailure(errors.New("health page is inconsistent"))
	}
	items := make([]HealthItem, len(page.Records))
	for index, event := range page.Records {
		item, err := mapHealthItem(event)
		if err != nil {
			return HealthListResponse{}, basequery.NewUnavailableFailure(err)
		}
		items[index] = item
	}
	var nextCursor *string
	var err error
	if page.NextCursor != nil {
		nextCursor, err = encodeCursor(
			healthCursorEndpoint, primary.Field, primary.Direction,
			page.NextCursor.LastSeenAtMS, page.NextCursor.EventID,
		)
		if err != nil {
			return HealthListResponse{}, basequery.NewUnavailableFailure(err)
		}
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, &basequery.PageInfo{
		Limit: limit, HasMore: nextCursor != nil, NextCursor: nextCursor,
	}, nil)
	if err != nil {
		return HealthListResponse{}, err
	}
	matched, err := knownNumeric(page.MatchedCount, basequery.NumericCount)
	if err != nil {
		return HealthListResponse{}, basequery.NewUnavailableFailure(err)
	}
	summary, err := mapHealthSummary(page.Summary, page.Lifecycle)
	if err != nil {
		return HealthListResponse{}, basequery.NewUnavailableFailure(err)
	}
	return HealthListResponse{Meta: meta, Items: items, MatchedCount: matched, Summary: summary}, nil
}

func mapHealthItem(event store.HealthEvent) (HealthItem, error) {
	if event.EventID == "" || !validHealthDomain(event.Domain) ||
		!validHealthSeverity(event.Severity) || !validHealthCode(event.Domain, event.Code) ||
		event.FirstSeenAtMS < 0 || event.LastSeenAtMS < event.FirstSeenAtMS ||
		event.OccurrenceCount < 1 || !validOpaqueIdentity(event.EventID) ||
		!validRuntimeErrorPointer(event.ErrorClass) || event.Fingerprint.String() == "" ||
		event.ResolvedAtMS != nil && *event.ResolvedAtMS < event.LastSeenAtMS ||
		event.UpdatedAtMS < event.LastSeenAtMS {
		return HealthItem{}, errors.New("health event is invalid")
	}
	firstSeen, err := knownNumeric(event.FirstSeenAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return HealthItem{}, err
	}
	lastSeen, err := knownNumeric(event.LastSeenAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return HealthItem{}, err
	}
	resolved, err := optionalNumeric(event.ResolvedAtMS, basequery.NumericMilliseconds, basequery.UnknownNotApplicable)
	if err != nil {
		return HealthItem{}, err
	}
	occurrences, err := knownNumeric(event.OccurrenceCount, basequery.NumericCount)
	if err != nil {
		return HealthItem{}, err
	}
	descriptor, ok := healthmodel.DescribeEvent(event.Domain, event.Code)
	if !ok {
		return HealthItem{}, errors.New("health event descriptor is unavailable")
	}
	item := HealthItem{
		EventID: event.EventID, Domain: string(event.Domain), Severity: string(event.Severity),
		Code: string(event.Code), Component: string(descriptor.Component), Rule: string(descriptor.Rule),
		Impact: string(descriptor.Impact), Protection: string(descriptor.Protection),
		ErrorClass:    runtimeErrorString(event.ErrorClass),
		FirstSeenAtMS: firstSeen, LastSeenAtMS: lastSeen, ResolvedAtMS: resolved,
		OccurrenceCount: occurrences, Active: event.ResolvedAtMS == nil,
		RecoveryAction: healthRecovery(event),
	}
	if event.SourceFileID != nil {
		if !validOpaqueIdentity(*event.SourceFileID) {
			return HealthItem{}, errors.New("health source identity is invalid")
		}
		item.SourceKey = cloneString("local_file:" + *event.SourceFileID)
	}
	if event.JobID != nil {
		if !validOpaqueIdentity(*event.JobID) {
			return HealthItem{}, errors.New("health job identity is invalid")
		}
		item.JobID = cloneString(*event.JobID)
	}
	return item, nil
}

func mapHealthSummary(
	value store.RuntimeHealthSummary,
	lifecycle *store.SchedulerLifecycle,
) (HealthSummary, error) {
	values := []int64{
		value.Total, value.Active, value.Resolved, value.Info,
		value.Warnings, value.Errors, value.Critical,
	}
	if value.Total < 0 || value.Active+value.Resolved != value.Total ||
		value.Info+value.Warnings+value.Errors+value.Critical != value.Total ||
		value.ActiveInfo+value.ActiveWarnings+value.ActiveErrors+value.ActiveCritical != value.Active ||
		value.ActiveInfo > value.Info || value.ActiveWarnings > value.Warnings ||
		value.ActiveErrors > value.Errors || value.ActiveCritical > value.Critical {
		return HealthSummary{}, errors.New("health summary is invalid")
	}
	mapped := make([]basequery.NumericValue, len(values))
	for index, current := range values {
		value, err := knownNumeric(current, basequery.NumericCount)
		if err != nil {
			return HealthSummary{}, err
		}
		mapped[index] = value
	}
	return HealthSummary{
		Level: healthLevel(value, lifecycle), Total: mapped[0], Active: mapped[1],
		Resolved: mapped[2], Info: mapped[3], Warnings: mapped[4], Errors: mapped[5],
		Critical: mapped[6],
	}, nil
}

func healthLevel(summary store.RuntimeHealthSummary, lifecycle *store.SchedulerLifecycle) HealthLevel {
	if summary.ActiveCritical > 0 || lifecycle != nil && lifecycle.Transition == store.LifecycleTransitionBlocked {
		return HealthBlocked
	}
	if lifecycle != nil && (lifecycle.UserPauseScope != store.LifecyclePauseNone ||
		lifecycle.SystemState == store.LifecycleSystemSleeping) {
		return HealthPaused
	}
	if summary.ActiveErrors > 0 || lifecycle == nil ||
		lifecycle.SourceState == store.LifecycleSourceUnknown ||
		lifecycle.SourceState == store.LifecycleSourceUnavailable {
		return HealthDegraded
	}
	if summary.ActiveWarnings > 0 || lifecycle != nil &&
		(lifecycle.Transition == store.LifecycleTransitionDraining ||
			lifecycle.Transition == store.LifecycleTransitionReconciling) {
		return HealthBusy
	}
	return HealthHealthy
}

func validHealthSeverity(value store.HealthSeverity) bool {
	return value == store.HealthInfo || value == store.HealthWarning ||
		value == store.HealthError || value == store.HealthCritical
}

func validHealthDomain(value store.HealthDomain) bool {
	switch value {
	case store.HealthDomainSource, store.HealthDomainJob, store.HealthDomainStore,
		store.HealthDomainPricing, store.HealthDomainRuntime:
		return true
	default:
		return false
	}
}

func validHealthCode(domain store.HealthDomain, value store.HealthCode) bool {
	_, ok := healthmodel.DescribeEvent(domain, value)
	return ok
}
