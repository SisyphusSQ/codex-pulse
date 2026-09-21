package core

import (
	"context"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountquota"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type codexAccountQuotasStub struct {
	snapshot   accountquota.Snapshot
	clear      accountquota.ClearReceipt
	listCalls  int
	clearCalls int
}

func (stub *codexAccountQuotasStub) ListCodexAccountQuotas(
	context.Context,
	int64,
	string,
) (accountquota.Snapshot, error) {
	stub.listCalls++
	return stub.snapshot, nil
}

func (stub *codexAccountQuotasStub) ClearCodexAccountQuotaHistory(
	context.Context,
) (accountquota.ClearReceipt, error) {
	stub.clearCalls++
	return stub.clear, nil
}

func TestServiceListsAndClearsCodexAccountQuotas(t *testing.T) {
	remaining, used := 79.0, 21.0
	windowMinutes, resetAtMS, collectedAtMS := int64(10_080), int64(1_784_100_000_000), int64(1_784_000_000_000)
	stub := &codexAccountQuotasStub{
		snapshot: accountquota.Snapshot{
			Version: accountquota.ContractVersion, EvaluatedAtMS: 1_784_000_100_000, TimeZone: "Asia/Shanghai",
			Accounts: []accountquota.Account{{
				Subscription: subscriptionaccounts.Account{
					AccountID: "11111111-1111-4111-8111-111111111111", Current: true, Detected: true,
					AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable,
					ResolvedPlanSource: subscriptionaccounts.ValueSourceUnavailable,
					DateSource:         subscriptionaccounts.ValueSourceUnavailable,
					DateState:          subscriptionaccounts.DateStateUnavailable,
				},
				Windows: []accountquota.Window{{
					WindowKind: store.QuotaWindowSecondary, LimitID: "codex",
					UsedPercent: &used, RemainingPercent: &remaining, WindowMinutes: &windowMinutes,
					ResetsAtMS: &resetAtMS, LastCollectedAtMS: &collectedAtMS,
					Freshness: store.QuotaCurrentFresh, Conflict: store.QuotaConflictNone,
				}},
				LastCollectedAtMS: &collectedAtMS,
			}},
		},
		clear: accountquota.ClearReceipt{AccountCount: 1, WindowCount: 2, ObservationCount: 3},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{},
		PricingCatalog: pricingCatalogQueryStub{}, RuntimeInfo: runtimeQueryStub{},
		CodexAccountQuotas: stub,
	})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := service.ListCodexAccountQuotas(t.Context(), 1_784_000_100_000, "Asia/Shanghai")
	if err != nil || stub.listCalls != 1 || listed.Version != accountquota.ContractVersion ||
		len(listed.Accounts) != 1 || len(listed.Accounts[0].Windows) != 1 ||
		listed.Accounts[0].Windows[0].RemainingPercent == nil ||
		*listed.Accounts[0].Windows[0].RemainingPercent != remaining {
		t.Fatalf("ListCodexAccountQuotas() = %#v, err=%v, calls=%d", listed, err, stub.listCalls)
	}
	cleared, err := service.ClearCodexAccountQuotaHistory(t.Context())
	if err != nil || stub.clearCalls != 1 || cleared != stub.clear {
		t.Fatalf("ClearCodexAccountQuotaHistory() = %#v, err=%v, calls=%d", cleared, err, stub.clearCalls)
	}
}
