package helper

import (
	"context"
	"strings"
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCServerMapsCodexSubscriptionListCreateAndDelete(t *testing.T) {
	t.Parallel()

	plan := subscriptionaccounts.PlanPlus
	accounts := &helperCodexSubscriptionStub{
		snapshot: subscriptionaccounts.Snapshot{
			Version:                 subscriptionaccounts.ContractVersion,
			EvaluatedAtMS:           1_700_000_000_000,
			TimeZone:                "UTC",
			AutomaticDateCapability: subscriptionaccounts.AutomaticDateCapabilityManualOnly,
			Accounts: []subscriptionaccounts.Account{{
				AccountID:          "44444444-4444-4444-8444-444444444444",
				AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable,
				ResolvedPlan:       &plan,
				ResolvedPlanSource: subscriptionaccounts.ValueSourceManual,
				DateSource:         subscriptionaccounts.ValueSourceUnavailable,
				DateState:          subscriptionaccounts.DateStateUnavailable,
				HasManual:          true,
			}},
		},
		mutation: core.CodexSubscriptionMutation{Result: string(subscriptionaccounts.MutationApplied)},
	}
	business, err := core.NewService(core.ServiceConfig{
		UsageCost: &helperUsageQueryStub{}, InvocationUsage: &helperInvocationQueryStub{},
		PricingCatalog:     helperPricingCatalogQueryStub{},
		RuntimeInfo:        helperRuntimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, authorize := startConfiguredTestGRPCServer(t, func(config *ServerConfig) {
		config.Service = business
	})
	list, err := client.ListCodexSubscriptionAccounts(
		authorize(t.Context()),
		&corev1.CodexSubscriptionAccountsRequest{EvaluatedAtMs: 1_700_000_000_000, TimeZone: "UTC"},
	)
	if err != nil || list.GetVersion() != subscriptionaccounts.ContractVersion ||
		list.GetAutomaticDateCapability() != corev1.CodexSubscriptionAutomaticDateCapability_CODEX_SUBSCRIPTION_AUTOMATIC_DATE_CAPABILITY_MANUAL_ONLY ||
		len(list.GetAccounts()) != 1 {
		t.Fatalf("ListCodexSubscriptionAccounts() = %#v, %v", list, err)
	}

	email := "person@example.com"
	create, err := client.CreateCodexSubscriptionAccount(
		authorize(t.Context()),
		&corev1.CreateCodexSubscriptionAccountRequest{
			ManualEntryId: "55555555-5555-4555-8555-555555555555",
			Manual:        &corev1.CodexSubscriptionManualFields{Email: &email},
		},
	)
	if err != nil || create.GetResult() != corev1.CodexSubscriptionMutationResult_CODEX_SUBSCRIPTION_MUTATION_RESULT_APPLIED ||
		accounts.create.Fields.Email == nil || *accounts.create.Fields.Email != email {
		t.Fatalf("CreateCodexSubscriptionAccount() = %#v, %v, stub=%#v", create, err, accounts.create)
	}

	detectedRevision, manualRevision, linkRevision := int64(2), int64(3), int64(4)
	deleted, err := client.DeleteCodexSubscriptionAccount(
		authorize(t.Context()),
		&corev1.DeleteCodexSubscriptionAccountRequest{
			AccountId:                "44444444-4444-4444-8444-444444444444",
			ExpectedDetectedRevision: &detectedRevision,
			ExpectedManualRevision:   &manualRevision,
			ExpectedLinkRevision:     &linkRevision,
		},
	)
	if err != nil || deleted.GetResult() != corev1.CodexSubscriptionMutationResult_CODEX_SUBSCRIPTION_MUTATION_RESULT_APPLIED ||
		accounts.delete.AccountID != "44444444-4444-4444-8444-444444444444" ||
		accounts.delete.ExpectedDetectedRevision == nil || *accounts.delete.ExpectedDetectedRevision != 2 ||
		accounts.delete.ExpectedManualRevision == nil || *accounts.delete.ExpectedManualRevision != 3 ||
		accounts.delete.ExpectedLinkRevision == nil || *accounts.delete.ExpectedLinkRevision != 4 {
		t.Fatalf("DeleteCodexSubscriptionAccount() = %#v, %v, stub=%#v", deleted, err, accounts.delete)
	}
}

func TestGRPCServerRejectsInvalidCodexSubscriptionTimeZone(t *testing.T) {
	t.Parallel()

	business, err := core.NewService(core.ServiceConfig{
		UsageCost: &helperUsageQueryStub{}, InvocationUsage: &helperInvocationQueryStub{},
		PricingCatalog:     helperPricingCatalogQueryStub{},
		RuntimeInfo:        helperRuntimeQueryStub{},
		CodexSubscriptions: &helperCodexSubscriptionStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, authorize := startConfiguredTestGRPCServer(t, func(config *ServerConfig) {
		config.Service = business
	})
	_, err = client.ListCodexSubscriptionAccounts(
		authorize(t.Context()),
		&corev1.CodexSubscriptionAccountsRequest{EvaluatedAtMs: 1, TimeZone: "Local"},
	)
	grpcStatus, ok := status.FromError(err)
	if !ok || grpcStatus.Code() != codes.InvalidArgument {
		t.Fatalf("ListCodexSubscriptionAccounts(invalid zone) error = %v", err)
	}
	details := grpcStatus.Details()
	if len(details) != 1 {
		t.Fatalf("details = %#v", details)
	}
	detail, ok := details[0].(*corev1.ErrorDetail)
	if !ok || detail.Field == nil || *detail.Field != "timeZone" ||
		strings.Contains(grpcStatus.Message(), "Local") {
		t.Fatalf("validation detail = %#v message=%q", details[0], grpcStatus.Message())
	}
}

func TestGRPCServerKeepsCodexSubscriptionConflictContentFree(t *testing.T) {
	t.Parallel()

	reason := subscriptionaccounts.ReasonRevisionChanged
	accounts := &helperCodexSubscriptionStub{
		mutation: core.CodexSubscriptionMutation{
			Result: string(subscriptionaccounts.MutationConflict),
			Reason: &reason,
		},
	}
	business, err := core.NewService(core.ServiceConfig{
		UsageCost: &helperUsageQueryStub{}, InvocationUsage: &helperInvocationQueryStub{},
		PricingCatalog:     helperPricingCatalogQueryStub{},
		RuntimeInfo:        helperRuntimeQueryStub{},
		CodexSubscriptions: accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, authorize := startConfiguredTestGRPCServer(t, func(config *ServerConfig) {
		config.Service = business
	})
	email := "secret-user@example.com"
	create, err := client.CreateCodexSubscriptionAccount(
		authorize(t.Context()),
		&corev1.CreateCodexSubscriptionAccountRequest{
			ManualEntryId: "66666666-6666-4666-8666-666666666666",
			Manual:        &corev1.CodexSubscriptionManualFields{Email: &email},
		},
	)
	if err != nil || create.GetResult() != corev1.CodexSubscriptionMutationResult_CODEX_SUBSCRIPTION_MUTATION_RESULT_CONFLICT ||
		create.GetReason() != subscriptionaccounts.ReasonRevisionChanged {
		t.Fatalf("CreateCodexSubscriptionAccount(conflict) = %#v, %v", create, err)
	}
	if strings.Contains(create.String(), email) || strings.Contains(create.GetReason(), "@") {
		t.Fatalf("conflict receipt leaked email: %#v", create)
	}
}

type helperCodexSubscriptionStub struct {
	snapshot subscriptionaccounts.Snapshot
	create   core.CodexSubscriptionCreateRequest
	delete   core.CodexSubscriptionDeleteRequest
	mutation core.CodexSubscriptionMutation
}

func (stub *helperCodexSubscriptionStub) ListCodexSubscriptionAccounts(
	context.Context,
	int64,
	string,
) (subscriptionaccounts.Snapshot, error) {
	return stub.snapshot, nil
}

func (stub *helperCodexSubscriptionStub) CreateCodexSubscriptionAccount(
	_ context.Context,
	request core.CodexSubscriptionCreateRequest,
) (core.CodexSubscriptionMutation, error) {
	stub.create = request
	return stub.mutation, nil
}

func (stub *helperCodexSubscriptionStub) UpdateCodexSubscriptionAccount(
	context.Context,
	core.CodexSubscriptionUpdateRequest,
) (core.CodexSubscriptionMutation, error) {
	return stub.mutation, nil
}

func (stub *helperCodexSubscriptionStub) DeleteCodexSubscriptionAccount(
	_ context.Context,
	request core.CodexSubscriptionDeleteRequest,
) (core.CodexSubscriptionMutation, error) {
	stub.delete = request
	return stub.mutation, nil
}

func (stub *helperCodexSubscriptionStub) LinkCodexSubscriptionAccount(
	context.Context,
	core.CodexSubscriptionLinkRequest,
) (core.CodexSubscriptionMutation, error) {
	return stub.mutation, nil
}

func (stub *helperCodexSubscriptionStub) UnlinkCodexSubscriptionAccount(
	context.Context,
	core.CodexSubscriptionUnlinkRequest,
) (core.CodexSubscriptionMutation, error) {
	return stub.mutation, nil
}
