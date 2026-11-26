package agentrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

type staticStateReader struct {
	state providercontrol.Snapshot
}

func (reader staticStateReader) ProviderState(string) (providercontrol.Snapshot, error) {
	return reader.state, nil
}
func (staticStateReader) EnabledProviders() []string { return nil }
func (staticStateReader) Generation() uint64         { return 1 }

func TestRouterAGYUnboundUnavailableBoundRoutesAndDisabledDenied(t *testing.T) {
	t.Parallel()
	calls := []string{}
	codex := routingStub{provider: agentprovider.Codex, calls: &calls}
	cursor := routingStub{provider: agentprovider.Cursor, calls: &calls}
	grok := routingStub{provider: agentprovider.Grok, calls: &calls}
	service, err := New(codex, codex, cursor, cursor, grok, grok)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	quotaCodex := quotaRoutingStub{provider: agentprovider.Codex, calls: &calls}
	quotaCursor := quotaRoutingStub{provider: agentprovider.Cursor, calls: &calls}
	quotaGrok := quotaRoutingStub{provider: agentprovider.Grok, calls: &calls}
	quotaRouter, err := NewQuota(quotaCodex, quotaCursor, quotaGrok)
	if err != nil {
		t.Fatalf("NewQuota() error = %v", err)
	}

	// 未绑定后端时必须返回 unavailable，而非参数校验失败
	if _, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
	}); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("UsageCost(unbound agy) error = %v, want ErrUnavailable", err)
	}
	if _, err := service.InvocationUsage(context.Background(), invocationusage.InvocationUsageRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
	}); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("InvocationUsage(agy) error = %v, want ErrUnavailable", err)
	}
	if _, err := quotaRouter.QuotaCurrent(context.Background(), agentprovider.Scope{Provider: agentprovider.AGY}, 100); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("QuotaCurrent(unbound agy) error = %v, want ErrUnavailable", err)
	}
	if _, err := quotaRouter.RefreshQuota(context.Background(), agentprovider.Scope{Provider: agentprovider.AGY}); !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("RefreshQuota(unbound agy) error = %v, want ErrUnavailable", err)
	}

	// 绑定后端后正确路由并回显 EffectiveProvider == "agy"
	agyUsage := routingStub{provider: agentprovider.AGY, calls: &calls}
	agyQuota := quotaRoutingStub{provider: agentprovider.AGY, calls: &calls}
	service.BindAGY(agyUsage)
	quotaRouter.BindAGY(agyQuota)
	usageResp, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
	})
	if err != nil || usageResp.ProviderContext.EffectiveProvider != agentprovider.AGY {
		t.Fatalf("UsageCost(bound agy) = %#v, %v", usageResp, err)
	}
	quotaResp, err := quotaRouter.QuotaCurrent(context.Background(), agentprovider.Scope{Provider: agentprovider.AGY}, 100)
	if err != nil || quotaResp.ProviderContext.EffectiveProvider != agentprovider.AGY {
		t.Fatalf("QuotaCurrent(bound agy) = %#v, %v", quotaResp, err)
	}

	// 显式关闭时稳定返回 ErrProviderDisabled
	disabled := staticStateReader{state: providercontrol.Snapshot{
		Provider:   agentprovider.AGY,
		Intent:     preferences.ProviderIntentDisabled,
		Discovery:  providercontrol.DiscoveryAvailable,
		Effective:  providercontrol.EffectiveDisabled,
		ReasonCode: providercontrol.ReasonDisabled,
	}}
	service.BindStates(disabled)
	quotaRouter.BindStates(disabled)
	if _, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		Provider: agentprovider.Scope{Provider: agentprovider.AGY},
	}); !errors.Is(err, basequery.ErrProviderDisabled) {
		t.Fatalf("UsageCost(disabled agy) error = %v, want ErrProviderDisabled", err)
	}
	if _, err := quotaRouter.QuotaCurrent(context.Background(), agentprovider.Scope{Provider: agentprovider.AGY}, 100); !errors.Is(err, basequery.ErrProviderDisabled) {
		t.Fatalf("QuotaCurrent(disabled agy) error = %v, want ErrProviderDisabled", err)
	}
}
