package core

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/pricingcatalog"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 Service 只暴露 Go Helper 的业务方法，不继续携带 updater 平台职责。
func TestServiceExposesExactBusinessSurface(t *testing.T) {
	typeOfService := reflect.TypeFor[*Service]()
	got := make([]string, 0, typeOfService.NumMethod())
	for i := range typeOfService.NumMethod() {
		got = append(got, typeOfService.Method(i).Name)
	}
	sort.Strings(got)
	want := []string{
		"APICredentialStatus", "APISubscriptionsCurrent", "AccountSnapshot", "AnalyzeSessionIndexRepair", "ConfirmHomeSwitch", "Contracts", "DataHealth", "Health",
		"HealthProjection", "InvocationUsage", "Job", "ListHealth", "ListJobs", "ListProjects", "ListSessions", "ListSources",
		"PlanHomeSwitch", "PricingCatalogCurrent", "ProjectDetail", "QuotaCurrent", "QuotaPace", "RecoverHomeSwitch", "RequestProviderRefresh", "RequestQuotaRefresh",
		"RunRuntimeAction", "SessionDetail", "Settings", "Source", "UpdateAPICredential", "UpdateSettings", "UsageCost",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Service methods = %v, want %v", got, want)
	}
}

func TestServiceDelegatesAPICredentialStatusAndMutationsWithoutReturningSecrets(t *testing.T) {
	t.Parallel()

	credentials := &apiCredentialStoreStub{
		status: apisubscriptions.CredentialStatus{DeepSeekConfigured: true},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{},
		APICredentials: credentials,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.APICredentialStatus(t.Context())
	if err != nil {
		t.Fatalf("APICredentialStatus() error = %v", err)
	}
	if status != credentials.status || credentials.statusCalls != 1 {
		t.Fatalf("APICredentialStatus() = %#v, calls = %d", status, credentials.statusCalls)
	}

	status, err = service.UpdateAPICredential(t.Context(), APICredentialUpdateRequest{
		Service: apisubscriptions.ServiceOpenCodeGo,
		Secret:  []byte("new-secret"),
	})
	if err != nil {
		t.Fatalf("UpdateAPICredential(save) error = %v", err)
	}
	if credentials.savedService != apisubscriptions.ServiceOpenCodeGo ||
		string(credentials.savedSecret) != "new-secret" || credentials.deletedService != "" ||
		status != credentials.status {
		t.Fatalf("save delegation = %#v, status = %#v", credentials, status)
	}

	status, err = service.UpdateAPICredential(t.Context(), APICredentialUpdateRequest{
		Service: apisubscriptions.ServiceDeepSeek,
		Delete:  true,
	})
	if err != nil {
		t.Fatalf("UpdateAPICredential(delete) error = %v", err)
	}
	if credentials.deletedService != apisubscriptions.ServiceDeepSeek || status != credentials.status {
		t.Fatalf("delete delegation = %#v, status = %#v", credentials, status)
	}
}

func TestServiceRejectsAmbiguousAPICredentialMutations(t *testing.T) {
	t.Parallel()

	credentials := &apiCredentialStoreStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{},
		APICredentials: credentials,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]APICredentialUpdateRequest{
		"unknown service": {Service: "unknown", Secret: []byte("secret")},
		"missing action":  {Service: apisubscriptions.ServiceDeepSeek},
		"conflicting action": {
			Service: apisubscriptions.ServiceDeepSeek, Secret: []byte("secret"), Delete: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.UpdateAPICredential(t.Context(), request); err == nil {
				t.Fatal("UpdateAPICredential() accepted invalid request")
			} else if envelope, ok := basequery.ErrorEnvelopeFrom(err); !ok || envelope.Error.Code != basequery.ErrorValidation {
				t.Fatalf("UpdateAPICredential() error = %v", err)
			}
		})
	}
	if credentials.saveCalls != 0 || credentials.deleteCalls != 0 {
		t.Fatalf("invalid mutations reached credential store: %#v", credentials)
	}
}

func TestServiceDelegatesAPISubscriptionsOutsideAgentProviderRouting(t *testing.T) {
	t.Parallel()

	query := &apiSubscriptionsQueryStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{}, APISubscriptions: query,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.APISubscriptionsCurrent(t.Context(), 123)
	if err != nil {
		t.Fatalf("APISubscriptionsCurrent() error = %v", err)
	}
	if query.atMS != 123 || response.EvaluatedAtMS != 123 {
		t.Fatalf("delegated at = %d, response = %#v", query.atMS, response)
	}
}

