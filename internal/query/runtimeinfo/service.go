package runtimeinfo

import (
	"context"
	"errors"
	"fmt"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidService = errors.New("runtime info query service is invalid")

type QuotaReader interface {
	Query(context.Context, int64) (quotaquery.CurrentResponse, error)
}

type RuntimeReader interface {
	MetricsSnapshot(context.Context, store.MetricsSnapshotFilter) (store.MetricsSnapshot, error)
	RuntimeSourcePage(context.Context, store.RuntimeSourceQuery) (store.RuntimeSourcePage, error)
	RuntimeSource(context.Context, string) (store.RuntimeSourceRecord, error)
	RuntimeJobPage(context.Context, store.RuntimeJobQuery) (store.RuntimeJobPage, error)
	RuntimeJob(context.Context, string) (store.RuntimeJobRecord, error)
	RuntimeHealthPage(context.Context, store.RuntimeHealthQuery) (store.RuntimeHealthPage, error)
	RuntimeHealth(context.Context, string) (store.HealthEvent, error)
}

type PreferencesReader interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}

type Dependencies struct {
	Quota       QuotaReader
	Runtime     RuntimeReader
	Preferences PreferencesReader
}

type Service struct {
	quota       QuotaReader
	runtime     RuntimeReader
	preferences PreferencesReader
	sourceSpec  basequery.Specification
	jobSpec     basequery.Specification
	healthSpec  basequery.Specification
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Quota == nil || dependencies.Runtime == nil || dependencies.Preferences == nil {
		return nil, ErrInvalidService
	}
	sourceSpec, err := runtimeSpecification(
		"updatedAt", "sourceKey",
		[]basequery.FilterField{
			{Field: "kind", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "state", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
		},
	)
	if err != nil {
		return nil, ErrInvalidService
	}
	jobSpec, err := runtimeSpecification(
		"updatedAt", "jobId",
		[]basequery.FilterField{
			{Field: "state", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "phase", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
		},
	)
	if err != nil {
		return nil, ErrInvalidService
	}
	healthSpec, err := runtimeSpecification(
		"lastSeenAt", "eventId",
		[]basequery.FilterField{
			{Field: "active", Operators: []basequery.FilterOperator{basequery.FilterEqual}},
			{Field: "severity", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
			{Field: "domain", Operators: []basequery.FilterOperator{basequery.FilterEqual, basequery.FilterIn}},
		},
	)
	if err != nil {
		return nil, ErrInvalidService
	}
	return &Service{
		quota: dependencies.Quota, runtime: dependencies.Runtime,
		preferences: dependencies.Preferences, sourceSpec: sourceSpec, jobSpec: jobSpec,
		healthSpec: healthSpec,
	}, nil
}

func runtimeSpecification(
	primary string,
	identity string,
	filters []basequery.FilterField,
) (basequery.Specification, error) {
	return basequery.NewSpecification(basequery.SpecificationConfig{
		DefaultLimit: 50, MaxLimit: 100, MaxRangeDays: 1,
		SortFields: []string{primary, identity}, FilterFields: filters,
		DefaultSort: []basequery.SortTerm{{Field: primary, Direction: basequery.SortDescending}},
		TieBreaker:  basequery.SortTerm{Field: identity, Direction: basequery.SortDescending},
	})
}

func (service *Service) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (QuotaCurrentResponse, error) {
	if service == nil || service.quota == nil {
		return QuotaCurrentResponse{}, ErrInvalidService
	}
	ctx, err := queryContext(ctx)
	if err != nil {
		return QuotaCurrentResponse{}, err
	}
	if evaluatedAtMS < 0 || evaluatedAtMS > basequery.JavaScriptMaxSafeInteger {
		return QuotaCurrentResponse{}, basequery.NewValidationFailure("evaluatedAtMS", nil)
	}
	current, err := service.quota.Query(ctx, evaluatedAtMS)
	if err != nil {
		return QuotaCurrentResponse{}, runtimeReadFailure(err)
	}
	if current.Version != quotaquery.CurrentContractVersion || current.EvaluatedAtMS != evaluatedAtMS ||
		current.Windows == nil || current.Sources == nil {
		return QuotaCurrentResponse{}, basequery.NewUnavailableFailure(
			fmt.Errorf("quota current response is inconsistent"),
		)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return QuotaCurrentResponse{}, err
	}
	return QuotaCurrentResponse{Meta: meta, Current: current}, nil
}

func queryContext(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ctx, nil
}

func runtimeReadFailure(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, basequery.ErrNotFound) {
		return basequery.NewNotFoundFailure(err)
	}
	return basequery.NewUnavailableFailure(err)
}

func validateRuntimeRequest(
	ctx context.Context,
	specification basequery.Specification,
	request basequery.Request,
	primary string,
) (basequery.ValidatedRequest, error) {
	if request.TimeRange != nil {
		return basequery.ValidatedRequest{}, basequery.NewValidationFailure("timeRange", nil)
	}
	validated, err := specification.Validate(ctx, request)
	if err != nil {
		return basequery.ValidatedRequest{}, err
	}
	if len(validated.Sort) != 2 || validated.Sort[0].Field != primary ||
		validated.Sort[0].Direction != validated.Sort[1].Direction {
		return basequery.ValidatedRequest{}, basequery.NewValidationFailure("sort", nil)
	}
	return validated, nil
}

func runtimeDirection(value basequery.SortDirection) store.RuntimeQueryDirection {
	if value == basequery.SortAscending {
		return store.RuntimeQueryAscending
	}
	return store.RuntimeQueryDescending
}
