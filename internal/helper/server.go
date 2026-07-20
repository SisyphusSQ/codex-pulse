package helper

import (
	"context"
	"errors"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumRPCMessageBytes = 16 * 1024 * 1024

var ErrGRPCServer = errors.New("helper grpc server is unavailable")

type ServerConfig struct {
	Authenticator *Authenticator
	HelperVersion string
	Service       *core.Service
	Broker        *core.InvalidationBroker
	Recovery      core.MigrationRecoveryService
	Lifecycle     LifecycleNotifier
	Shutdown      ShutdownRequester
}

type LifecycleNotifier interface {
	NotifyLifecycle(context.Context, string) error
}

type ShutdownRequester interface {
	RequestShutdown(string) bool
}

type grpcAPI struct {
	corev1.UnimplementedCoreServiceServer
	helperVersion string
	service       *core.Service
	broker        *core.InvalidationBroker
	recovery      core.MigrationRecoveryService
	lifecycle     LifecycleNotifier
	shutdown      ShutdownRequester
}

func NewGRPCServer(config ServerConfig) (*grpc.Server, error) {
	if config.Authenticator == nil || config.HelperVersion == "" {
		return nil, ErrGRPCServer
	}
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unaryAuthentication(config.Authenticator)),
		grpc.ChainStreamInterceptor(streamAuthentication(config.Authenticator)),
		grpc.MaxRecvMsgSize(maximumRPCMessageBytes),
		grpc.MaxSendMsgSize(maximumRPCMessageBytes),
	)
	corev1.RegisterCoreServiceServer(server, &grpcAPI{
		helperVersion: config.HelperVersion,
		service:       config.Service,
		broker:        config.Broker,
		recovery:      config.Recovery,
		lifecycle:     config.Lifecycle,
		shutdown:      config.Shutdown,
	})
	return server, nil
}

func (api *grpcAPI) ListSessions(
	ctx context.Context,
	request *corev1.ListSessionsRequest,
) (*corev1.SessionListResponse, error) {
	if api == nil || api.service == nil {
		return nil, status.Error(codes.FailedPrecondition, "core service unavailable")
	}
	response, err := api.service.ListSessions(ctx, fromProtoQuery(request.GetQuery()))
	if err != nil {
		return nil, toGRPCError(err)
	}
	target := &corev1.SessionListResponse{}
	if err := core.EncodeResponse(response, target); err != nil {
		return nil, toGRPCError(err)
	}
	return target, nil
}

func fromProtoQuery(request *corev1.QueryRequest) basequery.Request {
	if request == nil {
		return basequery.Request{}
	}
	result := basequery.Request{
		Page:    basequery.PageRequest{Limit: int(request.GetPage().GetLimit())},
		Sort:    make([]basequery.SortTerm, 0, len(request.Sort)),
		Filters: make([]basequery.FilterTerm, 0, len(request.Filters)),
	}
	if request.Page != nil && request.Page.Cursor != nil {
		cursor := request.Page.GetCursor()
		result.Page.Cursor = &cursor
	}
	for _, term := range request.Sort {
		if term == nil {
			continue
		}
		result.Sort = append(result.Sort, basequery.SortTerm{
			Field: term.Field, Direction: basequery.SortDirection(term.Direction),
		})
	}
	for _, term := range request.Filters {
		if term == nil {
			continue
		}
		result.Filters = append(result.Filters, basequery.FilterTerm{
			Field: term.Field, Operator: basequery.FilterOperator(term.Operator),
			Values: append([]string(nil), term.Values...),
		})
	}
	if request.TimeRange != nil {
		result.TimeRange = &basequery.LocalDateRange{
			StartDate: request.TimeRange.StartDate, EndDateExclusive: request.TimeRange.EndDateExclusive,
			TimeZone: request.TimeRange.TimeZone,
		}
	}
	return result
}

func toGRPCError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	envelope, classified := basequery.ErrorEnvelopeFrom(err)
	if !classified {
		envelope, _ = basequery.ErrorEnvelopeFrom(basequery.NewUnavailableFailure(err))
	}
	code := grpcCode(envelope.Error.Code)
	grpcStatus := status.New(code, grpcMessage(envelope.Error.Code))
	detail := &corev1.ErrorDetail{
		Code: string(envelope.Error.Code), MessageKey: envelope.Error.MessageKey,
		Retryable: envelope.Error.Retryable,
	}
	if envelope.Error.Field != nil {
		field := *envelope.Error.Field
		detail.Field = &field
	}
	withDetails, detailErr := grpcStatus.WithDetails(detail)
	if detailErr != nil {
		return status.Error(codes.Internal, "internal query failure")
	}
	return withDetails.Err()
}

func grpcCode(code basequery.ErrorCode) codes.Code {
	switch code {
	case basequery.ErrorValidation:
		return codes.InvalidArgument
	case basequery.ErrorNotFound:
		return codes.NotFound
	case basequery.ErrorPartial:
		return codes.FailedPrecondition
	case basequery.ErrorUnavailable:
		return codes.Unavailable
	case basequery.ErrorCancelled:
		return codes.Canceled
	case basequery.ErrorDeadlineExceeded:
		return codes.DeadlineExceeded
	default:
		return codes.Internal
	}
}

func grpcMessage(code basequery.ErrorCode) string {
	switch code {
	case basequery.ErrorValidation:
		return "query request is invalid"
	case basequery.ErrorNotFound:
		return "query result is not found"
	case basequery.ErrorPartial:
		return "query result is partial"
	case basequery.ErrorUnavailable:
		return "query result is unavailable"
	case basequery.ErrorCancelled:
		return "query request cancelled"
	case basequery.ErrorDeadlineExceeded:
		return "query deadline exceeded"
	default:
		return "internal query failure"
	}
}

func (api *grpcAPI) Handshake(
	ctx context.Context,
	request *corev1.HandshakeRequest,
) (*corev1.HandshakeResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if request == nil || request.ClientName == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid handshake")
	}
	if request.ContractVersion != core.ContractVersion {
		return nil, status.Error(codes.FailedPrecondition, "contract version mismatch")
	}
	return &corev1.HandshakeResponse{
		HelperVersion: api.helperVersion, ContractVersion: core.ContractVersion,
		QueryVersion: basequery.ContractVersion, Transport: "grpc+unix",
	}, nil
}

func unaryAuthentication(authenticator *Authenticator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if authenticator == nil || info == nil {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		if err := authenticator.Authorize(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

func streamAuthentication(authenticator *Authenticator) grpc.StreamServerInterceptor {
	return func(
		service any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if authenticator == nil || stream == nil || info == nil {
			return status.Error(codes.Unauthenticated, "authentication required")
		}
		if err := authenticator.Authorize(stream.Context()); err != nil {
			return err
		}
		return handler(service, stream)
	}
}