func TestServiceRejectsInvalidAPISubscriptionsEvaluationTime(t *testing.T) {
	t.Parallel()

	query := &apiSubscriptionsQueryStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{}, APISubscriptions: query,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.APISubscriptionsCurrent(t.Context(), 0); err == nil {
		t.Fatal("APISubscriptionsCurrent() accepted a zero evaluation time")
	} else if envelope, ok := basequery.ErrorEnvelopeFrom(err); !ok || envelope.Error.Code != basequery.ErrorValidation {
		t.Fatalf("APISubscriptionsCurrent() error = %v", err)
	}
	if query.calls != 0 {
		t.Fatalf("invalid request reached query %d times", query.calls)
	}
}

func TestServiceDelegatesInvocationUsage(t *testing.T) {
	t.Parallel()

	query := &invocationUsageQueryStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: query,
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := invocationusage.InvocationUsageRequest{TopLimit: 17}
	if _, err := service.InvocationUsage(context.Background(), request); err != nil {
		t.Fatalf("InvocationUsage() error = %v", err)
	}
	if query.request.TopLimit != 17 {
		t.Fatalf("delegated request = %#v", query.request)
	}
}

func TestServiceDelegatesProviderScopedQuota(t *testing.T) {
	t.Parallel()

	quota := &agentQuotaQueryStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{}, QuotaInfo: quota,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.QuotaCurrent(
		context.Background(), agentprovider.Scope{Provider: agentprovider.Cursor}, 123,
	)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	if quota.scope.Provider != agentprovider.Cursor || quota.atMS != 123 ||
		response.ProviderContext.EffectiveProvider != agentprovider.Cursor {
		t.Fatalf("delegated quota = scope %#v, at %d, response %#v", quota.scope, quota.atMS, response)
	}
}

func TestServiceRoutesQuotaRefreshByProvider(t *testing.T) {
	t.Parallel()
	nextDueAtMS := int64(456)
	codex := &quotaRefreshCommandStub{schedule: store.SourceRefreshSchedule{
		NextDueAtMS: &nextDueAtMS, Reason: store.RefreshReasonManual,
	}}
	providers := &providerQuotaRefreshStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{},
		QuotaRefresh: codex, ProviderQuotaRefresh: providers,
	})
	if err != nil {
		t.Fatal(err)
	}

	cursorReceipt, err := service.RequestQuotaRefresh(
		context.Background(), agentprovider.Scope{Provider: agentprovider.Cursor}, quotaonline.RefreshSourceQuota,
	)
	if err != nil || cursorReceipt.ProviderContext.EffectiveProvider != agentprovider.Cursor ||
		cursorReceipt.Reason != store.RefreshReasonManual || providers.scope.Provider != agentprovider.Cursor {
		t.Fatalf("Cursor RequestQuotaRefresh() = %#v, scope %#v, %v", cursorReceipt, providers.scope, err)
	}
	if codex.calls != 0 {
		t.Fatalf("Cursor refresh reached Codex command %d times", codex.calls)
	}

	codexReceipt, err := service.RequestQuotaRefresh(
		context.Background(), agentprovider.Scope{}, quotaonline.RefreshSourceResetCredits,
	)
	if err != nil || codexReceipt.ProviderContext.EffectiveProvider != agentprovider.Codex ||
		codexReceipt.NextDueAtMS == nil || *codexReceipt.NextDueAtMS != nextDueAtMS || codex.calls != 1 {
		t.Fatalf("default RequestQuotaRefresh() = %#v, calls %d, %v", codexReceipt, codex.calls, err)
	}

	if _, err := service.RequestQuotaRefresh(
		context.Background(), agentprovider.Scope{Provider: agentprovider.Grok}, quotaonline.RefreshSourceResetCredits,
	); err == nil {
		t.Fatal("Grok reset-credits refresh was accepted")
	} else if envelope, ok := basequery.ErrorEnvelopeFrom(err); !ok || envelope.Error.Code != basequery.ErrorValidation {
		t.Fatalf("Grok reset-credits error = %v", err)
	}
	if providers.calls != 1 {
		t.Fatalf("invalid Grok request reached provider command; calls = %d", providers.calls)
	}
}

