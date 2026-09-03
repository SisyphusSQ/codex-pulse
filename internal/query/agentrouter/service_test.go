package agentrouter

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

type routingStub struct {
	provider string
	calls    *[]string
}

type quotaRoutingStub struct {
	provider string
	calls    *[]string
}

func (stub quotaRoutingStub) QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error) {
	*stub.calls = append(*stub.calls, stub.provider+":quota-current")
	return runtimeinfo.QuotaCurrentResponse{}, nil
}

func (stub quotaRoutingStub) QuotaPace(context.Context, int64) (runtimeinfo.QuotaPaceResponse, error) {
	*stub.calls = append(*stub.calls, stub.provider+":quota-pace")
	return runtimeinfo.QuotaPaceResponse{}, nil
}

func (stub quotaRoutingStub) RefreshQuota(context.Context) error {
	*stub.calls = append(*stub.calls, stub.provider+":quota-refresh")
	return nil
}

func (stub routingStub) record(name string) {
	*stub.calls = append(*stub.calls, stub.provider+":"+name)
}

func (stub routingStub) UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	stub.record("usage")
	return usagecost.UsageCostResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}
func (stub routingStub) ListSessions(context.Context, basequery.Request) (usagecost.SessionListResponse, error) {
	stub.record("sessions")
	return usagecost.SessionListResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}
func (stub routingStub) SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	stub.record("session-detail")
	return usagecost.SessionDetailResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}
func (stub routingStub) ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error) {
	stub.record("projects")
	return usagecost.ProjectListResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}
func (stub routingStub) ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	stub.record("project-detail")
	return usagecost.ProjectDetailResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}
func (stub routingStub) InvocationUsage(context.Context, invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error) {
	stub.record("invocation")
	return invocationusage.InvocationUsageResponse{ProviderContext: agentprovider.Context{EffectiveProvider: stub.provider}}, nil
}

func TestRouterDefaultsToCodexAndRoutesExplicitCursorAndGrok(t *testing.T) {
	calls := []string{}
	codex := routingStub{provider: agentprovider.Codex, calls: &calls}
	cursor := routingStub{provider: agentprovider.Cursor, calls: &calls}
	grok := routingStub{provider: agentprovider.Grok, calls: &calls}
	service, err := New(codex, codex, cursor, cursor, grok, grok)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{})
	if err != nil || usage.ProviderContext.EffectiveProvider != agentprovider.Codex {
		t.Fatalf("default usage = %#v, %v", usage, err)
	}
	sessions, err := service.ListSessions(context.Background(), basequery.Request{
		Provider: agentprovider.Scope{Provider: agentprovider.Cursor},
	})
	if err != nil || sessions.ProviderContext.EffectiveProvider != agentprovider.Cursor {
		t.Fatalf("cursor sessions = %#v, %v", sessions, err)
	}
	projects, err := service.ListProjects(context.Background(), basequery.Request{
		Provider: agentprovider.Scope{Provider: agentprovider.Grok},
	})
	if err != nil || projects.ProviderContext.EffectiveProvider != agentprovider.Grok {
		t.Fatalf("grok projects = %#v, %v", projects, err)
	}
	if len(calls) != 3 || calls[0] != "codex:usage" || calls[1] != "cursor:sessions" || calls[2] != "grok:projects" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestRouterRejectsUnknownProviderBeforeCallingBackend(t *testing.T) {
	calls := []string{}
	stub := routingStub{provider: agentprovider.Codex, calls: &calls}
	service, err := New(stub, stub, stub, stub, stub, stub)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = service.InvocationUsage(context.Background(), invocationusage.InvocationUsageRequest{
		Provider: agentprovider.Scope{Provider: "openai"},
	})
	if !errors.Is(err, basequery.ErrValidation) || len(calls) != 0 {
		t.Fatalf("error = %v, calls = %#v", err, calls)
	}
}

