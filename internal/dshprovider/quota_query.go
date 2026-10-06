package dshprovider

import (
	"context"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// DSH logs contain usage, not a subscription quota or account balance.
func (service *QueryService) QuotaCurrent(ctx context.Context, at int64) (runtimeinfo.QuotaCurrentResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	return runtimeinfo.QuotaCurrentResponse{ProviderContext: contextFor(snapshot), Meta: snapshotMeta(snapshot, nil), Current: quotaquery.CurrentResponse{Version: quotaquery.CurrentContractVersion, AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: at, Windows: []quotaquery.CurrentWindow{}, Sources: []quotaquery.CurrentSource{}}}, nil
}
func (service *QueryService) QuotaPace(ctx context.Context, at int64) (runtimeinfo.QuotaPaceResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	return runtimeinfo.QuotaPaceResponse{ProviderContext: contextFor(snapshot), Meta: snapshotMeta(snapshot, nil), Pace: quotaquery.PaceResponse{Version: quotaquery.PaceContractVersion, AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: at, Windows: []quotaquery.PaceWindow{}}}, nil
}
