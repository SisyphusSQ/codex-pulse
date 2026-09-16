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
	return subscriptionaccounts.Project(records, evaluatedAtMS, timeZone)
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
