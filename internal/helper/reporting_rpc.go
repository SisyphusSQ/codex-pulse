package helper

import (
	"context"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/reporting"
)

func (api *grpcAPI) ReportingStatus(ctx context.Context, _ *corev1.Empty) (*corev1.ReportingStatusResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	out, err := api.service.ReportingStatus(ctx)
	return encodeRPC(out, &corev1.ReportingStatusResponse{}, err)
}
func (api *grpcAPI) PairReporting(ctx context.Context, request *corev1.PairReportingRequest) (*corev1.ReportingStatusResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	out, err := api.service.PairReporting(ctx, reporting.PairRequest{Endpoint: request.GetEndpoint(), AllowHTTP: request.GetAllowHttp(), Code: request.GetCode()})
	return encodeRPC(out, &corev1.ReportingStatusResponse{}, err)
}
func (api *grpcAPI) ConfigureReporting(ctx context.Context, request *corev1.ConfigureReportingRequest) (*corev1.ReportingStatusResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	out, err := api.service.ConfigureReporting(ctx, reporting.ConfigureRequest{Enabled: request.GetEnabled(), IntervalSeconds: request.GetIntervalSeconds(), HistoryStartAtMS: request.GetHistoryStartAtMs(), ClearPending: request.GetClearPending()})
	return encodeRPC(out, &corev1.ReportingStatusResponse{}, err)
}
func (api *grpcAPI) SyncReportingNow(ctx context.Context, _ *corev1.Empty) (*corev1.ReportingStatusResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	out, err := api.service.SyncReportingNow(ctx)
	return encodeRPC(out, &corev1.ReportingStatusResponse{}, err)
}

func (api *grpcAPI) FullSyncReporting(ctx context.Context, _ *corev1.Empty) (*corev1.ReportingStatusResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	out, err := api.service.FullSyncReporting(ctx)
	return encodeRPC(out, &corev1.ReportingStatusResponse{}, err)
}
