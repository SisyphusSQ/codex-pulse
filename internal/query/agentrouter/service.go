package agentrouter

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
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

type ProviderQuotaService interface {
	QuotaService
	RefreshQuota(context.Context) error
}

type QuotaRouter struct {
	codex  QuotaService
	cursor ProviderQuotaService
	grok   ProviderQuotaService
	states providercontrol.StateReader
}

func NewQuota(codex QuotaService, cursor ProviderQuotaService, grok ProviderQuotaService) (*QuotaRouter, error) {
	if codex == nil || cursor == nil || grok == nil {
		return nil, ErrInvalidService
	}
	return &QuotaRouter{codex: codex, cursor: cursor, grok: grok}, nil
}

func (service *QuotaRouter) RefreshQuota(
	ctx context.Context,
	scope agentprovider.Scope,
) (agentprovider.Context, error) {
	provider, err := normalized(scope)
	if err != nil {
		return agentprovider.Context{}, err
	}
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return agentprovider.Context{}, err
	}
	defer finish()
	var backend ProviderQuotaService
	switch provider {
	case agentprovider.Cursor:
		backend = service.cursor
	case agentprovider.Grok:
		backend = service.grok
	default:
		return agentprovider.Context{}, basequery.NewValidationFailure("provider", agentprovider.ErrInvalidProvider)
	}
	if err := backend.RefreshQuota(operationContext); err != nil {
		return agentprovider.Context{}, err
	}
	return quotaProviderContext(provider, agentprovider.Context{}), nil
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
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	defer finish()
	backend, err := service.quotaBackend(provider)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	response, err := backend.QuotaCurrent(operationContext, evaluatedAtMS)
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
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	defer finish()
	backend, err := service.quotaBackend(provider)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	response, err := backend.QuotaPace(operationContext, evaluatedAtMS)
	response.ProviderContext = quotaProviderContext(provider, response.ProviderContext)
	return response, err
}

func (service *QuotaRouter) quotaBackend(provider string) (QuotaService, error) {
	switch provider {
	case agentprovider.Cursor:
		return service.cursor, nil
	case agentprovider.Grok:
		return service.grok, nil
	case agentprovider.Codex:
		return service.codex, nil
	default:
		return nil, basequery.NewValidationFailure("provider", agentprovider.ErrInvalidProvider)
	}
}

func quotaProviderContext(provider string, responseContext agentprovider.Context) agentprovider.Context {
	switch provider {
	case agentprovider.Codex:
		return agentprovider.CodexContext()
	case agentprovider.Grok:
		if responseContext.EffectiveProvider == "" {
			return agentprovider.GrokContext()
		}
		responseContext.EffectiveProvider = provider
		return responseContext
	default:
		responseContext.EffectiveProvider = provider
		return responseContext
	}
}

type Service struct {
	codexUsage       UsageService
	codexInvocation  InvocationService
	cursorUsage      UsageService
	cursorInvocation InvocationService
	grokUsage        UsageService
	grokInvocation   InvocationService
	states           providercontrol.StateReader
}

func New(
	codexUsage UsageService,
	codexInvocation InvocationService,
	cursorUsage UsageService,
	cursorInvocation InvocationService,
	grokUsage UsageService,
	grokInvocation InvocationService,
) (*Service, error) {
	if codexUsage == nil || codexInvocation == nil || cursorUsage == nil || cursorInvocation == nil ||
		grokUsage == nil || grokInvocation == nil {
		return nil, ErrInvalidService
	}
	return &Service{
		codexUsage: codexUsage, codexInvocation: codexInvocation,
		cursorUsage: cursorUsage, cursorInvocation: cursorInvocation,
		grokUsage: grokUsage, grokInvocation: grokInvocation,
	}, nil
}

func (service *Service) BindStates(reader providercontrol.StateReader) {
	if service == nil {
		return
	}
	service.states = reader
}

func (router *QuotaRouter) BindStates(reader providercontrol.StateReader) {
	if router == nil {
		return
	}
	router.states = reader
}

func admitProvider(reader providercontrol.StateReader, provider string) error {
	if reader == nil {
		return nil
	}
	snapshot, err := reader.ProviderState(provider)
	if err != nil {
		if errors.Is(err, providercontrol.ErrInvalidProvider) {
			return basequery.NewValidationFailure("provider", err)
		}
		return basequery.NewUnavailableFailure(err)
	}
	switch {
	case errors.Is(providercontrol.QueryFailure(snapshot), providercontrol.ErrDisabled):
		return basequery.NewProviderDisabledFailure(nil)
	case errors.Is(providercontrol.QueryFailure(snapshot), providercontrol.ErrUnavailable):
		return basequery.NewUnavailableFailure(nil)
	default:
		return nil
	}
}

