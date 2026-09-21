package app

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountquota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type codexAccountQuotaRuntime struct {
	service      *accountquota.Service
	repository   *store.Repository
	invalidation queryInvalidationNotifier
}

func newCodexAccountQuotaRuntime(
	repository *store.Repository,
	invalidation queryInvalidationNotifier,
) (*codexAccountQuotaRuntime, error) {
	service, err := accountquota.NewService(repository)
	if err != nil {
		return nil, err
	}
	return &codexAccountQuotaRuntime{
		service: service, repository: repository, invalidation: invalidation,
	}, nil
}

func (runtime *codexAccountQuotaRuntime) ListCodexAccountQuotas(
	ctx context.Context,
	evaluatedAtMS int64,
	timeZone string,
) (accountquota.Snapshot, error) {
	if runtime == nil || runtime.service == nil {
		return accountquota.Snapshot{}, accountquota.ErrInvalidService
	}
	return runtime.service.List(ctx, evaluatedAtMS, timeZone)
}

func (runtime *codexAccountQuotaRuntime) ClearCodexAccountQuotaHistory(
	ctx context.Context,
) (accountquota.ClearReceipt, error) {
	if runtime == nil || runtime.repository == nil {
		return accountquota.ClearReceipt{}, store.ErrInvalidRepository
	}
	purged, err := runtime.repository.ClearHistoricalCodexAccountQuota(ctx)
	if err != nil {
		return accountquota.ClearReceipt{}, err
	}
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationQuotaCodex)
	return accountquota.ClearReceipt{
		AccountCount: purged.AccountCount, WindowCount: purged.WindowCount,
		ObservationCount: purged.ObservationCount, ResetSnapshotCount: purged.ResetSnapshotCount,
	}, nil
}
