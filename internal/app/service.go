package app

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	appName                = "Codex Pulse"
	defaultLocale          = "zh-CN"
	BindingContractVersion = "wails-bindings-v1"
)

var (
	ErrBindingService = errors.New("binding service is unavailable")
	ErrBindingQuery   = errors.New("binding query failed")
)

type usageCostBindingQuery interface {
	UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error)
	ListSessions(context.Context, basequery.Request) (usagecost.SessionListResponse, error)
	SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error)
	ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error)
	ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error)
}

type runtimeInfoBindingQuery interface {
	QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error)
	ListSources(context.Context, basequery.Request) (runtimeinfo.SourceListResponse, error)
	Source(context.Context, runtimeinfo.SourceDetailRequest) (runtimeinfo.SourceDetailResponse, error)
	ListJobs(context.Context, basequery.Request) (runtimeinfo.JobListResponse, error)
	Job(context.Context, runtimeinfo.JobDetailRequest) (runtimeinfo.JobDetailResponse, error)
	ListHealth(context.Context, basequery.Request) (runtimeinfo.HealthListResponse, error)
	Health(context.Context, runtimeinfo.HealthDetailRequest) (runtimeinfo.HealthDetailResponse, error)
	Settings(context.Context) (runtimeinfo.SettingsResponse, error)
}

type quotaRefreshBindingCommand interface {
	RequestQuotaRefresh(context.Context, quotaonline.RefreshSource) (store.SourceRefreshSchedule, error)
}

type healthProjectionBindingQuery interface {
	Projection() healthmodel.Projection
}

type QueryObserver interface {
	Observe(time.Duration)
}

type ServiceConfig struct {
	UsageCost       usageCostBindingQuery
	RuntimeInfo     runtimeInfoBindingQuery
	QuotaRefresh    quotaRefreshBindingCommand
	RuntimeControls runtimeControlBindingCommand
	QueryObserver   QueryObserver
}

// Service is the only business service registered with Wails. Its unexported
// dependencies keep Store, Preferences, filesystem and credential primitives
// outside the generated frontend surface.
type Service struct {
	usageCost        usageCostBindingQuery
	runtimeInfo      runtimeInfoBindingQuery
	quotaMu          sync.RWMutex
	quotaRefresh     quotaRefreshBindingCommand
	runtimeMu        sync.RWMutex
	runtimeControls  runtimeControlBindingCommand
	healthMu         sync.RWMutex
	healthProjection healthProjectionBindingQuery
	queryObserver    QueryObserver
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.UsageCost == nil || config.RuntimeInfo == nil {
		return nil, ErrBindingService
	}
	return &Service{
		usageCost:       config.UsageCost,
		runtimeInfo:     config.RuntimeInfo,
		quotaRefresh:    config.QuotaRefresh,
		runtimeControls: config.RuntimeControls,
		queryObserver:   config.QueryObserver,
	}, nil
}

func (service *Service) bindRuntimeControls(command runtimeControlBindingCommand) error {
	if service == nil || command == nil {
		return ErrBindingService
	}
	service.runtimeMu.Lock()
	defer service.runtimeMu.Unlock()
	if service.runtimeControls != nil {
		return ErrBindingService
	}
	service.runtimeControls = command
	return nil
}

func (service *Service) bindQuotaRefresh(command quotaRefreshBindingCommand) error {
	if service == nil || command == nil {
		return ErrBindingService
	}
	service.quotaMu.Lock()
	defer service.quotaMu.Unlock()
	if service.quotaRefresh != nil {
		return ErrBindingService
	}
	service.quotaRefresh = command
	return nil
}

func (service *Service) bindHealthProjection(query healthProjectionBindingQuery) error {
	if service == nil || query == nil {
		return ErrBindingService
	}
	service.healthMu.Lock()
	defer service.healthMu.Unlock()
	if service.healthProjection != nil {
		return ErrBindingService
	}
	service.healthProjection = query
	return nil
}

// BootstrapInfo contains the non-sensitive metadata needed to render the
// application shell.
type BootstrapInfo struct {
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Platform string `json:"platform"`
}

func (service *Service) Bootstrap() BootstrapInfo {
	return bindingQueryValue(service, func() BootstrapInfo {
		return BootstrapInfo{Name: appName, Locale: defaultLocale, Platform: runtime.GOOS}
	})
}

