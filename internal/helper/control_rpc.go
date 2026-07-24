package helper

import (
	"context"
	"runtime"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func (api *grpcAPI) Bootstrap(
	ctx context.Context,
	_ *corev1.BootstrapRequest,
) (*corev1.BootstrapResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	response := &corev1.BootstrapResponse{
		Name: "Codex Pulse", Locale: "zh-CN", Platform: runtime.GOOS, Mode: "normal",
	}
	if api != nil && api.recovery != nil {
		snapshot, err := api.recovery.State(ctx)
		if err != nil {
			return nil, toGRPCError(err)
		}
		mapped, err := encodeRPC(snapshot, &corev1.MigrationRecoverySnapshot{}, nil)
		if err != nil {
			return nil, err
		}
		response.Mode = "recovery"
		response.Recovery = mapped
	}
	return response, nil
}

func (api *grpcAPI) NotifyLifecycle(
	ctx context.Context,
	request *corev1.LifecycleNotificationRequest,
) (*corev1.LifecycleNotificationReceipt, error) {
	if api == nil || api.lifecycle == nil {
		return nil, status.Error(codes.FailedPrecondition, "lifecycle service unavailable")
	}
	event := request.GetEvent()
	if !validLifecycleEvent(event) {
		return nil, status.Error(codes.InvalidArgument, "invalid lifecycle event")
	}
	if err := api.lifecycle.NotifyLifecycle(ctx, event); err != nil {
		return nil, toGRPCError(err)
	}
	return &corev1.LifecycleNotificationReceipt{Event: event, Accepted: true}, nil
}

func (api *grpcAPI) MigrationRecoveryState(
	ctx context.Context,
	_ *corev1.MigrationRecoveryStateRequest,
) (*corev1.MigrationRecoverySnapshot, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	response, err := api.recovery.State(ctx)
	return encodeRPC(response, &corev1.MigrationRecoverySnapshot{}, err)
}

func (api *grpcAPI) MigrationRecoveryRetry(
	ctx context.Context,
	_ *corev1.MigrationRecoveryRetryRequest,
) (*corev1.MigrationRecoveryReceipt, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	response, err := api.recovery.Retry(ctx)
	return encodeRPC(response, &corev1.MigrationRecoveryReceipt{}, err)
}

func (api *grpcAPI) MigrationRecoveryPrepare(
	ctx context.Context,
	request *corev1.MigrationRecoveryPrepareRequest,
) (*corev1.MigrationRestoreConfirmation, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	response, err := api.recovery.Prepare(ctx, request.GetBackupName())
	return encodeRPC(response, &corev1.MigrationRestoreConfirmation{}, err)
}

func (api *grpcAPI) MigrationRecoveryConfirm(
	ctx context.Context,
	request *corev1.MigrationRecoveryConfirmRequest,
) (*corev1.MigrationRecoveryReceipt, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	response, err := api.recovery.Confirm(ctx, request.GetConfirmationToken())
	return encodeRPC(response, &corev1.MigrationRecoveryReceipt{}, err)
}

func (api *grpcAPI) MigrationRecoveryCancel(
	ctx context.Context,
	_ *corev1.MigrationRecoveryCancelRequest,
) (*corev1.Empty, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	if err := api.recovery.Cancel(ctx); err != nil {
		return nil, toGRPCError(err)
	}
	return &corev1.Empty{}, nil
}

func (api *grpcAPI) MigrationRecoveryExit(
	ctx context.Context,
	_ *corev1.MigrationRecoveryExitRequest,
) (*corev1.Empty, error) {
	if api == nil || api.recovery == nil {
		return nil, recoveryUnavailable()
	}
	if err := api.recovery.Exit(ctx); err != nil {
		return nil, toGRPCError(err)
	}
	return &corev1.Empty{}, nil
}

func (api *grpcAPI) SubscribeInvalidations(
	request *corev1.SubscribeInvalidationsRequest,
	stream grpc.ServerStreamingServer[corev1.QueryInvalidationEvent],
) error {
	if api == nil || api.broker == nil || stream == nil {
		return status.Error(codes.FailedPrecondition, "invalidation service unavailable")
	}
	domains := make([]core.InvalidationDomain, 0, len(request.GetDomains()))
	for _, domain := range request.GetDomains() {
		domains = append(domains, core.InvalidationDomain(domain))
	}
	events, unsubscribe, err := api.broker.Subscribe(stream.Context(), domains, request.GetAfterSequence())
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid invalidation subscription")
	}
	defer unsubscribe()
	if err := stream.SendHeader(metadata.Pairs("codex-pulse-stream-ready", "1")); err != nil {
		return err
	}
	for event := range events {
		if err := stream.Send(&corev1.QueryInvalidationEvent{
			Version: event.Version, Domain: string(event.Domain), Sequence: event.Sequence,
		}); err != nil {
			return err
		}
	}
	if err := stream.Context().Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return nil
}

func (api *grpcAPI) Shutdown(
	ctx context.Context,
	request *corev1.ShutdownRequest,
) (*corev1.ShutdownResponse, error) {
	if api == nil || api.shutdown == nil {
		return nil, status.Error(codes.FailedPrecondition, "shutdown service unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	reason := request.GetReason()
	if !validShutdownReason(reason) {
		return nil, status.Error(codes.InvalidArgument, "invalid shutdown reason")
	}
	return &corev1.ShutdownResponse{Accepted: api.shutdown.RequestShutdown(reason)}, nil
}

func validLifecycleEvent(event string) bool {
	switch event {
	case "system_will_sleep", "system_did_wake", "application_did_become_active":
		return true
	default:
		return false
	}
}

func validShutdownReason(reason string) bool {
	switch reason {
	case "client_exit", "client_restart", "migration_exit":
		return true
	default:
		return false
	}
}

func recoveryUnavailable() error {
	return status.Error(codes.FailedPrecondition, "migration recovery unavailable")
}
