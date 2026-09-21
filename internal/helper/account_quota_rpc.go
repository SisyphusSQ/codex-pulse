package helper

import (
	"context"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
)

func (api *grpcAPI) ListCodexAccountQuotas(
	ctx context.Context,
	request *corev1.CodexAccountQuotasRequest,
) (*corev1.CodexAccountQuotasResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListCodexAccountQuotas(
		ctx, request.GetEvaluatedAtMs(), request.GetTimeZone(),
	)
	return encodeRPC(response, &corev1.CodexAccountQuotasResponse{}, err)
}

func (api *grpcAPI) ClearCodexAccountQuotaHistory(
	ctx context.Context,
	_ *corev1.ClearCodexAccountQuotaHistoryRequest,
) (*corev1.CodexAccountQuotaHistoryClearReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ClearCodexAccountQuotaHistory(ctx)
	return encodeRPC(response, &corev1.CodexAccountQuotaHistoryClearReceipt{}, err)
}