type BindingMethodKind string

const (
	BindingMethodQuery   BindingMethodKind = "query"
	BindingMethodCommand BindingMethodKind = "command"
)

type BindingMethodInfo struct {
	Name string            `json:"name"`
	Kind BindingMethodKind `json:"kind"`
}

type BindingContractInfo struct {
	Version            string                  `json:"version"`
	QueryVersion       string                  `json:"queryVersion"`
	UsageCostVersion   string                  `json:"usageCostVersion"`
	RuntimeInfoVersion string                  `json:"runtimeInfoVersion"`
	Methods            []BindingMethodInfo     `json:"methods"`
	CommandMethods     []string                `json:"commandMethods"`
	ErrorExample       basequery.ErrorEnvelope `json:"errorExample"`
}

var bindingMethodAllowlist = []BindingMethodInfo{
	{Name: "Bootstrap", Kind: BindingMethodQuery},
	{Name: "Contracts", Kind: BindingMethodQuery},
	{Name: "UsageCost", Kind: BindingMethodQuery},
	{Name: "ListSessions", Kind: BindingMethodQuery},
	{Name: "SessionDetail", Kind: BindingMethodQuery},
	{Name: "ListProjects", Kind: BindingMethodQuery},
	{Name: "ProjectDetail", Kind: BindingMethodQuery},
	{Name: "QuotaCurrent", Kind: BindingMethodQuery},
	{Name: "RequestQuotaRefresh", Kind: BindingMethodCommand},
	{Name: "UpdateSettings", Kind: BindingMethodCommand},
	{Name: "PlanHomeSwitch", Kind: BindingMethodCommand},
	{Name: "ConfirmHomeSwitch", Kind: BindingMethodCommand},
	{Name: "RecoverHomeSwitch", Kind: BindingMethodCommand},
	{Name: "RunRuntimeAction", Kind: BindingMethodCommand},
	{Name: "AnalyzeSessionIndexRepair", Kind: BindingMethodCommand},
	{Name: "ListSources", Kind: BindingMethodQuery},
	{Name: "Source", Kind: BindingMethodQuery},
	{Name: "ListJobs", Kind: BindingMethodQuery},
	{Name: "Job", Kind: BindingMethodQuery},
	{Name: "ListHealth", Kind: BindingMethodQuery},
	{Name: "Health", Kind: BindingMethodQuery},
	{Name: "HealthProjection", Kind: BindingMethodQuery},
	{Name: "Settings", Kind: BindingMethodQuery},
}

func (service *Service) Contracts() BindingContractInfo {
	return bindingQueryValue(service, func() BindingContractInfo {
		errorExample, _ := basequery.ErrorEnvelopeFrom(ErrBindingService)
		return BindingContractInfo{
			Version: BindingContractVersion, QueryVersion: basequery.ContractVersion,
			UsageCostVersion: usagecost.ContractVersion, RuntimeInfoVersion: runtimeinfo.ContractVersion,
			Methods: append([]BindingMethodInfo(nil), bindingMethodAllowlist...),
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
		return QuotaRefreshReceipt{}, newBindingFailure(ErrBindingService)
	}
	service.quotaMu.RLock()
	command := service.quotaRefresh
	service.quotaMu.RUnlock()
	if command == nil {
		return QuotaRefreshReceipt{}, newBindingFailure(ErrBindingService)
	}
	if source != quotaonline.RefreshSourceQuota && source != quotaonline.RefreshSourceResetCredits {
		return QuotaRefreshReceipt{}, newBindingFailure(
			basequery.NewValidationFailure("source", nil),
		)
	}
	return bindingCall(func() (QuotaRefreshReceipt, error) {
		schedule, err := command.RequestQuotaRefresh(ctx, source)
		if err != nil {
			return QuotaRefreshReceipt{}, err
		}
		return QuotaRefreshReceipt{
			Source: source, NextDueAtMS: cloneBindingInt64(schedule.NextDueAtMS),
			Reason: schedule.Reason, LastManualAtMS: cloneBindingInt64(schedule.LastManualAtMS),
		}, nil
	})
}

func cloneBindingInt64(value *int64) *int64 {
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
		return usagecost.UsageCostResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (usagecost.UsageCostResponse, error) {
		return service.usageCost.UsageCost(ctx, request)
	})
}

func (service *Service) ListSessions(
	ctx context.Context,
	request basequery.Request,
) (usagecost.SessionListResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.SessionListResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (usagecost.SessionListResponse, error) {
		return service.usageCost.ListSessions(ctx, request)
	})
}

