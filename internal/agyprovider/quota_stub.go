package agyprovider

import (
	"context"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// QuotaCurrent 在 TOO-732 接入 LS RPC 前提供安全的空额度契约占位。
func (service *QueryService) QuotaCurrent(ctx context.Context, at int64) (runtimeinfo.QuotaCurrentResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	return runtimeinfo.QuotaCurrentResponse{
		ProviderContext: contextFor(snapshot),
		Meta:            snapshotMeta(snapshot, nil),
		Current: quotaquery.CurrentResponse{
			Version:       quotaquery.CurrentContractVersion,
			AccountScope:  store.QuotaAccountScopeDefault,
			EvaluatedAtMS: at,
			Windows:       []quotaquery.CurrentWindow{},
			Sources:       []quotaquery.CurrentSource{},
		},
	}, nil
}

// QuotaPace 在 TOO-732 接入 LS RPC 前提供安全的空配额节奏占位。
func (service *QueryService) QuotaPace(ctx context.Context, at int64) (runtimeinfo.QuotaPaceResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	return runtimeinfo.QuotaPaceResponse{
		ProviderContext: contextFor(snapshot),
		Meta:            snapshotMeta(snapshot, nil),
		Pace: quotaquery.PaceResponse{
			Version:       quotaquery.PaceContractVersion,
			AccountScope:  store.QuotaAccountScopeDefault,
			EvaluatedAtMS: at,
			Windows:       []quotaquery.PaceWindow{},
		},
	}, nil
}

// RefreshQuota 在 TOO-732 接入 LS RPC 前返回明确不可用状态。
func (service *QueryService) RefreshQuota(context.Context) error {
	return basequery.NewUnavailableFailure(nil)
}
