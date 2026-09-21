package accountquota

import (
	"context"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type recordsReaderStub struct {
	records store.CodexAccountQuotaRecords
}

func (stub recordsReaderStub) CodexAccountQuotaRecords(
	context.Context,
	int64,
) (store.CodexAccountQuotaRecords, error) {
	return stub.records, nil
}

func TestListKeepsCurrentFirstAndOmitsEmptyHistoricalAccounts(t *testing.T) {
	const evaluatedAtMS = int64(1_784_000_000_000)
	scopeCurrent := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scopeHistorical := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	scopeEmpty := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	currentID := "11111111-1111-4111-8111-111111111111"
	historicalID := "22222222-2222-4222-8222-222222222222"
	emptyID := "33333333-3333-4333-8333-333333333333"
	lastCurrent, lastHistorical := evaluatedAtMS-1_000, evaluatedAtMS-2_000
	currentUsed, historicalUsed := 20.0, 40.0
	reader := recordsReaderStub{records: store.CodexAccountQuotaRecords{
		Subscriptions: subscriptionaccounts.Records{
			Binding: subscriptionaccounts.Binding{
				State:        subscriptionaccounts.BindingConfirmed,
				AccountScope: &scopeCurrent,
			},
			Detected: []subscriptionaccounts.DetectedAccount{
				{
					AccountScope: scopeHistorical, DetectedAccountID: historicalID,
					AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable, Revision: 1,
				},
				{
					AccountScope: scopeCurrent, DetectedAccountID: currentID,
					AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable, Revision: 1,
				},
				{
					AccountScope: scopeEmpty, DetectedAccountID: emptyID,
					AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable, Revision: 1,
				},
			},
		},
		Accounts: []store.CodexDetectedAccountQuotaRecord{
			{
				DetectedAccountID: historicalID,
				Windows: []store.QuotaCurrent{{
					WindowKind: store.QuotaWindowSecondary, LimitID: "codex",
					EffectiveUsedPercent: &historicalUsed, LastSuccessAtMS: &lastHistorical,
					FreshnessState: store.QuotaCurrentStale, ConflictState: store.QuotaConflictNone,
				}},
			},
			{
				DetectedAccountID: currentID,
				Windows: []store.QuotaCurrent{{
					WindowKind: store.QuotaWindowPrimary, LimitID: "codex",
					EffectiveUsedPercent: &currentUsed, LastSuccessAtMS: &lastCurrent,
					FreshnessState: store.QuotaCurrentFresh, ConflictState: store.QuotaConflictNone,
				}},
			},
			{DetectedAccountID: emptyID},
		},
	}}
	service, err := NewService(reader)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	snapshot, err := service.List(t.Context(), evaluatedAtMS, "Asia/Shanghai")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(snapshot.Accounts) != 2 {
		t.Fatalf("accounts = %#v, want current plus retained historical", snapshot.Accounts)
	}
	if !snapshot.Accounts[0].Subscription.Current ||
		snapshot.Accounts[0].Subscription.DetectedAccountID == nil ||
		*snapshot.Accounts[0].Subscription.DetectedAccountID != currentID {
		t.Fatalf("first account = %#v, want current", snapshot.Accounts[0])
	}
	if snapshot.Accounts[1].Subscription.Current ||
		snapshot.Accounts[1].Subscription.DetectedAccountID == nil ||
		*snapshot.Accounts[1].Subscription.DetectedAccountID != historicalID {
		t.Fatalf("second account = %#v, want retained historical", snapshot.Accounts[1])
	}
	if got := *snapshot.Accounts[0].Windows[0].RemainingPercent; got != 80 {
		t.Fatalf("current remaining = %v, want 80", got)
	}
}