func TestServiceDelegatesEphemeralAccountSnapshot(t *testing.T) {
	email, planType := "person@example.com", "pro"
	account := &accountSnapshotQueryStub{snapshot: AccountSnapshot{
		Account: &AccountIdentity{Type: "chatgpt", Email: &email, PlanType: &planType},
	}}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:     runtimeQueryStub{},
		AccountSnapshot: account,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	got, err := service.AccountSnapshot(context.Background(), agentprovider.Scope{Provider: agentprovider.Cursor})
	if err != nil {
		t.Fatalf("AccountSnapshot() error = %v", err)
	}
	if got.Account == nil || got.Account.Type != "chatgpt" || got.Account.Email == nil ||
		*got.Account.Email != email || got.Account.PlanType == nil ||
		*got.Account.PlanType != planType || account.calls != 1 || account.scope.Provider != agentprovider.Cursor {
		t.Fatalf("AccountSnapshot() = %#v, calls = %d, scope = %#v", got, account.calls, account.scope)
	}
}

// 测试 Contracts 的命令清单与方法 kind 一致，且不会向 client 发布重复能力。
func TestServiceContractsExposeUniqueCommandMethods(t *testing.T) {
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo: runtimeQueryStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	contract := service.Contracts()
	if contract.Version != "core-rpc-v2" ||
		contract.UsageCostVersion != "usage-cost-v2" ||
		contract.InvocationUsageVersion != "invocation-usage-v1" ||
		contract.PricingCatalogVersion != "pricing-catalog-v1" {
		t.Fatalf("Contracts() versions = %#v", contract)
	}
	commandsFromMethods := make([]string, 0)
	for _, method := range contract.Methods {
		if method.Kind == MethodCommand {
			commandsFromMethods = append(commandsFromMethods, method.Name)
		}
	}
	sort.Strings(commandsFromMethods)
	commands := append([]string(nil), contract.CommandMethods...)
	sort.Strings(commands)
	if !reflect.DeepEqual(commands, commandsFromMethods) {
		t.Fatalf("CommandMethods = %v, want unique command methods %v", commands, commandsFromMethods)
	}
}

// 测试 Service 将 session 查询原样委托给现有业务 service。
func TestServiceDelegatesSessionQuery(t *testing.T) {
	usage := &usageQueryStub{sessions: usagecost.SessionListResponse{
		Meta: basequery.ResponseMeta{Version: usagecost.ContractVersion, Status: basequery.ResponsePartial},
	}}
	service, err := NewService(ServiceConfig{
		UsageCost: usage, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo: runtimeQueryStub{},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	request := basequery.Request{Page: basequery.PageRequest{Limit: 17}}
	got, err := service.ListSessions(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if usage.request.Page.Limit != 17 || got.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("delegation request = %#v, response = %#v", usage.request, got)
	}
}

type usageQueryStub struct {
	request  basequery.Request
	sessions usagecost.SessionListResponse
}

type invocationUsageQueryStub struct {
	request invocationusage.InvocationUsageRequest
}

type apiSubscriptionsQueryStub struct {
	atMS  int64
	calls int
}

type apiCredentialStoreStub struct {
	status         apisubscriptions.CredentialStatus
	statusCalls    int
	savedService   string
	savedSecret    []byte
	saveCalls      int
	deletedService string
	deleteCalls    int
}

func (stub *apiCredentialStoreStub) Status(context.Context) (apisubscriptions.CredentialStatus, error) {
	stub.statusCalls++
	return stub.status, nil
}

func (stub *apiCredentialStoreStub) Save(_ context.Context, service string, secret []byte) error {
	stub.savedService = service
	stub.savedSecret = append([]byte(nil), secret...)
	stub.saveCalls++
	return nil
}

func (stub *apiCredentialStoreStub) Delete(_ context.Context, service string) error {
	stub.deletedService = service
	stub.deleteCalls++
	return nil
}

func (stub *apiSubscriptionsQueryStub) Current(
	_ context.Context,
	atMS int64,
) (apisubscriptions.CurrentSnapshot, error) {
	stub.atMS = atMS
	stub.calls++
	return apisubscriptions.CurrentSnapshot{EvaluatedAtMS: atMS}, nil
}

func (stub *invocationUsageQueryStub) InvocationUsage(
	_ context.Context,
	request invocationusage.InvocationUsageRequest,
) (invocationusage.InvocationUsageResponse, error) {
	stub.request = request
	return invocationusage.InvocationUsageResponse{}, nil
}

func (*usageQueryStub) UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	return usagecost.UsageCostResponse{}, nil
}

func (stub *usageQueryStub) ListSessions(_ context.Context, request basequery.Request) (usagecost.SessionListResponse, error) {
	stub.request = request
	return stub.sessions, nil
}

func (*usageQueryStub) SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	return usagecost.SessionDetailResponse{}, nil
}

