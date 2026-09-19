package app

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type codexSubscriptionRuntime struct {
	repository   *store.Repository
	invalidation queryInvalidationNotifier
	now          func() time.Time
}

func newCodexSubscriptionRuntime(
	repository *store.Repository,
	invalidation queryInvalidationNotifier,
) *codexSubscriptionRuntime {
	return &codexSubscriptionRuntime{
		repository:   repository,
		invalidation: invalidation,
		now:          time.Now,
	}
}

func (runtime *codexSubscriptionRuntime) clock() time.Time {
	if runtime == nil || runtime.now == nil {
		return time.Now()
	}
	return runtime.now()
}

func (runtime *codexSubscriptionRuntime) ListCodexSubscriptionAccounts(
	ctx context.Context,
	evaluatedAtMS int64,
	timeZone string,
) (subscriptionaccounts.Snapshot, error) {
	if runtime == nil || runtime.repository == nil {
		return subscriptionaccounts.Snapshot{}, store.ErrInvalidRepository
	}
	records, err := runtime.repository.ListCodexSubscriptionRecords(ctx)
	if err != nil {
		return subscriptionaccounts.Snapshot{}, err
	}
	snapshot, err := subscriptionaccounts.Project(records, evaluatedAtMS, timeZone)
	if err != nil {
		return subscriptionaccounts.Snapshot{}, err
	}
	historyByScope := make(map[string]subscriptionaccounts.LegacyQuotaHistory, len(records.Detected))
	for _, account := range records.Detected {
		status, err := runtime.repository.LegacyQuotaHistoryStatus(ctx, account.AccountScope)
		if err != nil {
			return subscriptionaccounts.Snapshot{}, err
		}
		historyByScope[account.AccountScope] = subscriptionaccounts.LegacyQuotaHistory{
			State:               subscriptionaccounts.LegacyQuotaHistoryState(status.State),
			ObservationCount:    status.ObservationCount,
			CycleCount:          status.CycleCount,
			FirstObservedAtMS:   cloneAppInt64(status.FirstObservedAtMS),
			LastObservedAtMS:    cloneAppInt64(status.LastObservedAtMS),
			AssociationRevision: cloneAppInt64(status.AssociationRevision),
		}
	}
	subscriptionaccounts.AttachLegacyQuotaHistory(&snapshot, historyByScope)
	return snapshot, nil
}

func (runtime *codexSubscriptionRuntime) LinkLegacyQuotaHistory(
	ctx context.Context,
	request core.LegacyQuotaHistoryLinkRequest,
) (store.LegacyQuotaHistoryMutation, error) {
	if runtime == nil || runtime.repository == nil {
		return store.LegacyQuotaHistoryMutation{}, store.ErrInvalidRepository
	}
	mutation, err := runtime.repository.LinkLegacyQuotaHistory(ctx, store.LegacyQuotaHistoryLinkRequest{
		DetectedAccountID:        request.DetectedAccountID,
		ExpectedDetectedRevision: request.ExpectedDetectedRevision,
		NowMS:                    runtime.clock().UnixMilli(),
	})
	if err != nil {
		return store.LegacyQuotaHistoryMutation{}, err
	}
	runtime.notifyLegacyHistoryApplied(ctx, mutation.Result)
	return mutation, nil
}

func (runtime *codexSubscriptionRuntime) UnlinkLegacyQuotaHistory(
	ctx context.Context,
	request core.LegacyQuotaHistoryUnlinkRequest,
) (store.LegacyQuotaHistoryMutation, error) {
	if runtime == nil || runtime.repository == nil {
		return store.LegacyQuotaHistoryMutation{}, store.ErrInvalidRepository
	}
	mutation, err := runtime.repository.UnlinkLegacyQuotaHistory(ctx, store.LegacyQuotaHistoryUnlinkRequest{
		DetectedAccountID:           request.DetectedAccountID,
		ExpectedDetectedRevision:    request.ExpectedDetectedRevision,
		ExpectedAssociationRevision: request.ExpectedAssociationRevision,
	})
	if err != nil {
		return store.LegacyQuotaHistoryMutation{}, err
	}
	runtime.notifyLegacyHistoryApplied(ctx, mutation.Result)
	return mutation, nil
}

func (runtime *codexSubscriptionRuntime) CreateCodexSubscriptionAccount(
	ctx context.Context,
	request core.CodexSubscriptionCreateRequest,
) (core.CodexSubscriptionMutation, error) {
	return runtime.mutate(ctx, func(nowMS int64) (core.CodexSubscriptionMutation, error) {
		request.NowMS = nowMS
		return runtime.repository.CreateCodexSubscriptionManualEntry(ctx, request)
	})
}

func (runtime *codexSubscriptionRuntime) UpdateCodexSubscriptionAccount(
	ctx context.Context,
	request core.CodexSubscriptionUpdateRequest,
) (core.CodexSubscriptionMutation, error) {
	return runtime.mutate(ctx, func(nowMS int64) (core.CodexSubscriptionMutation, error) {
		request.NowMS = nowMS
		return runtime.repository.UpdateCodexSubscriptionManualEntry(ctx, request)
	})
}

func (runtime *codexSubscriptionRuntime) DeleteCodexSubscriptionAccount(
	ctx context.Context,
	request core.CodexSubscriptionDeleteRequest,
) (core.CodexSubscriptionMutation, error) {
	return runtime.mutate(ctx, func(int64) (core.CodexSubscriptionMutation, error) {
		return runtime.repository.DeleteCodexSubscriptionAccount(ctx, request)
	})
}

func (runtime *codexSubscriptionRuntime) LinkCodexSubscriptionAccount(
	ctx context.Context,
	request core.CodexSubscriptionLinkRequest,
) (core.CodexSubscriptionMutation, error) {
	return runtime.mutate(ctx, func(nowMS int64) (core.CodexSubscriptionMutation, error) {
		request.NowMS = nowMS
		return runtime.repository.LinkCodexSubscriptionAccount(ctx, request)
	})
}

func (runtime *codexSubscriptionRuntime) UnlinkCodexSubscriptionAccount(
	ctx context.Context,
	request core.CodexSubscriptionUnlinkRequest,
) (core.CodexSubscriptionMutation, error) {
	return runtime.mutate(ctx, func(nowMS int64) (core.CodexSubscriptionMutation, error) {
		request.NowMS = nowMS
		return runtime.repository.UnlinkCodexSubscriptionAccount(ctx, request)
	})
}

func (runtime *codexSubscriptionRuntime) mutate(
	ctx context.Context,
	operation func(int64) (core.CodexSubscriptionMutation, error),
) (core.CodexSubscriptionMutation, error) {
	if runtime == nil || runtime.repository == nil {
		return core.CodexSubscriptionMutation{}, store.ErrInvalidRepository
	}
	mutation, err := operation(runtime.clock().UnixMilli())
	if err != nil {
		return core.CodexSubscriptionMutation{}, err
	}
	runtime.notifyApplied(ctx, mutation.Result)
	return mutation, nil
}

func (runtime *codexSubscriptionRuntime) notifyApplied(ctx context.Context, result string) {
	if result != string(subscriptionaccounts.MutationApplied) {
		return
	}
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationAccount)
}

func (runtime *codexSubscriptionRuntime) notifyLegacyHistoryApplied(
	ctx context.Context,
	result store.LegacyQuotaHistoryMutationResult,
) {
	if result != store.LegacyQuotaHistoryMutationApplied {
		return
	}
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationAccount)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationQuota)
}

func cloneAppInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
