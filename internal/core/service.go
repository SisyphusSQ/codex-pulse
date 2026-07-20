package core

import (
	"context"
	"errors"
	"sync"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	ContractVersion = "core-rpc-v1"
)

var (
	ErrService = errors.New("core service is unavailable")
	ErrQuery   = errors.New("core query failed")
)

type usageCostQuery interface {
	UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error)
	ListSessions(context.Context, basequery.Request) (usagecost.SessionListResponse, error)
	SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error)
	ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error)
	ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error)
}

type runtimeInfoQuery interface {
	QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error)
	ListSources(context.Context, basequery.Request) (runtimeinfo.SourceListResponse, error)
	Source(context.Context, runtimeinfo.SourceDetailRequest) (runtimeinfo.SourceDetailResponse, error)
	ListJobs(context.Context, basequery.Request) (runtimeinfo.JobListResponse, error)
	Job(context.Context, runtimeinfo.JobDetailRequest) (runtimeinfo.JobDetailResponse, error)
	ListHealth(context.Context, basequery.Request) (runtimeinfo.HealthListResponse, error)
	Health(context.Context, runtimeinfo.HealthDetailRequest) (runtimeinfo.HealthDetailResponse, error)
	DataHealth(context.Context, int64) (runtimeinfo.DataHealthResponse, error)
	Settings(context.Context) (runtimeinfo.SettingsResponse, error)
}

type quotaRefreshCommand interface {
	RequestQuotaRefresh(context.Context, quotaonline.RefreshSource) (store.SourceRefreshSchedule, error)
}

type sessionDeepIndexCommand interface {
	DeepIndexSession(context.Context, string) (lightindex.DeepIndexResult, error)
}

type healthProjectionQuery interface {
	Projection() healthmodel.Projection
}

type QueryObserver interface {
	Observe(time.Duration)
}

type ServiceConfig struct {
	UsageCost        usageCostQuery
	RuntimeInfo      runtimeInfoQuery
	QuotaRefresh     quotaRefreshCommand
	RuntimeControls  runtimeControlCommand
	HealthProjection healthProjectionQuery
	QueryObserver    QueryObserver
	SessionDeepIndex sessionDeepIndexCommand
}

// Service 是 Go Helper 唯一的业务 facade；未导出依赖阻止 Store、文件系统和凭据原语进入 RPC surface。
type Service struct {
	usageCost        usageCostQuery
	runtimeInfo      runtimeInfoQuery
	quotaMu          sync.RWMutex
	quotaRefresh     quotaRefreshCommand
	runtimeMu        sync.RWMutex
	runtimeControls  runtimeControlCommand
	deepMu           sync.RWMutex
	sessionDeepIndex sessionDeepIndexCommand
	healthMu         sync.RWMutex
	healthProjection healthProjectionQuery
	queryObserver    QueryObserver
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.UsageCost == nil || config.RuntimeInfo == nil {
		return nil, ErrService
	}
	return &Service{
		usageCost:        config.UsageCost,
		runtimeInfo:      config.RuntimeInfo,
		quotaRefresh:     config.QuotaRefresh,
		runtimeControls:  config.RuntimeControls,
		sessionDeepIndex: config.SessionDeepIndex,
		queryObserver:    config.QueryObserver,
		healthProjection: config.HealthProjection,
	}, nil
}

func (service *Service) bindSessionDeepIndex(command sessionDeepIndexCommand) error {
	if service == nil || command == nil {
		return ErrService
	}
	service.deepMu.Lock()
	defer service.deepMu.Unlock()
	if service.sessionDeepIndex != nil {
		return ErrService
	}
	service.sessionDeepIndex = command
	return nil
}

func (service *Service) bindRuntimeControls(command runtimeControlCommand) error {
	if service == nil || command == nil {
		return ErrService
	}
	service.runtimeMu.Lock()
	defer service.runtimeMu.Unlock()
	if service.runtimeControls != nil {
		return ErrService
	}
	service.runtimeControls = command
	return nil
}

func (service *Service) bindQuotaRefresh(command quotaRefreshCommand) error {
	if service == nil || command == nil {
		return ErrService
	}
	service.quotaMu.Lock()
	defer service.quotaMu.Unlock()
	if service.quotaRefresh != nil {
		return ErrService
	}
	service.quotaRefresh = command
	return nil
}

func (service *Service) bindHealthProjection(query healthProjectionQuery) error {
	if service == nil || query == nil {
		return ErrService
	}
	service.healthMu.Lock()
	defer service.healthMu.Unlock()
	if service.healthProjection != nil {
		return ErrService
	}
	service.healthProjection = query
	return nil
}

// BindDependencies attaches runtime capabilities without widening Service's
// exported RPC-shaped method surface. Each capability can be bound once.
func BindDependencies(service *Service, config ServiceConfig) error {
	if service == nil {
		return ErrService
	}
	if config.QuotaRefresh != nil {
		if err := service.bindQuotaRefresh(config.QuotaRefresh); err != nil {
			return err
		}
	}
	if config.RuntimeControls != nil {
		if err := service.bindRuntimeControls(config.RuntimeControls); err != nil {
			return err
		}
	}
	if config.SessionDeepIndex != nil {
		if err := service.bindSessionDeepIndex(config.SessionDeepIndex); err != nil {
			return err
		}
	}
	if config.HealthProjection != nil {
		if err := service.bindHealthProjection(config.HealthProjection); err != nil {
			return err
		}
	}
	return nil
}