func (service *Service) SessionDetail(
	ctx context.Context,
	request usagecost.SessionDetailRequest,
) (usagecost.SessionDetailResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.SessionDetailResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (usagecost.SessionDetailResponse, error) {
		return service.usageCost.SessionDetail(ctx, request)
	})
}

func (service *Service) ListProjects(
	ctx context.Context,
	request basequery.Request,
) (usagecost.ProjectListResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.ProjectListResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (usagecost.ProjectListResponse, error) {
		return service.usageCost.ListProjects(ctx, request)
	})
}

func (service *Service) ProjectDetail(
	ctx context.Context,
	request usagecost.ProjectDetailRequest,
) (usagecost.ProjectDetailResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.ProjectDetailResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (usagecost.ProjectDetailResponse, error) {
		return service.usageCost.ProjectDetail(ctx, request)
	})
}

func (service *Service) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.QuotaCurrentResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.QuotaCurrentResponse, error) {
		return service.runtimeInfo.QuotaCurrent(ctx, evaluatedAtMS)
	})
}

func (service *Service) ListSources(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.SourceListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SourceListResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.SourceListResponse, error) {
		return service.runtimeInfo.ListSources(ctx, request)
	})
}

func (service *Service) Source(
	ctx context.Context,
	request runtimeinfo.SourceDetailRequest,
) (runtimeinfo.SourceDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SourceDetailResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.SourceDetailResponse, error) {
		return service.runtimeInfo.Source(ctx, request)
	})
}

func (service *Service) ListJobs(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.JobListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.JobListResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.JobListResponse, error) {
		return service.runtimeInfo.ListJobs(ctx, request)
	})
}

func (service *Service) Job(
	ctx context.Context,
	request runtimeinfo.JobDetailRequest,
) (runtimeinfo.JobDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.JobDetailResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.JobDetailResponse, error) {
		return service.runtimeInfo.Job(ctx, request)
	})
}

func (service *Service) ListHealth(
	ctx context.Context,
	request basequery.Request,
) (runtimeinfo.HealthListResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.HealthListResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.HealthListResponse, error) {
		return service.runtimeInfo.ListHealth(ctx, request)
	})
}

func (service *Service) Health(
	ctx context.Context,
	request runtimeinfo.HealthDetailRequest,
) (runtimeinfo.HealthDetailResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.HealthDetailResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.HealthDetailResponse, error) {
		return service.runtimeInfo.Health(ctx, request)
	})
}

func (service *Service) HealthProjection(ctx context.Context) (HealthProjectionResponse, error) {
	if service == nil {
		return HealthProjectionResponse{}, newBindingFailure(ErrBindingService)
	}
	service.healthMu.RLock()
	query := service.healthProjection
	service.healthMu.RUnlock()
	if query == nil {
		return HealthProjectionResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (HealthProjectionResponse, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return HealthProjectionResponse{}, err
		}
		return mapHealthProjection(query.Projection())
	})
}

func (service *Service) Settings(ctx context.Context) (runtimeinfo.SettingsResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SettingsResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingQueryCall(service, func() (runtimeinfo.SettingsResponse, error) {
		return service.runtimeInfo.Settings(ctx)
	})
}

func bindingCall[T any](call func() (T, error)) (value T, returnErr error) {
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
			returnErr = newBindingFailure(ErrBindingService)
		}
	}()
	value, returnErr = call()
	if returnErr != nil {
		returnErr = newBindingFailure(returnErr)
	}
	return value, returnErr
}

func bindingQueryCall[T any](service *Service, call func() (T, error)) (T, error) {
	startedAt := time.Now()
	value, err := bindingCall(call)
	observeBindingQuery(service, time.Since(startedAt))
	return value, err
}

func bindingQueryValue[T any](service *Service, call func() T) T {
	startedAt := time.Now()
	value := call()
	observeBindingQuery(service, time.Since(startedAt))
	return value
}

func observeBindingQuery(service *Service, duration time.Duration) {
	if service == nil || service.queryObserver == nil {
		return
	}
	defer func() { _ = recover() }()
	service.queryObserver.Observe(duration)
}