func (*usageQueryStub) ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error) {
	return usagecost.ProjectListResponse{}, nil
}

func (*usageQueryStub) ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	return usagecost.ProjectDetailResponse{}, nil
}

type accountSnapshotQueryStub struct {
	snapshot AccountSnapshot
	calls    int
	scope    agentprovider.Scope
}

type quotaRefreshCommandStub struct {
	schedule store.SourceRefreshSchedule
	calls    int
}

func (stub *quotaRefreshCommandStub) RequestQuotaRefresh(
	context.Context,
	quotaonline.RefreshSource,
) (store.SourceRefreshSchedule, error) {
	stub.calls++
	return stub.schedule, nil
}

type providerQuotaRefreshStub struct {
	scope agentprovider.Scope
	calls int
}

func (stub *providerQuotaRefreshStub) RefreshQuota(
	_ context.Context,
	scope agentprovider.Scope,
) (agentprovider.Context, error) {
	stub.scope = scope
	stub.calls++
	return agentprovider.Context{EffectiveProvider: scope.Provider}, nil
}

type pricingCatalogQueryStub struct{}

func (pricingCatalogQueryStub) Current(context.Context, agentprovider.Scope) (pricingcatalog.CurrentResponse, error) {
	return pricingcatalog.CurrentResponse{}, nil
}

func (stub *accountSnapshotQueryStub) AccountSnapshot(_ context.Context, scope agentprovider.Scope) (AccountSnapshot, error) {
	stub.calls++
	stub.scope = scope
	return stub.snapshot, nil
}

type runtimeQueryStub struct{}

func (runtimeQueryStub) QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error) {
	return runtimeinfo.QuotaCurrentResponse{}, nil
}

func (runtimeQueryStub) QuotaPace(context.Context, int64) (runtimeinfo.QuotaPaceResponse, error) {
	return runtimeinfo.QuotaPaceResponse{}, nil
}

type agentQuotaQueryStub struct {
	scope agentprovider.Scope
	atMS  int64
}

func (stub *agentQuotaQueryStub) QuotaCurrent(
	_ context.Context,
	scope agentprovider.Scope,
	atMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	stub.scope, stub.atMS = scope, atMS
	return runtimeinfo.QuotaCurrentResponse{
		ProviderContext: agentprovider.Context{EffectiveProvider: scope.Provider},
	}, nil
}

func (stub *agentQuotaQueryStub) QuotaPace(
	_ context.Context,
	scope agentprovider.Scope,
	atMS int64,
) (runtimeinfo.QuotaPaceResponse, error) {
	stub.scope, stub.atMS = scope, atMS
	return runtimeinfo.QuotaPaceResponse{
		ProviderContext: agentprovider.Context{EffectiveProvider: scope.Provider},
	}, nil
}

func (runtimeQueryStub) ListSources(context.Context, basequery.Request) (runtimeinfo.SourceListResponse, error) {
	return runtimeinfo.SourceListResponse{}, nil
}

func (runtimeQueryStub) Source(context.Context, runtimeinfo.SourceDetailRequest) (runtimeinfo.SourceDetailResponse, error) {
	return runtimeinfo.SourceDetailResponse{}, nil
}

func (runtimeQueryStub) ListJobs(context.Context, basequery.Request) (runtimeinfo.JobListResponse, error) {
	return runtimeinfo.JobListResponse{}, nil
}

func (runtimeQueryStub) Job(context.Context, runtimeinfo.JobDetailRequest) (runtimeinfo.JobDetailResponse, error) {
	return runtimeinfo.JobDetailResponse{}, nil
}

func (runtimeQueryStub) ListHealth(context.Context, basequery.Request) (runtimeinfo.HealthListResponse, error) {
	return runtimeinfo.HealthListResponse{}, nil
}

func (runtimeQueryStub) Health(context.Context, runtimeinfo.HealthDetailRequest) (runtimeinfo.HealthDetailResponse, error) {
	return runtimeinfo.HealthDetailResponse{}, nil
}

func (runtimeQueryStub) DataHealth(context.Context, int64) (runtimeinfo.DataHealthResponse, error) {
	return runtimeinfo.DataHealthResponse{}, nil
}

func (runtimeQueryStub) Settings(context.Context) (runtimeinfo.SettingsResponse, error) {
	return runtimeinfo.SettingsResponse{}, nil
}
