package core

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type AccountSnapshotQuery struct {
	Scope         agentprovider.Scope
	EvaluatedAtMS int64
	TimeZone      string
}

type CodexSubscriptionCreateRequest = subscriptionaccounts.CreateRequest
type CodexSubscriptionUpdateRequest = subscriptionaccounts.UpdateRequest
type CodexSubscriptionDeleteRequest = subscriptionaccounts.DeleteRequest
type CodexSubscriptionLinkRequest = subscriptionaccounts.LinkRequest
type CodexSubscriptionUnlinkRequest = subscriptionaccounts.UnlinkRequest

type CodexSubscriptionMutation = subscriptionaccounts.Mutation

type LegacyQuotaHistoryLinkRequest struct {
	DetectedAccountID        string
	ExpectedDetectedRevision int64
}

type LegacyQuotaHistoryUnlinkRequest struct {
	DetectedAccountID           string
	ExpectedDetectedRevision    int64
	ExpectedAssociationRevision int64
}

type CodexSubscriptionAccounts interface {
	ListCodexSubscriptionAccounts(context.Context, int64, string) (subscriptionaccounts.Snapshot, error)
	CreateCodexSubscriptionAccount(context.Context, CodexSubscriptionCreateRequest) (CodexSubscriptionMutation, error)
	UpdateCodexSubscriptionAccount(context.Context, CodexSubscriptionUpdateRequest) (CodexSubscriptionMutation, error)
	DeleteCodexSubscriptionAccount(context.Context, CodexSubscriptionDeleteRequest) (CodexSubscriptionMutation, error)
	LinkCodexSubscriptionAccount(context.Context, CodexSubscriptionLinkRequest) (CodexSubscriptionMutation, error)
	UnlinkCodexSubscriptionAccount(context.Context, CodexSubscriptionUnlinkRequest) (CodexSubscriptionMutation, error)
	LinkLegacyQuotaHistory(context.Context, LegacyQuotaHistoryLinkRequest) (store.LegacyQuotaHistoryMutation, error)
	UnlinkLegacyQuotaHistory(context.Context, LegacyQuotaHistoryUnlinkRequest) (store.LegacyQuotaHistoryMutation, error)
}

func (service *Service) LinkLegacyQuotaHistory(
	ctx context.Context,
	request LegacyQuotaHistoryLinkRequest,
) (LegacyQuotaHistoryMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.DetectedAccountID); err != nil {
		return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("detectedAccountId", err),
		)
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedDetectedRevision); err != nil {
		return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("expectedDetectedRevision", err),
		)
	}
	return service.legacyQuotaHistoryMutation(func() (store.LegacyQuotaHistoryMutation, error) {
		return service.codexSubscriptions.LinkLegacyQuotaHistory(ctx, request)
	})
}

func (service *Service) UnlinkLegacyQuotaHistory(
	ctx context.Context,
	request LegacyQuotaHistoryUnlinkRequest,
) (LegacyQuotaHistoryMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.DetectedAccountID); err != nil {
		return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("detectedAccountId", err),
		)
	}
	for field, revision := range map[string]int64{
		"expectedDetectedRevision":    request.ExpectedDetectedRevision,
		"expectedAssociationRevision": request.ExpectedAssociationRevision,
	} {
		if err := subscriptionaccounts.ParseRevision(revision); err != nil {
			return LegacyQuotaHistoryMutationReceipt{}, newServiceFailure(
				basequery.NewValidationFailure(field, err),
			)
		}
	}
	return service.legacyQuotaHistoryMutation(func() (store.LegacyQuotaHistoryMutation, error) {
		return service.codexSubscriptions.UnlinkLegacyQuotaHistory(ctx, request)
	})
}

func (service *Service) legacyQuotaHistoryMutation(
	operation func() (store.LegacyQuotaHistoryMutation, error),
) (LegacyQuotaHistoryMutationReceipt, error) {
	return serviceQueryCall(service, func() (LegacyQuotaHistoryMutationReceipt, error) {
		mutation, err := operation()
		if err != nil {
			return LegacyQuotaHistoryMutationReceipt{}, mapCodexSubscriptionError(err)
		}
		return encodeLegacyQuotaHistoryMutation(mutation)
	})
}

func (service *Service) ListCodexSubscriptionAccounts(
	ctx context.Context,
	evaluatedAtMS int64,
	timeZone string,
) (CodexSubscriptionAccountsView, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionAccountsView{}, newServiceFailure(ErrService)
	}
	if err := validateSubscriptionEvaluation(evaluatedAtMS, timeZone); err != nil {
		return CodexSubscriptionAccountsView{}, newServiceFailure(err)
	}
	return serviceQueryCall(service, func() (CodexSubscriptionAccountsView, error) {
		snapshot, err := service.codexSubscriptions.ListCodexSubscriptionAccounts(ctx, evaluatedAtMS, timeZone)
		if err != nil {
			return CodexSubscriptionAccountsView{}, mapCodexSubscriptionError(err)
		}
		view, err := encodeCodexSubscriptionAccounts(snapshot)
		if err != nil {
			return CodexSubscriptionAccountsView{}, err
		}
		return view, nil
	})
}

func (service *Service) CreateCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionCreateRequest,
) (CodexSubscriptionMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.ManualEntryID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("manualEntryId", err),
		)
	}
	return service.codexSubscriptionMutation(func() (CodexSubscriptionMutation, error) {
		return service.codexSubscriptions.CreateCodexSubscriptionAccount(ctx, request)
	})
}

