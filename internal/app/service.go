package app

import (
	"context"
	"errors"
	"runtime"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
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

type ServiceConfig struct {
	UsageCost   usageCostBindingQuery
	RuntimeInfo runtimeInfoBindingQuery
}

// Service is the only business service registered with Wails. Its unexported
// dependencies keep Store, Preferences, filesystem and credential primitives
// outside the generated frontend surface.
type Service struct {
	usageCost   usageCostBindingQuery
	runtimeInfo runtimeInfoBindingQuery
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.UsageCost == nil || config.RuntimeInfo == nil {
		return nil, ErrBindingService
	}
	return &Service{usageCost: config.UsageCost, runtimeInfo: config.RuntimeInfo}, nil
}

// BootstrapInfo contains the non-sensitive metadata needed to render the
// application shell.
type BootstrapInfo struct {
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Platform string `json:"platform"`
}

func (*Service) Bootstrap() BootstrapInfo {
	return BootstrapInfo{Name: appName, Locale: defaultLocale, Platform: runtime.GOOS}
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
	{Name: "ListSources", Kind: BindingMethodQuery},
	{Name: "Source", Kind: BindingMethodQuery},
	{Name: "ListJobs", Kind: BindingMethodQuery},
	{Name: "Job", Kind: BindingMethodQuery},
	{Name: "ListHealth", Kind: BindingMethodQuery},
	{Name: "Health", Kind: BindingMethodQuery},
	{Name: "Settings", Kind: BindingMethodQuery},
}

func (*Service) Contracts() BindingContractInfo {
	errorExample, _ := basequery.ErrorEnvelopeFrom(ErrBindingService)
	return BindingContractInfo{
		Version: BindingContractVersion, QueryVersion: basequery.ContractVersion,
		UsageCostVersion: usagecost.ContractVersion, RuntimeInfoVersion: runtimeinfo.ContractVersion,
		Methods:        append([]BindingMethodInfo(nil), bindingMethodAllowlist...),
		CommandMethods: make([]string, 0), ErrorExample: errorExample,
	}
}

func (service *Service) UsageCost(
	ctx context.Context,
	request usagecost.UsageCostRequest,
) (usagecost.UsageCostResponse, error) {
	if service == nil || service.usageCost == nil {
		return usagecost.UsageCostResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingCall(func() (usagecost.UsageCostResponse, error) {
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
	return bindingCall(func() (usagecost.SessionListResponse, error) {
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
	return bindingCall(func() (usagecost.SessionDetailResponse, error) {
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
	return bindingCall(func() (usagecost.ProjectListResponse, error) {
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
	return bindingCall(func() (usagecost.ProjectDetailResponse, error) {
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
	return bindingCall(func() (runtimeinfo.QuotaCurrentResponse, error) {
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
	return bindingCall(func() (runtimeinfo.SourceListResponse, error) {
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
	return bindingCall(func() (runtimeinfo.SourceDetailResponse, error) {
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
	return bindingCall(func() (runtimeinfo.JobListResponse, error) {
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
	return bindingCall(func() (runtimeinfo.JobDetailResponse, error) {
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
	return bindingCall(func() (runtimeinfo.HealthListResponse, error) {
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
	return bindingCall(func() (runtimeinfo.HealthDetailResponse, error) {
		return service.runtimeInfo.Health(ctx, request)
	})
}

func (service *Service) Settings(ctx context.Context) (runtimeinfo.SettingsResponse, error) {
	if service == nil || service.runtimeInfo == nil {
		return runtimeinfo.SettingsResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingCall(func() (runtimeinfo.SettingsResponse, error) {
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