type MethodKind string

const (
	MethodQuery   MethodKind = "query"
	MethodCommand MethodKind = "command"
)

type MethodInfo struct {
	Name string     `json:"name"`
	Kind MethodKind `json:"kind"`
}

type ContractInfo struct {
	Version            string                  `json:"version"`
	QueryVersion       string                  `json:"queryVersion"`
	UsageCostVersion   string                  `json:"usageCostVersion"`
	RuntimeInfoVersion string                  `json:"runtimeInfoVersion"`
	Methods            []MethodInfo            `json:"methods"`
	CommandMethods     []string                `json:"commandMethods"`
	ErrorExample       basequery.ErrorEnvelope `json:"errorExample"`
}

var methodAllowlist = []MethodInfo{
	{Name: "Contracts", Kind: MethodQuery},
	{Name: "UsageCost", Kind: MethodQuery},
	{Name: "ListSessions", Kind: MethodQuery},
	{Name: "SessionDetail", Kind: MethodQuery},
	{Name: "ListProjects", Kind: MethodQuery},
	{Name: "ProjectDetail", Kind: MethodQuery},
	{Name: "QuotaCurrent", Kind: MethodQuery},
	{Name: "RequestQuotaRefresh", Kind: MethodCommand},
	{Name: "UpdateSettings", Kind: MethodCommand},
	{Name: "PlanHomeSwitch", Kind: MethodCommand},
	{Name: "ConfirmHomeSwitch", Kind: MethodCommand},
	{Name: "RecoverHomeSwitch", Kind: MethodCommand},
	{Name: "RunRuntimeAction", Kind: MethodCommand},
	{Name: "AnalyzeSessionIndexRepair", Kind: MethodCommand},
	{Name: "ListSources", Kind: MethodQuery},
	{Name: "Source", Kind: MethodQuery},
	{Name: "ListJobs", Kind: MethodQuery},
	{Name: "Job", Kind: MethodQuery},
	{Name: "ListHealth", Kind: MethodQuery},
	{Name: "Health", Kind: MethodQuery},
	{Name: "HealthProjection", Kind: MethodQuery},
	{Name: "DataHealth", Kind: MethodQuery},
	{Name: "Settings", Kind: MethodQuery},
}

func (service *Service) Contracts() ContractInfo {
	return serviceQueryValue(service, func() ContractInfo {
		errorExample, _ := basequery.ErrorEnvelopeFrom(ErrService)
		return ContractInfo{
			Version: ContractVersion, QueryVersion: basequery.ContractVersion,
			UsageCostVersion: usagecost.ContractVersion, RuntimeInfoVersion: runtimeinfo.ContractVersion,
			Methods: append([]MethodInfo(nil), methodAllowlist...),
			CommandMethods: []string{
				"RequestQuotaRefresh", "UpdateSettings", "PlanHomeSwitch", "ConfirmHomeSwitch",
				"RecoverHomeSwitch", "RunRuntimeAction", "AnalyzeSessionIndexRepair",
			}, ErrorExample: errorExample,
		}
	})
}

type QuotaRefreshReceipt struct {
	Source         quotaonline.RefreshSource `json:"source"`
	NextDueAtMS    *int64                    `json:"nextDueAtMs"`
	Reason         store.SourceRefreshReason `json:"reason"`
	LastManualAtMS *int64                    `json:"lastManualAtMs"`
}

func (service *Service) RequestQuotaRefresh(
	ctx context.Context,
	source quotaonline.RefreshSource,
) (QuotaRefreshReceipt, error) {
	if service == nil {
		return QuotaRefreshReceipt{}, newServiceFailure(ErrService)
	}
	service.quotaMu.RLock()
	command := service.quotaRefresh
	service.quotaMu.RUnlock()
	if command == nil {
		return QuotaRefreshReceipt{}, newServiceFailure(ErrService)
	}
	if source != quotaonline.RefreshSourceQuota && source != quotaonline.RefreshSourceResetCredits {
		return QuotaRefreshReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("source", nil),
		)
	}
	return serviceCall(func() (QuotaRefreshReceipt, error) {
		schedule, err := command.RequestQuotaRefresh(ctx, source)
		if err != nil {
			return QuotaRefreshReceipt{}, err
		}
		return QuotaRefreshReceipt{
			Source: source, NextDueAtMS: cloneInt64(schedule.NextDueAtMS),
			Reason: schedule.Reason, LastManualAtMS: cloneInt64(schedule.LastManualAtMS),
		}, nil
	})
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (service *Service) UsageCost(
	ctx context.Context,
	request usagecost.UsageCostRequest,
) (usagecost.UsageCostResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.UsageCostResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (usagecost.UsageCostResponse, error) {
		return service.usageCost.UsageCost(ctx, request)
	})
}

