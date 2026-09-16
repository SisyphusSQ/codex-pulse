package helper

import (
	"context"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func (api *grpcAPI) ListCodexSubscriptionAccounts(
	ctx context.Context,
	request *corev1.CodexSubscriptionAccountsRequest,
) (*corev1.CodexSubscriptionAccountsResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListCodexSubscriptionAccounts(
		ctx,
		request.GetEvaluatedAtMs(),
		request.GetTimeZone(),
	)
	return encodeRPC(response, &corev1.CodexSubscriptionAccountsResponse{}, err)
}

func (api *grpcAPI) CreateCodexSubscriptionAccount(
	ctx context.Context,
	request *corev1.CreateCodexSubscriptionAccountRequest,
) (*corev1.CodexSubscriptionMutationReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	fields, err := fromProtoManualFields(request.GetManual())
	if err != nil {
		return nil, toGRPCError(err)
	}
	response, err := api.service.CreateCodexSubscriptionAccount(ctx, core.CodexSubscriptionCreateRequest{
		ManualEntryID: request.GetManualEntryId(),
		Fields:        fields,
	})
	return encodeRPC(response, &corev1.CodexSubscriptionMutationReceipt{}, err)
}

func (api *grpcAPI) UpdateCodexSubscriptionAccount(
	ctx context.Context,
	request *corev1.UpdateCodexSubscriptionAccountRequest,
) (*corev1.CodexSubscriptionMutationReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	fields, err := fromProtoManualFields(request.GetManual())
	if err != nil {
		return nil, toGRPCError(err)
	}
	update := core.CodexSubscriptionUpdateRequest{
		AccountID:              request.GetAccountId(),
		Fields:                 fields,
		NewManualEntryID:       cloneProtoValue(request.NewManualEntryId),
		ExpectedManualRevision: cloneProtoValue(request.ExpectedManualRevision),
		ExpectedLinkRevision:   cloneProtoValue(request.ExpectedLinkRevision),
	}
	response, err := api.service.UpdateCodexSubscriptionAccount(ctx, update)
	return encodeRPC(response, &corev1.CodexSubscriptionMutationReceipt{}, err)
}

func (api *grpcAPI) DeleteCodexSubscriptionAccount(
	ctx context.Context,
	request *corev1.DeleteCodexSubscriptionAccountRequest,
) (*corev1.CodexSubscriptionMutationReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.DeleteCodexSubscriptionAccount(ctx, core.CodexSubscriptionDeleteRequest{
		AccountID:                request.GetAccountId(),
		ExpectedDetectedRevision: cloneProtoValue(request.ExpectedDetectedRevision),
		ExpectedManualRevision:   cloneProtoValue(request.ExpectedManualRevision),
		ExpectedLinkRevision:     cloneProtoValue(request.ExpectedLinkRevision),
	})
	return encodeRPC(response, &corev1.CodexSubscriptionMutationReceipt{}, err)
}

func (api *grpcAPI) LinkCodexSubscriptionAccount(
	ctx context.Context,
	request *corev1.LinkCodexSubscriptionAccountRequest,
) (*corev1.CodexSubscriptionMutationReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.LinkCodexSubscriptionAccount(ctx, core.CodexSubscriptionLinkRequest{
		DetectedAccountID:      request.GetDetectedAccountId(),
		ManualEntryID:          request.GetManualEntryId(),
		ExpectedManualRevision: request.GetExpectedManualRevision(),
	})
	return encodeRPC(response, &corev1.CodexSubscriptionMutationReceipt{}, err)
}

func (api *grpcAPI) UnlinkCodexSubscriptionAccount(
	ctx context.Context,
	request *corev1.UnlinkCodexSubscriptionAccountRequest,
) (*corev1.CodexSubscriptionMutationReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.UnlinkCodexSubscriptionAccount(ctx, core.CodexSubscriptionUnlinkRequest{
		DetectedAccountID:      request.GetDetectedAccountId(),
		ManualEntryID:          request.GetManualEntryId(),
		ExpectedManualRevision: request.GetExpectedManualRevision(),
		ExpectedLinkRevision:   request.GetExpectedLinkRevision(),
	})
	return encodeRPC(response, &corev1.CodexSubscriptionMutationReceipt{}, err)
}

func fromProtoManualFields(fields *corev1.CodexSubscriptionManualFields) (subscriptionaccounts.ManualFields, error) {
	if fields == nil {
		return subscriptionaccounts.ManualFields{}, basequery.NewValidationFailure("manual", nil)
	}
	result := subscriptionaccounts.ManualFields{
		Email:          cloneProtoValue(fields.Email),
		Alias:          cloneProtoValue(fields.Alias),
		MembershipDate: cloneProtoValue(fields.MembershipDate),
	}
	if fields.Plan != nil {
		plan, err := fromProtoSubscriptionPlan(*fields.Plan)
		if err != nil {
			return subscriptionaccounts.ManualFields{}, err
		}
		result.Plan = plan
	}
	if fields.DateKind != nil {
		kind, err := fromProtoSubscriptionDateKind(*fields.DateKind)
		if err != nil {
			return subscriptionaccounts.ManualFields{}, err
		}
		result.DateKind = kind
	}
	return result, nil
}

func fromProtoSubscriptionPlan(value corev1.CodexSubscriptionPlan) (*subscriptionaccounts.Plan, error) {
	switch value {
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_FREE:
		return planPointer(subscriptionaccounts.PlanFree), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_GO:
		return planPointer(subscriptionaccounts.PlanGo), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_PLUS:
		return planPointer(subscriptionaccounts.PlanPlus), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_PRO_5X:
		return planPointer(subscriptionaccounts.PlanPro5X), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_PRO_20X:
		return planPointer(subscriptionaccounts.PlanPro20X), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_TEAM:
		return planPointer(subscriptionaccounts.PlanTeam), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_BUSINESS:
		return planPointer(subscriptionaccounts.PlanBusiness), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_ENTERPRISE:
		return planPointer(subscriptionaccounts.PlanEnterprise), nil
	case corev1.CodexSubscriptionPlan_CODEX_SUBSCRIPTION_PLAN_EDU:
		return planPointer(subscriptionaccounts.PlanEdu), nil
	default:
		return nil, basequery.NewValidationFailure("manual.plan", nil)
	}
}

func fromProtoSubscriptionDateKind(value corev1.CodexSubscriptionDateKind) (*subscriptionaccounts.DateKind, error) {
	switch value {
	case corev1.CodexSubscriptionDateKind_CODEX_SUBSCRIPTION_DATE_KIND_NEXT_RENEWAL:
		kind := subscriptionaccounts.DateKindNextRenewal
		return &kind, nil
	case corev1.CodexSubscriptionDateKind_CODEX_SUBSCRIPTION_DATE_KIND_MEMBERSHIP_EXPIRY:
		kind := subscriptionaccounts.DateKindMembershipExpiry
		return &kind, nil
	default:
		return nil, basequery.NewValidationFailure("manual.dateKind", nil)
	}
}

func planPointer(value subscriptionaccounts.Plan) *subscriptionaccounts.Plan {
	return cloneProtoValue(&value)
}

func cloneProtoValue[Value any](value *Value) *Value {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