func (service *Service) UpdateCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionUpdateRequest,
) (CodexSubscriptionMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.AccountID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("accountId", err),
		)
	}
	if request.NewManualEntryID != nil {
		if _, err := subscriptionaccounts.ParsePublicID(*request.NewManualEntryID); err != nil {
			return CodexSubscriptionMutationReceipt{}, newServiceFailure(
				basequery.NewValidationFailure("newManualEntryId", err),
			)
		}
	}
	if request.ExpectedManualRevision != nil {
		if err := subscriptionaccounts.ParseRevision(*request.ExpectedManualRevision); err != nil {
			return CodexSubscriptionMutationReceipt{}, newServiceFailure(
				basequery.NewValidationFailure("expectedManualRevision", err),
			)
		}
	}
	if request.ExpectedLinkRevision != nil {
		if err := subscriptionaccounts.ParseRevision(*request.ExpectedLinkRevision); err != nil {
			return CodexSubscriptionMutationReceipt{}, newServiceFailure(
				basequery.NewValidationFailure("expectedLinkRevision", err),
			)
		}
	}
	return service.codexSubscriptionMutation(func() (CodexSubscriptionMutation, error) {
		return service.codexSubscriptions.UpdateCodexSubscriptionAccount(ctx, request)
	})
}

func (service *Service) DeleteCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionDeleteRequest,
) (CodexSubscriptionMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.AccountID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("accountId", err),
		)
	}
	for field, revision := range map[string]*int64{
		"expectedDetectedRevision": request.ExpectedDetectedRevision,
		"expectedManualRevision":   request.ExpectedManualRevision,
		"expectedLinkRevision":     request.ExpectedLinkRevision,
	} {
		if revision != nil {
			if err := subscriptionaccounts.ParseRevision(*revision); err != nil {
				return CodexSubscriptionMutationReceipt{}, newServiceFailure(
					basequery.NewValidationFailure(field, err),
				)
			}
		}
	}
	return service.codexSubscriptionMutation(func() (CodexSubscriptionMutation, error) {
		return service.codexSubscriptions.DeleteCodexSubscriptionAccount(ctx, request)
	})
}

func (service *Service) LinkCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionLinkRequest,
) (CodexSubscriptionMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.DetectedAccountID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("detectedAccountId", err),
		)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.ManualEntryID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("manualEntryId", err),
		)
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedManualRevision); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("expectedManualRevision", err),
		)
	}
	return service.codexSubscriptionMutation(func() (CodexSubscriptionMutation, error) {
		return service.codexSubscriptions.LinkCodexSubscriptionAccount(ctx, request)
	})
}

func (service *Service) UnlinkCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionUnlinkRequest,
) (CodexSubscriptionMutationReceipt, error) {
	if service == nil || service.codexSubscriptions == nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(ErrService)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.DetectedAccountID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("detectedAccountId", err),
		)
	}
	if _, err := subscriptionaccounts.ParsePublicID(request.ManualEntryID); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("manualEntryId", err),
		)
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedManualRevision); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("expectedManualRevision", err),
		)
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedLinkRevision); err != nil {
		return CodexSubscriptionMutationReceipt{}, newServiceFailure(
			basequery.NewValidationFailure("expectedLinkRevision", err),
		)
	}
	return service.codexSubscriptionMutation(func() (CodexSubscriptionMutation, error) {
		return service.codexSubscriptions.UnlinkCodexSubscriptionAccount(ctx, request)
	})
}

func (service *Service) codexSubscriptionMutation(
	operation func() (CodexSubscriptionMutation, error),
) (CodexSubscriptionMutationReceipt, error) {
	return serviceQueryCall(service, func() (CodexSubscriptionMutationReceipt, error) {
		mutation, err := operation()
		if err != nil {
			return CodexSubscriptionMutationReceipt{}, mapCodexSubscriptionError(err)
		}
		return encodeCodexSubscriptionMutation(mutation)
	})
}

func validateSubscriptionEvaluation(evaluatedAtMS int64, timeZone string) error {
	if err := subscriptionaccounts.ParseEvaluationTime(evaluatedAtMS); err != nil {
		return basequery.NewValidationFailure("evaluatedAtMS", err)
	}
	if _, err := subscriptionaccounts.LoadTimeZone(timeZone); err != nil {
		return basequery.NewValidationFailure("timeZone", err)
	}
	return nil
}

func mapCodexSubscriptionError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, subscriptionaccounts.ErrInvalidEvaluationTime):
		return basequery.NewValidationFailure("evaluatedAtMS", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidTimeZone):
		return basequery.NewValidationFailure("timeZone", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidEmail),
		errors.Is(err, subscriptionaccounts.ErrStandaloneEmailRequired):
		return basequery.NewValidationFailure("manual.email", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidAlias):
		return basequery.NewValidationFailure("manual.alias", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidPlan):
		return basequery.NewValidationFailure("manual.plan", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidMembershipDate),
		errors.Is(err, subscriptionaccounts.ErrInvalidDatePair):
		return basequery.NewValidationFailure("manual.membershipDate", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidDateKind):
		return basequery.NewValidationFailure("manual.dateKind", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidUUID):
		return basequery.NewValidationFailure("accountId", err)
	case errors.Is(err, subscriptionaccounts.ErrInvalidRevision):
		return basequery.NewValidationFailure("expectedManualRevision", err)
	case errors.Is(err, store.ErrInvalidRecord):
		return basequery.NewValidationFailure("accountId", err)
	case errors.Is(err, store.ErrInvalidRepository):
		return basequery.NewUnavailableFailure(err)
	default:
		return err
	}
}