func (service *Service) ListSessions(
	ctx context.Context,
	request basequery.Request,
) (usagecost.SessionListResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.SessionListResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (usagecost.SessionListResponse, error) {
		return service.usageCost.ListSessions(ctx, request)
	})
}

func (service *Service) SessionDetail(
	ctx context.Context,
	request usagecost.SessionDetailRequest,
) (usagecost.SessionDetailResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.SessionDetailResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (usagecost.SessionDetailResponse, error) {
		response, err := service.usageCost.SessionDetail(ctx, request)
		if err != nil || len(response.Turns) != 0 {
			return response, err
		}
		service.deepMu.RLock()
		command := service.sessionDeepIndex
		service.deepMu.RUnlock()
		if command == nil {
			return response, nil
		}
		if _, deepErr := command.DeepIndexSession(ctx, request.SessionID); deepErr != nil {
			if errors.Is(deepErr, context.Canceled) || errors.Is(deepErr, context.DeadlineExceeded) {
				return usagecost.SessionDetailResponse{}, deepErr
			}
			return response, nil
		}
		return service.usageCost.SessionDetail(ctx, request)
	})
}

func (service *Service) ListProjects(
	ctx context.Context,
	request basequery.Request,
) (usagecost.ProjectListResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.ProjectListResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (usagecost.ProjectListResponse, error) {
		return service.usageCost.ListProjects(ctx, request)
	})
}

func (service *Service) ProjectDetail(
	ctx context.Context,
	request usagecost.ProjectDetailRequest,
) (usagecost.ProjectDetailResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.ProjectDetailResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (usagecost.ProjectDetailResponse, error) {
		return service.usageCost.ProjectDetail(ctx, request)
	})
}

func (service *Service) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.QuotaCurrentResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.QuotaCurrentResponse, error) {
		return service.runtimeInfo.QuotaCurrent(ctx, evaluatedAtMS)
	})
}

func (service *Service) ListSources(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.SourceListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SourceListResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.SourceListResponse, error) {
		return service.runtimeInfo.ListSources(ctx, request)
	})
}

func (service *Service) Source(
	ctx context.Context,
	request runtimeinfo.SourceDetailRequest,
) (runtimeinfo.SourceDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SourceDetailResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.SourceDetailResponse, error) {
		return service.runtimeInfo.Source(ctx, request)
	})
}

func (service *Service) ListJobs(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.JobListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.JobListResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.JobListResponse, error) {
		return service.runtimeInfo.ListJobs(ctx, request)
	})
}

func (service *Service) Job(
	ctx context.Context,
	request runtimeinfo.JobDetailRequest,
) (runtimeinfo.JobDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.JobDetailResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.JobDetailResponse, error) {
		return service.runtimeInfo.Job(ctx, request)
	})
}

func (service *Service) ListHealth(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.HealthListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.HealthListResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.HealthListResponse, error) {
		return service.runtimeInfo.ListHealth(ctx, request)
	})
}

func (service *Service) Health(
	ctx context.Context,
	request runtimeinfo.HealthDetailRequest,
) (runtimeinfo.HealthDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.HealthDetailResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.HealthDetailResponse, error) {
		return service.runtimeInfo.Health(ctx, request)
	})
}

func (service *Service) HealthProjection(ctx context.Context) (HealthProjectionResponse, error) {
	if service == nil {
		return HealthProjectionResponse{}, newServiceFailure(ErrService)
	}
	service.healthMu.RLock()
	query := service.healthProjection
	service.healthMu.RUnlock()
	if query == nil {
		return HealthProjectionResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (HealthProjectionResponse, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return HealthProjectionResponse{}, err
		}
		return mapHealthProjection(query.Projection())
	})
}

func (service *Service) DataHealth(ctx context.Context, evaluatedAtMS int64) (runtimeinfo.DataHealthResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.DataHealthResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.DataHealthResponse, error) {
		return service.runtimeInfo.DataHealth(ctx, evaluatedAtMS)
	})
}

func (service *Service) Settings(ctx context.Context) (runtimeinfo.SettingsResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SettingsResponse{}, newServiceFailure(ErrService)
	}
	return serviceQueryCall(service, func() (runtimeinfo.SettingsResponse, error) {
		return service.runtimeInfo.Settings(ctx)
	})
}

func serviceCall[T any](call func() (T, error)) (value T, returnErr error) {
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
			returnErr = newServiceFailure(ErrService)
		}
	}()
	value, returnErr = call()
	if returnErr != nil {
		returnErr = newServiceFailure(returnErr)
	}
	return value, returnErr
}

func serviceQueryCall[T any](service *Service, call func() (T, error)) (T, error) {
	startedAt := time.Now()
	value, err := serviceCall(call)
	observeServiceQuery(service, time.Since(startedAt))
	return value, err
}

func serviceQueryValue[T any](service *Service, call func() T) T {
	startedAt := time.Now()
	value := call()
	observeServiceQuery(service, time.Since(startedAt))
	return value
}

func observeServiceQuery(service *Service, duration time.Duration) {
	if service == nil || service.queryObserver == nil {
		return
	}
	defer func() { _ = recover() }()
	service.queryObserver.Observe(duration)
}
