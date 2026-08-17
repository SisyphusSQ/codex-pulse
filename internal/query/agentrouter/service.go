package agentrouter

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

var ErrInvalidService = errors.New("agent provider query router is invalid")

type UsageService interface {
	UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error)
	ListSessions(context.Context, basequery.Request) (usagecost.SessionListResponse, error)
	SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error)
	ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error)
	ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error)
}

type InvocationService interface {
	InvocationUsage(context.Context, invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error)
}

type QuotaService interface {
	QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error)
	QuotaPace(context.Context, int64) (runtimeinfo.QuotaPaceResponse, error)
}

type QuotaRouter struct {
	codex  QuotaService
	cursor QuotaService
}

func NewQuota(codex QuotaService, cursor QuotaService) (*QuotaRouter, error) {
	if codex == nil || cursor == nil {
		return nil, ErrInvalidService
	}
	return &QuotaRouter{codex: codex, cursor: cursor}, nil
}

func (service *QuotaRouter) QuotaCurrent(
	ctx context.Context,
	scope agentprovider.Scope,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	provider, err := normalized(scope)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	backend := service.codex
	if provider == agentprovider.Cursor {
		backend = service.cursor
	}
	response, err := backend.QuotaCurrent(ctx, evaluatedAtMS)
	response.ProviderContext = quotaProviderContext(provider, response.ProviderContext)
	return response, err
}

func (service *QuotaRouter) QuotaPace(
	ctx context.Context,
	scope agentprovider.Scope,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaPaceResponse, error) {
	provider, err := normalized(scope)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	backend := service.codex
	if provider == agentprovider.Cursor {
		backend = service.cursor
	}
	response, err := backend.QuotaPace(ctx, evaluatedAtMS)
	response.ProviderContext = quotaProviderContext(provider, response.ProviderContext)
	return response, err
}

func quotaProviderContext(provider string, responseContext agentprovider.Context) agentprovider.Context {
	if provider == agentprovider.Codex {
		return agentprovider.CodexContext()
	}
	responseContext.EffectiveProvider = provider
	return responseContext
}

type Service struct {
	codexUsage       UsageService
	codexInvocation  InvocationService
	cursorUsage      UsageService
	cursorInvocation InvocationService
}

func New(
	codexUsage UsageService,
	codexInvocation InvocationService,
	cursorUsage UsageService,
	cursorInvocation InvocationService,
) (*Service, error) {
	if codexUsage == nil || codexInvocation == nil || cursorUsage == nil || cursorInvocation == nil {
		return nil, ErrInvalidService
	}
	return &Service{
		codexUsage: codexUsage, codexInvocation: codexInvocation,
		cursorUsage: cursorUsage, cursorInvocation: cursorInvocation,
	}, nil
}

func (service *Service) UsageCost(ctx context.Context, request usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorUsage.UsageCost(ctx, request)
	}
	response, err := service.codexUsage.UsageCost(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func (service *Service) ListSessions(ctx context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorUsage.ListSessions(ctx, request)
	}
	response, err := service.codexUsage.ListSessions(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func (service *Service) SessionDetail(ctx context.Context, request usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorUsage.SessionDetail(ctx, request)
	}
	response, err := service.codexUsage.SessionDetail(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func (service *Service) ListProjects(ctx context.Context, request basequery.Request) (usagecost.ProjectListResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorUsage.ListProjects(ctx, request)
	}
	response, err := service.codexUsage.ListProjects(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func (service *Service) ProjectDetail(ctx context.Context, request usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorUsage.ProjectDetail(ctx, request)
	}
	response, err := service.codexUsage.ProjectDetail(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func (service *Service) InvocationUsage(ctx context.Context, request invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return invocationusage.InvocationUsageResponse{}, err
	}
	request.Provider.Provider = provider
	if provider == agentprovider.Cursor {
		return service.cursorInvocation.InvocationUsage(ctx, request)
	}
	response, err := service.codexInvocation.InvocationUsage(ctx, request)
	response.ProviderContext = agentprovider.CodexContext()
	return response, err
}

func normalized(scope agentprovider.Scope) (string, error) {
	provider, err := agentprovider.Normalize(scope.Provider)
	if err != nil {
		return "", basequery.NewValidationFailure("provider", err)
	}
	return provider, nil
}
