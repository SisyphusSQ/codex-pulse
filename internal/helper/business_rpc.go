package helper

import (
	"context"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (api *grpcAPI) Contracts(ctx context.Context, _ *corev1.ContractsRequest) (*corev1.ContractsResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	contract := api.service.Contracts()
	methods := make([]*corev1.MethodInfo, 0, len(contract.Methods))
	for _, method := range contract.Methods {
		methods = append(methods, &corev1.MethodInfo{Name: method.Name, Kind: string(method.Kind)})
	}
	detail := &corev1.ErrorDetail{
		Code: string(contract.ErrorExample.Error.Code), MessageKey: contract.ErrorExample.Error.MessageKey,
		Retryable: contract.ErrorExample.Error.Retryable,
	}
	if contract.ErrorExample.Error.Field != nil {
		field := *contract.ErrorExample.Error.Field
		detail.Field = &field
	}
	return &corev1.ContractsResponse{
		Version: contract.Version, QueryVersion: contract.QueryVersion,
		UsageCostVersion: contract.UsageCostVersion, RuntimeInfoVersion: contract.RuntimeInfoVersion,
		Methods: methods, CommandMethods: append([]string(nil), contract.CommandMethods...), ErrorExample: detail,
	}, nil
}

func (api *grpcAPI) UsageCost(
	ctx context.Context,
	request *corev1.UsageCostRequest,
) (*corev1.UsageCostResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.UsageCost(ctx, usagecost.UsageCostRequest{
		Range: fromProtoDateRange(request.GetRange()), Granularity: usagecost.TrendGranularity(request.GetGranularity()),
	})
	return encodeRPC(response, &corev1.UsageCostResponse{}, err)
}

func (api *grpcAPI) SessionDetail(
	ctx context.Context,
	request *corev1.SessionDetailRequest,
) (*corev1.SessionDetailResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	query := usagecost.SessionDetailRequest{
		SessionID: request.GetSessionId(), TurnPage: fromProtoPage(request.GetTurnPage()),
	}
	if request != nil && request.ReportingTimezone != nil {
		timezone := request.GetReportingTimezone()
		query.ReportingTimezone = &timezone
	}
	response, err := api.service.SessionDetail(ctx, query)
	return encodeRPC(response, &corev1.SessionDetailResponse{}, err)
}

func (api *grpcAPI) ListProjects(
	ctx context.Context,
	request *corev1.ListProjectsRequest,
) (*corev1.ProjectListResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListProjects(ctx, fromProtoQuery(request.GetQuery()))
	return encodeRPC(response, &corev1.ProjectListResponse{}, err)
}

func (api *grpcAPI) ProjectDetail(
	ctx context.Context,
	request *corev1.ProjectDetailRequest,
) (*corev1.ProjectDetailResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	query := usagecost.ProjectDetailRequest{
		DimensionKey: request.GetDimensionKey(), Range: fromProtoDateRange(request.GetRange()),
		SessionPage: fromProtoPage(request.GetSessionPage()), ModelPage: fromProtoPage(request.GetModelPage()),
	}
	response, err := api.service.ProjectDetail(ctx, query)
	return encodeRPC(response, &corev1.ProjectDetailResponse{}, err)
}

func (api *grpcAPI) QuotaCurrent(
	ctx context.Context,
	request *corev1.QuotaCurrentRequest,
) (*corev1.QuotaCurrentResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.QuotaCurrent(ctx, request.GetEvaluatedAtMs())
	return encodeRPC(response, &corev1.QuotaCurrentResponse{}, err)
}

func (api *grpcAPI) RequestQuotaRefresh(
	ctx context.Context,
	request *corev1.QuotaRefreshRequest,
) (*corev1.QuotaRefreshReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.RequestQuotaRefresh(ctx, quotaonline.RefreshSource(request.GetSource()))
	return encodeRPC(response, &corev1.QuotaRefreshReceipt{}, err)
}

func (api *grpcAPI) ListSources(
	ctx context.Context,
	request *corev1.ListSourcesRequest,
) (*corev1.SourceListResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListSources(ctx, fromProtoQuery(request.GetQuery()))
	return encodeRPC(response, &corev1.SourceListResponse{}, err)
}

func (api *grpcAPI) Source(
	ctx context.Context,
	request *corev1.SourceRequest,
) (*corev1.SourceDetailResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.Source(ctx, runtimeinfo.SourceDetailRequest{SourceKey: request.GetSourceKey()})
	return encodeRPC(response, &corev1.SourceDetailResponse{}, err)
}

func (api *grpcAPI) ListJobs(
	ctx context.Context,
	request *corev1.ListJobsRequest,
) (*corev1.JobListResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListJobs(ctx, fromProtoQuery(request.GetQuery()))
	return encodeRPC(response, &corev1.JobListResponse{}, err)
}

func (api *grpcAPI) Job(
	ctx context.Context,
	request *corev1.JobRequest,
) (*corev1.JobDetailResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.Job(ctx, runtimeinfo.JobDetailRequest{JobID: request.GetJobId()})
	return encodeRPC(response, &corev1.JobDetailResponse{}, err)
}

func (api *grpcAPI) ListHealth(
	ctx context.Context,
	request *corev1.ListHealthRequest,
) (*corev1.HealthListResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ListHealth(ctx, fromProtoQuery(request.GetQuery()))
	return encodeRPC(response, &corev1.HealthListResponse{}, err)
}

func (api *grpcAPI) Health(
	ctx context.Context,
	request *corev1.HealthRequest,
) (*corev1.HealthDetailResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.Health(ctx, runtimeinfo.HealthDetailRequest{EventID: request.GetEventId()})
	return encodeRPC(response, &corev1.HealthDetailResponse{}, err)
}

func (api *grpcAPI) HealthProjection(
	ctx context.Context,
	_ *corev1.HealthProjectionRequest,
) (*corev1.HealthProjectionResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.HealthProjection(ctx)
	return encodeRPC(response, &corev1.HealthProjectionResponse{}, err)
}

func (api *grpcAPI) DataHealth(
	ctx context.Context,
	request *corev1.DataHealthRequest,
) (*corev1.DataHealthResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.DataHealth(ctx, request.GetEvaluatedAtMs())
	return encodeRPC(response, &corev1.DataHealthResponse{}, err)
}

func (api *grpcAPI) Settings(
	ctx context.Context,
	_ *corev1.SettingsRequest,
) (*corev1.SettingsResponse, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.Settings(ctx)
	return encodeRPC(response, &corev1.SettingsResponse{}, err)
}

func (api *grpcAPI) UpdateSettings(
	ctx context.Context,
	request *corev1.UpdateSettingsRequest,
) (*corev1.SettingsUpdateReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.UpdateSettings(ctx, core.SettingsUpdateRequest{
		ExpectedRevision: request.GetExpectedRevision(),
		Online: core.SettingsOnlineUpdate{
			QuotaEnabled:        request.GetOnline().GetQuotaEnabled(),
			ResetCreditsEnabled: request.GetOnline().GetResetCreditsEnabled(),
		},
		Refresh: core.SettingsRefreshUpdate{
			QuotaIntervalSeconds:        request.GetRefresh().GetQuotaIntervalSeconds(),
			ResetCreditsIntervalSeconds: request.GetRefresh().GetResetCreditsIntervalSeconds(),
			ReconcileIntervalSeconds:    request.GetRefresh().GetReconcileIntervalSeconds(),
			JSONLDebounceMilliseconds:   request.GetRefresh().GetJsonlDebounceMilliseconds(),
		},
		Updates: core.SettingsUpdatesUpdate{
			AutoCheckEnabled:     request.GetUpdates().GetAutoCheckEnabled(),
			CheckIntervalSeconds: request.GetUpdates().GetCheckIntervalSeconds(),
		},
		UI: core.SettingsUIUpdate{
			LaunchBehavior: request.GetUi().GetLaunchBehavior(), OverviewRange: request.GetUi().GetOverviewRange(),
		},
	})
	return encodeRPC(response, &corev1.SettingsUpdateReceipt{}, err)
}

func (api *grpcAPI) PlanHomeSwitch(
	ctx context.Context,
	request *corev1.PlanHomeSwitchRequest,
) (*corev1.HomeSwitchPlanReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.PlanHomeSwitch(ctx, core.HomeSwitchPlanRequest{
		TargetPath: request.GetTargetPath(), Strategy: core.HomeSwitchStrategy(request.GetStrategy()),
	})
	return encodeRPC(response, &corev1.HomeSwitchPlanReceipt{}, err)
}

func (api *grpcAPI) ConfirmHomeSwitch(
	ctx context.Context,
	_ *corev1.ConfirmHomeSwitchRequest,
) (*corev1.HomeSwitchReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.ConfirmHomeSwitch(ctx)
	return encodeRPC(response, &corev1.HomeSwitchReceipt{}, err)
}

func (api *grpcAPI) RecoverHomeSwitch(
	ctx context.Context,
	_ *corev1.RecoverHomeSwitchRequest,
) (*corev1.HomeSwitchReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.RecoverHomeSwitch(ctx)
	return encodeRPC(response, &corev1.HomeSwitchReceipt{}, err)
}

func (api *grpcAPI) RunRuntimeAction(
	ctx context.Context,
	request *corev1.RuntimeActionRequest,
) (*corev1.RuntimeActionReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.RunRuntimeAction(ctx, core.RuntimeAction(request.GetAction()))
	return encodeRPC(response, &corev1.RuntimeActionReceipt{}, err)
}

func (api *grpcAPI) AnalyzeSessionIndexRepair(
	ctx context.Context,
	_ *corev1.AnalyzeSessionIndexRepairRequest,
) (*corev1.RepairDryRunReceipt, error) {
	if api == nil || api.service == nil {
		return nil, coreServiceUnavailable()
	}
	response, err := api.service.AnalyzeSessionIndexRepair(ctx)
	return encodeRPC(response, &corev1.RepairDryRunReceipt{}, err)
}

func encodeRPC[T proto.Message](source any, target T, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, toGRPCError(err)
	}
	if err := core.EncodeResponse(source, target); err != nil {
		var zero T
		return zero, toGRPCError(err)
	}
	return target, nil
}

func fromProtoPage(page *corev1.PageRequest) basequery.PageRequest {
	if page == nil {
		return basequery.PageRequest{}
	}
	result := basequery.PageRequest{Limit: int(page.Limit)}
	if page.Cursor != nil {
		cursor := page.GetCursor()
		result.Cursor = &cursor
	}
	return result
}

func fromProtoDateRange(dateRange *corev1.LocalDateRange) basequery.LocalDateRange {
	if dateRange == nil {
		return basequery.LocalDateRange{}
	}
	return basequery.LocalDateRange{
		StartDate: dateRange.StartDate, EndDateExclusive: dateRange.EndDateExclusive, TimeZone: dateRange.TimeZone,
	}
}

func coreServiceUnavailable() error {
	return status.Error(codes.FailedPrecondition, "core service unavailable")
}
