package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestServiceRejectsInvalidCodexAccountSnapshotEvaluation(t *testing.T) {
	account := &accountSnapshotQueryStub{snapshot: AccountSnapshot{}}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:     runtimeQueryStub{},
		AccountSnapshot: account,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AccountSnapshot(context.Background(), AccountSnapshotQuery{
		Scope:         agentprovider.Scope{Provider: agentprovider.Codex},
		EvaluatedAtMS: 1,
		TimeZone:      "Local",
	})
	if !errors.Is(err, basequery.ErrValidation) || account.calls != 0 {
		t.Fatalf("AccountSnapshot(invalid zone) error = %v, calls = %d", err, account.calls)
	}
}

func TestServiceOmitsSubscriptionForNonCodexProviders(t *testing.T) {
	account := &accountSnapshotQueryStub{snapshot: AccountSnapshot{
		Subscription: &subscriptionaccounts.Account{AccountID: "should-not-escape", Current: true},
	}}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:     runtimeQueryStub{},
		AccountSnapshot: account,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.AccountSnapshot(context.Background(), AccountSnapshotQuery{
		Scope: agentprovider.Scope{Provider: agentprovider.Cursor},
	})
	if err != nil || got.Subscription != nil {
		t.Fatalf("AccountSnapshot(cursor) = %#v, %v", got, err)
	}
}

func TestServiceListCodexSubscriptionAccountsDelegatesAndEncodes(t *testing.T) {
	plan := subscriptionaccounts.PlanPlus
	firstHistoryAtMS := int64(1_721_700_000_000)
	lastHistoryAtMS := int64(1_758_000_000_000)
	accounts := &codexSubscriptionAccountsStub{snapshot: subscriptionaccounts.Snapshot{
		Version:                 subscriptionaccounts.ContractVersion,
		EvaluatedAtMS:           1_700_000_000_000,
		TimeZone:                "Asia/Shanghai",
		AutomaticDateCapability: subscriptionaccounts.AutomaticDateCapabilityManualOnly,
		Accounts: []subscriptionaccounts.Account{{
			AccountID:          "22222222-2222-4222-8222-222222222222",
			AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable,
			ResolvedPlan:       &plan,
			ResolvedPlanSource: subscriptionaccounts.ValueSourceManual,
			DateSource:         subscriptionaccounts.ValueSourceUnavailable,
			DateState:          subscriptionaccounts.DateStateUnavailable,
			HasManual:          true,
			LegacyQuotaHistory: &subscriptionaccounts.LegacyQuotaHistory{
				State:               subscriptionaccounts.LegacyQuotaHistoryAvailable,
				ObservationCount:    11401,
				CycleCount:          8,
				FirstObservedAtMS:   &firstHistoryAtMS,
				LastObservedAtMS:    &lastHistoryAtMS,
				AssociationRevision: nil,
			},
		}},
	}}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ListCodexSubscriptionAccounts(context.Background(), 1_700_000_000_000, "Asia/Shanghai")
	if err != nil || accounts.listCalls != 1 || got.TimeZone != "Asia/Shanghai" ||
		got.AutomaticDateCapability != "CODEX_SUBSCRIPTION_AUTOMATIC_DATE_CAPABILITY_MANUAL_ONLY" ||
		len(got.Accounts) != 1 || got.Accounts[0].ResolvedPlan == nil ||
		*got.Accounts[0].ResolvedPlan != "CODEX_SUBSCRIPTION_PLAN_PLUS" ||
		got.Accounts[0].LegacyQuotaHistory == nil ||
		got.Accounts[0].LegacyQuotaHistory.State != "CODEX_LEGACY_QUOTA_HISTORY_STATE_AVAILABLE" ||
		got.Accounts[0].LegacyQuotaHistory.ObservationCount != 11401 ||
		got.Accounts[0].LegacyQuotaHistory.CycleCount != 8 {
		t.Fatalf("ListCodexSubscriptionAccounts() = %#v, err=%v, calls=%d", got, err, accounts.listCalls)
	}
}