func beginProviderOperation(
	ctx context.Context,
	reader providercontrol.StateReader,
	provider string,
) (context.Context, func(), error) {
	if beginner, ok := reader.(providercontrol.Beginner); ok {
		operation, err := beginner.Begin(ctx, provider)
		if err != nil {
			return nil, nil, providerOperationFailure(err)
		}
		return operation.Context(), operation.Finish, nil
	}
	if err := admitProvider(reader, provider); err != nil {
		return nil, nil, err
	}
	return ctx, func() {}, nil
}

func providerOperationFailure(err error) error {
	switch {
	case errors.Is(err, providercontrol.ErrInvalidProvider):
		return basequery.NewValidationFailure("provider", err)
	case errors.Is(err, providercontrol.ErrDisabled):
		return basequery.NewProviderDisabledFailure(nil)
	case errors.Is(err, providercontrol.ErrUnavailable), errors.Is(err, providercontrol.ErrSealed):
		return basequery.NewUnavailableFailure(err)
	default:
		return err
	}
}

func (service *Service) UsageCost(ctx context.Context, request usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	defer finish()
	backend, err := service.usageBackend(provider)
	if err != nil {
		return usagecost.UsageCostResponse{}, err
	}
	response, err := backend.UsageCost(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) ListSessions(ctx context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	defer finish()
	backend, err := service.usageBackend(provider)
	if err != nil {
		return usagecost.SessionListResponse{}, err
	}
	response, err := backend.ListSessions(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) SessionDetail(ctx context.Context, request usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	defer finish()
	backend, err := service.usageBackend(provider)
	if err != nil {
		return usagecost.SessionDetailResponse{}, err
	}
	response, err := backend.SessionDetail(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) ListProjects(ctx context.Context, request basequery.Request) (usagecost.ProjectListResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	defer finish()
	backend, err := service.usageBackend(provider)
	if err != nil {
		return usagecost.ProjectListResponse{}, err
	}
	response, err := backend.ListProjects(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) ProjectDetail(ctx context.Context, request usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	defer finish()
	backend, err := service.usageBackend(provider)
	if err != nil {
		return usagecost.ProjectDetailResponse{}, err
	}
	response, err := backend.ProjectDetail(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) InvocationUsage(ctx context.Context, request invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error) {
	provider, err := normalized(request.Provider)
	if err != nil {
		return invocationusage.InvocationUsageResponse{}, err
	}
	request.Provider.Provider = provider
	operationContext, finish, err := beginProviderOperation(ctx, service.states, provider)
	if err != nil {
		return invocationusage.InvocationUsageResponse{}, err
	}
	defer finish()
	backend, err := service.invocationBackend(provider)
	if err != nil {
		return invocationusage.InvocationUsageResponse{}, err
	}
	response, err := backend.InvocationUsage(operationContext, request)
	if provider == agentprovider.Codex {
		response.ProviderContext = agentprovider.CodexContext()
	}
	return response, err
}

func (service *Service) usageBackend(provider string) (UsageService, error) {
	switch provider {
	case agentprovider.Cursor:
		return service.cursorUsage, nil
	case agentprovider.Grok:
		return service.grokUsage, nil
	case agentprovider.Codex:
		return service.codexUsage, nil
	default:
		return nil, basequery.NewValidationFailure("provider", agentprovider.ErrInvalidProvider)
	}
}

func (service *Service) invocationBackend(provider string) (InvocationService, error) {
	switch provider {
	case agentprovider.Cursor:
		return service.cursorInvocation, nil
	case agentprovider.Grok:
		return service.grokInvocation, nil
	case agentprovider.Codex:
		return service.codexInvocation, nil
	default:
		return nil, basequery.NewValidationFailure("provider", agentprovider.ErrInvalidProvider)
	}
}

func normalized(scope agentprovider.Scope) (string, error) {
	provider, err := agentprovider.Normalize(scope.Provider)
	if err != nil {
		return "", basequery.NewValidationFailure("provider", err)
	}
	return provider, nil
}