func TestQuotaRouterScopesRequestsAndEchoesEffectiveProvider(t *testing.T) {
	calls := []string{}
	codex := quotaRoutingStub{provider: agentprovider.Codex, calls: &calls}
	cursor := quotaRoutingStub{provider: agentprovider.Cursor, calls: &calls}
	grok := quotaRoutingStub{provider: agentprovider.Grok, calls: &calls}
	service, err := NewQuota(codex, cursor, grok)
	if err != nil {
		t.Fatalf("NewQuota() error = %v", err)
	}
	current, err := service.QuotaCurrent(
		context.Background(), agentprovider.Scope{Provider: agentprovider.Cursor}, 100,
	)
	if err != nil || current.ProviderContext.EffectiveProvider != agentprovider.Cursor {
		t.Fatalf("cursor current = %#v, %v", current, err)
	}
	grokCurrent, err := service.QuotaCurrent(
		context.Background(), agentprovider.Scope{Provider: agentprovider.Grok}, 100,
	)
	if err != nil || grokCurrent.ProviderContext.EffectiveProvider != agentprovider.Grok {
		t.Fatalf("grok current = %#v, %v", grokCurrent, err)
	}
	pace, err := service.QuotaPace(context.Background(), agentprovider.Scope{}, 100)
	if err != nil || pace.ProviderContext.EffectiveProvider != agentprovider.Codex ||
		len(pace.ProviderContext.Sources) == 0 || len(pace.ProviderContext.Capabilities) == 0 {
		t.Fatalf("default pace = %#v, %v", pace, err)
	}
	if want := []string{"cursor:quota-current", "grok:quota-current", "codex:quota-pace"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestQuotaRouterRefreshesOnlyExplicitExternalProvider(t *testing.T) {
	t.Parallel()
	calls := []string{}
	codex := quotaRoutingStub{provider: agentprovider.Codex, calls: &calls}
	cursor := quotaRoutingStub{provider: agentprovider.Cursor, calls: &calls}
	grok := quotaRoutingStub{provider: agentprovider.Grok, calls: &calls}
	service, err := NewQuota(codex, cursor, grok)
	if err != nil {
		t.Fatalf("NewQuota() error = %v", err)
	}
	for _, provider := range []string{agentprovider.Cursor, agentprovider.Grok} {
		providerContext, err := service.RefreshQuota(
			context.Background(), agentprovider.Scope{Provider: provider},
		)
		if err != nil || providerContext.EffectiveProvider != provider {
			t.Fatalf("RefreshQuota(%q) = %#v, %v", provider, providerContext, err)
		}
	}
	if _, err := service.RefreshQuota(context.Background(), agentprovider.Scope{}); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("RefreshQuota(default Codex) error = %v", err)
	}
	if want := []string{"cursor:quota-refresh", "grok:quota-refresh"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRouterIsolatesProviderErrors(t *testing.T) {
	t.Parallel()

	calls := []string{}
	wantErr := errors.New("cursor backend failed")
	codex := routingStub{provider: agentprovider.Codex, calls: &calls}
	cursor := failingRoutingStub{provider: agentprovider.Cursor, calls: &calls, err: wantErr}
	grok := routingStub{provider: agentprovider.Grok, calls: &calls}
	service, err := New(codex, codex, cursor, cursor, grok, grok)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.Cursor},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("cursor error = %v, want %v", err, wantErr)
	}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.Grok},
	})
	if err != nil || usage.ProviderContext.EffectiveProvider != agentprovider.Grok {
		t.Fatalf("grok usage = %#v, %v", usage, err)
	}
	if want := []string{"cursor:usage", "grok:usage"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

type failingRoutingStub struct {
	provider string
	calls    *[]string
	err      error
}

func (stub failingRoutingStub) record(name string) {
	*stub.calls = append(*stub.calls, stub.provider+":"+name)
}

func (stub failingRoutingStub) UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	stub.record("usage")
	return usagecost.UsageCostResponse{}, stub.err
}
func (stub failingRoutingStub) ListSessions(context.Context, basequery.Request) (usagecost.SessionListResponse, error) {
	stub.record("sessions")
	return usagecost.SessionListResponse{}, stub.err
}
func (stub failingRoutingStub) SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	stub.record("session-detail")
	return usagecost.SessionDetailResponse{}, stub.err
}
func (stub failingRoutingStub) ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error) {
	stub.record("projects")
	return usagecost.ProjectListResponse{}, stub.err
}
func (stub failingRoutingStub) ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	stub.record("project-detail")
	return usagecost.ProjectDetailResponse{}, stub.err
}
func (stub failingRoutingStub) InvocationUsage(context.Context, invocationusage.InvocationUsageRequest) (invocationusage.InvocationUsageResponse, error) {
	stub.record("invocation")
	return invocationusage.InvocationUsageResponse{}, stub.err
}