func TestServiceCreateCodexSubscriptionAccountRejectsInvalidUUID(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateCodexSubscriptionAccount(context.Background(), CodexSubscriptionCreateRequest{
		ManualEntryID: "not-a-uuid",
	})
	if !errors.Is(err, basequery.ErrValidation) || accounts.createCalls != 0 {
		t.Fatalf("CreateCodexSubscriptionAccount() error = %v, calls = %d", err, accounts.createCalls)
	}
}

func TestServiceCreateCodexSubscriptionAccountDelegates(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{
		mutation: CodexSubscriptionMutation{Result: string(subscriptionaccounts.MutationApplied)},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := "33333333-3333-4333-8333-333333333333"
	email := "person@example.com"
	got, err := service.CreateCodexSubscriptionAccount(context.Background(), CodexSubscriptionCreateRequest{
		ManualEntryID: id,
		Fields:        subscriptionaccounts.ManualFields{Email: &email},
	})
	if err != nil || accounts.createCalls != 1 || got.Result != "CODEX_SUBSCRIPTION_MUTATION_RESULT_APPLIED" ||
		accounts.create.ManualEntryID != id || accounts.create.Fields.Email == nil ||
		*accounts.create.Fields.Email != email {
		t.Fatalf("CreateCodexSubscriptionAccount() = %#v, err=%v, stub=%#v", got, err, accounts)
	}
}

func TestServiceDeleteCodexSubscriptionAccountValidatesAndDelegates(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{
		mutation: CodexSubscriptionMutation{Result: string(subscriptionaccounts.MutationApplied)},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteCodexSubscriptionAccount(context.Background(), subscriptionaccounts.DeleteRequest{
		AccountID: "not-a-uuid",
	}); !errors.Is(err, basequery.ErrValidation) || accounts.deleteCalls != 0 {
		t.Fatalf("DeleteCodexSubscriptionAccount(invalid) error = %v, calls = %d", err, accounts.deleteCalls)
	}
	detectedRevision, manualRevision, linkRevision := int64(2), int64(3), int64(4)
	request := subscriptionaccounts.DeleteRequest{
		AccountID:                "33333333-3333-4333-8333-333333333333",
		ExpectedDetectedRevision: &detectedRevision,
		ExpectedManualRevision:   &manualRevision,
		ExpectedLinkRevision:     &linkRevision,
	}
	got, err := service.DeleteCodexSubscriptionAccount(context.Background(), request)
	if err != nil || accounts.deleteCalls != 1 ||
		got.Result != "CODEX_SUBSCRIPTION_MUTATION_RESULT_APPLIED" || accounts.delete != request {
		t.Fatalf("DeleteCodexSubscriptionAccount() = %#v, err=%v, stub=%#v", got, err, accounts)
	}
}

func TestServiceLinkLegacyQuotaHistoryDelegatesExplicitConfirmation(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{
		legacyMutation: store.LegacyQuotaHistoryMutation{
			Result: store.LegacyQuotaHistoryMutationApplied,
		},
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.LinkLegacyQuotaHistory(context.Background(), LegacyQuotaHistoryLinkRequest{
		DetectedAccountID:        "22222222-2222-4222-8222-222222222222",
		ExpectedDetectedRevision: 7,
	})
	if err != nil || accounts.legacyLinkCalls != 1 ||
		accounts.legacyLink.DetectedAccountID != "22222222-2222-4222-8222-222222222222" ||
		accounts.legacyLink.ExpectedDetectedRevision != 7 ||
		got.Result != "CODEX_SUBSCRIPTION_MUTATION_RESULT_APPLIED" {
		t.Fatalf("LinkLegacyQuotaHistory() = %#v, err=%v, stub=%#v", got, err, accounts)
	}
}

type codexSubscriptionAccountsStub struct {
	snapshot        subscriptionaccounts.Snapshot
	listCalls       int
	createCalls     int
	create          CodexSubscriptionCreateRequest
	deleteCalls     int
	delete          subscriptionaccounts.DeleteRequest
	mutation        CodexSubscriptionMutation
	legacyMutation  store.LegacyQuotaHistoryMutation
	legacyLink      LegacyQuotaHistoryLinkRequest
	legacyLinkCalls int
	mutationErr     error
}

func (stub *codexSubscriptionAccountsStub) LinkLegacyQuotaHistory(
	_ context.Context,
	request LegacyQuotaHistoryLinkRequest,
) (store.LegacyQuotaHistoryMutation, error) {
	stub.legacyLinkCalls++
	stub.legacyLink = request
	return stub.legacyMutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) UnlinkLegacyQuotaHistory(
	context.Context,
	LegacyQuotaHistoryUnlinkRequest,
) (store.LegacyQuotaHistoryMutation, error) {
	return stub.legacyMutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) ListCodexSubscriptionAccounts(
	context.Context,
	int64,
	string,
) (subscriptionaccounts.Snapshot, error) {
	stub.listCalls++
	return stub.snapshot, nil
}

func (stub *codexSubscriptionAccountsStub) CreateCodexSubscriptionAccount(
	_ context.Context,
	request CodexSubscriptionCreateRequest,
) (CodexSubscriptionMutation, error) {
	stub.createCalls++
	stub.create = request
	return stub.mutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) UpdateCodexSubscriptionAccount(
	context.Context,
	CodexSubscriptionUpdateRequest,
) (CodexSubscriptionMutation, error) {
	return stub.mutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) DeleteCodexSubscriptionAccount(
	_ context.Context,
	request CodexSubscriptionDeleteRequest,
) (CodexSubscriptionMutation, error) {
	stub.deleteCalls++
	stub.delete = request
	return stub.mutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) LinkCodexSubscriptionAccount(
	context.Context,
	CodexSubscriptionLinkRequest,
) (CodexSubscriptionMutation, error) {
	return stub.mutation, stub.mutationErr
}

func (stub *codexSubscriptionAccountsStub) UnlinkCodexSubscriptionAccount(
	context.Context,
	CodexSubscriptionUnlinkRequest,
) (CodexSubscriptionMutation, error) {
	return stub.mutation, stub.mutationErr
}

func TestMapCodexSubscriptionErrorDoesNotEchoEmail(t *testing.T) {
	err := mapCodexSubscriptionError(subscriptionaccounts.ErrInvalidEmail)
	if !errors.Is(err, basequery.ErrValidation) || strings.Contains(err.Error(), "@") {
		t.Fatalf("mapCodexSubscriptionError() = %v", err)
	}
}

func TestMapCodexSubscriptionStoreValidationKeepsManualField(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{
		mutationErr: errors.Join(store.ErrInvalidRecord, subscriptionaccounts.ErrInvalidEmail),
	}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	email := "invalid email"
	_, err = service.CreateCodexSubscriptionAccount(context.Background(), CodexSubscriptionCreateRequest{
		ManualEntryID: "33333333-3333-4333-8333-333333333333",
		Fields:        subscriptionaccounts.ManualFields{Email: &email},
	})
	envelope, ok := basequery.ErrorEnvelopeFrom(err)
	if !ok || envelope.Error.Field == nil || *envelope.Error.Field != "manual.email" {
		t.Fatalf("CreateCodexSubscriptionAccount(invalid email) = %#v, %v", envelope, err)
	}
}

func TestServiceUpdateCodexSubscriptionAccountRejectsInvalidOptionalRevision(t *testing.T) {
	accounts := &codexSubscriptionAccountsStub{}
	service, err := NewService(ServiceConfig{
		UsageCost: &usageQueryStub{}, InvocationUsage: &invocationUsageQueryStub{}, PricingCatalog: pricingCatalogQueryStub{},
		RuntimeInfo:        runtimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid := int64(0)
	_, err = service.UpdateCodexSubscriptionAccount(context.Background(), CodexSubscriptionUpdateRequest{
		AccountID:              "33333333-3333-4333-8333-333333333333",
		ExpectedManualRevision: &invalid,
	})
	envelope, ok := basequery.ErrorEnvelopeFrom(err)
	if !ok || envelope.Error.Field == nil || *envelope.Error.Field != "expectedManualRevision" {
		t.Fatalf("UpdateCodexSubscriptionAccount(invalid revision) = %#v, %v", envelope, err)
	}
}
