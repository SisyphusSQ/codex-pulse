package helper

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// 测试 gRPC server 对所有 unary 调用执行鉴权，并协商精确 contract。
func TestGRPCServerAuthenticatesHandshakeAndNegotiatesContract(t *testing.T) {
	client, authorize := startTestGRPCServer(t)
	if _, err := client.Handshake(t.Context(), &corev1.HandshakeRequest{
		ClientName: "swift-tests", ContractVersion: core.ContractVersion,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Handshake(without credentials) error = %v", err)
	}

	response, err := client.Handshake(authorize(t.Context()), &corev1.HandshakeRequest{
		ClientName: "swift-tests", ClientVersion: "test", ContractVersion: core.ContractVersion,
	})
	if err != nil {
		t.Fatalf("Handshake(valid) error = %v", err)
	}
	if response.ContractVersion != core.ContractVersion || response.HelperVersion != "test-helper" ||
		response.Transport != "grpc+unix" {
		t.Fatalf("Handshake(valid) response = %#v", response)
	}

	if _, err := client.Handshake(authorize(t.Context()), &corev1.HandshakeRequest{
		ClientName: "swift-tests", ContractVersion: "future-contract",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Handshake(mismatch) error = %v, want FailedPrecondition", err)
	}
}

// 测试 grpcAPI 显式实现每个冻结 RPC，不能落入 generated Unimplemented 默认实现。
func TestGRPCAPIImplementsEveryFrozenRPC(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(".", entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		source.Write(content)
	}
	matches := regexp.MustCompile(`func \(api \*grpcAPI\) ([A-Z][A-Za-z0-9_]*)\s*\(`).FindAllStringSubmatch(source.String(), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	sort.Strings(got)
	want := []string{
		"AnalyzeSessionIndexRepair", "Bootstrap", "ConfirmHomeSwitch", "Contracts", "DataHealth",
		"Handshake", "Health", "HealthProjection", "Job", "ListHealth", "ListJobs", "ListProjects",
		"ListSessions", "ListSources", "MigrationRecoveryCancel", "MigrationRecoveryConfirm",
		"MigrationRecoveryExit", "MigrationRecoveryPrepare", "MigrationRecoveryRetry",
		"MigrationRecoveryState", "NotifyLifecycle", "PlanHomeSwitch", "ProjectDetail", "QuotaCurrent",
		"RecoverHomeSwitch", "RequestQuotaRefresh", "RunRuntimeAction", "SessionDetail", "Settings",
		"Shutdown", "Source", "SubscribeInvalidations", "UpdateSettings", "UsageCost",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("grpcAPI handlers = %v, want %v", got, want)
	}
}

// 测试 ListSessions RPC 保留 partial/unknown，并返回 content-free typed validation detail。
func TestGRPCServerMapsBusinessResponseAndTypedError(t *testing.T) {
	unknownCount, err := basequery.UnknownNumeric(basequery.NumericCount, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	unknownTokens, err := basequery.UnknownNumeric(basequery.NumericTokens, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	unknownCost, err := basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	unknownTime, err := basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNotComputed)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := basequery.NewResponseMeta(basequery.ResponsePartial, nil, []basequery.ErrorCode{basequery.ErrorPartial})
	if err != nil {
		t.Fatal(err)
	}
	usage := &helperUsageQueryStub{response: usagecost.SessionListResponse{
		Meta: meta, MatchedCount: unknownCount,
		MatchedTotals: helperUsageTotals(unknownCount, unknownTokens, unknownCost, unknownTime),
		PageTotals:    helperUsageTotals(unknownCount, unknownTokens, unknownCost, unknownTime),
	}}
	business, err := core.NewService(core.ServiceConfig{UsageCost: usage, RuntimeInfo: helperRuntimeQueryStub{}})
	if err != nil {
		t.Fatal(err)
	}
	client, authorize := startConfiguredTestGRPCServer(t, func(config *ServerConfig) { config.Service = business })
	response, err := client.ListSessions(authorize(t.Context()), &corev1.ListSessionsRequest{
		Query: &corev1.QueryRequest{
			Page: &corev1.PageRequest{Limit: 17},
			ExactTimeRange: &corev1.UTCTimeRange{
				StartAtMs: 1_753_056_000_000, EndAtMs: 1_753_059_600_000, TimeZone: "Asia/Shanghai",
			},
		},
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if usage.request.Page.Limit != 17 || usage.request.ExactTimeRange == nil ||
		usage.request.ExactTimeRange.StartAtMS != 1_753_056_000_000 ||
		usage.request.ExactTimeRange.EndAtMS != 1_753_059_600_000 ||
		usage.request.ExactTimeRange.TimeZone != "Asia/Shanghai" ||
		response.Meta == nil || response.Meta.Status != "partial" ||
		response.MatchedCount == nil || response.MatchedCount.UnknownReason == nil {
		t.Fatalf("ListSessions() request = %#v, response = %#v", usage.request, response)
	}

	usage.err = basequery.NewValidationFailure("page.limit", nil)
	_, err = client.ListSessions(authorize(t.Context()), &corev1.ListSessionsRequest{})
	grpcStatus, ok := status.FromError(err)
	if !ok || grpcStatus.Code() != codes.InvalidArgument || grpcStatus.Message() != "query request is invalid" {
		t.Fatalf("ListSessions(invalid) error = %v", err)
	}
	details := grpcStatus.Details()
	if len(details) != 1 {
		t.Fatalf("validation details = %#v", details)
	}
	detail, ok := details[0].(*corev1.ErrorDetail)
	if !ok || detail.Code != "validation" || detail.MessageKey == "" || detail.Field == nil || *detail.Field != "page.limit" {
		t.Fatalf("validation detail = %#v", details[0])
	}
}

func TestGRPCServerStreamsInvalidationsAndAcceptsShutdownOnce(t *testing.T) {
	broker, err := core.NewInvalidationBroker(2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	shutdown := &shutdownRequestStub{}
	client, authorize := startConfiguredTestGRPCServer(t, func(config *ServerConfig) {
		config.Broker = broker
		config.Shutdown = shutdown
	})
	unauthenticated, err := client.SubscribeInvalidations(t.Context(), &corev1.SubscribeInvalidationsRequest{})
	if err == nil {
		_, err = unauthenticated.Recv()
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("SubscribeInvalidations(without auth) error = %v", err)
	}
	stream, err := client.SubscribeInvalidations(authorize(t.Context()), &corev1.SubscribeInvalidationsRequest{
		Domains: []string{"quota"},
	})
	if err != nil {
		t.Fatal(err)
	}
	headers, err := stream.Header()
	if err != nil || strings.Join(headers.Get("codex-pulse-stream-ready"), "") != "1" {
		t.Fatalf("stream headers = %v, %v", headers, err)
	}
	if err := broker.Notify(t.Context(), core.InvalidationIndex); err != nil {
		t.Fatal(err)
	}
	if err := broker.Notify(t.Context(), core.InvalidationQuota); err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if event.Domain != "quota" || event.Sequence != 2 || event.Version != core.InvalidationContractVersion {
		t.Fatalf("event = %#v", event)
	}
	first, err := client.Shutdown(authorize(t.Context()), &corev1.ShutdownRequest{Reason: "client_exit"})
	if err != nil || !first.Accepted {
		t.Fatalf("Shutdown(first) = %#v, %v", first, err)
	}
	second, err := client.Shutdown(authorize(t.Context()), &corev1.ShutdownRequest{Reason: "client_restart"})
	if err != nil || second.Accepted {
		t.Fatalf("Shutdown(second) = %#v, %v", second, err)
	}
	if shutdown.reason != "client_exit" {
		t.Fatalf("shutdown reason = %q", shutdown.reason)
	}
}

func startTestGRPCServer(
	t testing.TB,
) (corev1.CoreServiceClient, func(context.Context) context.Context) {
	return startConfiguredTestGRPCServer(t, nil)
}

// 测试 UsageCost RPC 映射保留精确 UTC 半开区间，不转换成本地自然日。
func TestFromProtoUsageCostRequestPreservesExactRange(t *testing.T) {
	request := fromProtoUsageCostRequest(&corev1.UsageCostRequest{
		Granularity: "day",
		ExactRange: &corev1.UTCTimeRange{
			StartAtMs: 1_753_056_000_000, EndAtMs: 1_753_059_600_000, TimeZone: "Asia/Shanghai",
		},
	})
	if request.Granularity != usagecost.TrendDay || request.ExactRange == nil ||
		request.ExactRange.StartAtMS != 1_753_056_000_000 || request.ExactRange.EndAtMS != 1_753_059_600_000 ||
		request.ExactRange.TimeZone != "Asia/Shanghai" || request.Range != (basequery.LocalDateRange{}) {
		t.Fatalf("mapped exact usage request = %#v", request)
	}
}

func startConfiguredTestGRPCServer(
	t testing.TB,
	configure func(*ServerConfig),
) (corev1.CoreServiceClient, func(context.Context) context.Context) {
	t.Helper()
	rawToken := bytes.Repeat([]byte("g"), 32)
	credential := string(rawToken)
	authenticator, err := NewAuthenticator(rawToken)
	if err != nil {
		t.Fatal(err)
	}
	config := ServerConfig{
		Authenticator: authenticator,
		HelperVersion: "test-helper",
	}
	if configure != nil {
		configure(&config)
	}
	server, err := NewGRPCServer(config)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	connection, err := grpc.NewClient(
		"passthrough:///codex-pulse-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	authorize := func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+credential)
	}
	return corev1.NewCoreServiceClient(connection), authorize
}

type helperUsageQueryStub struct {
	request  basequery.Request
	response usagecost.SessionListResponse
	err      error
}

type shutdownRequestStub struct {
	mu     sync.Mutex
	reason string
}

func (stub *shutdownRequestStub) RequestShutdown(reason string) bool {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.reason != "" {
		return false
	}
	stub.reason = reason
	return true
}

func (*helperUsageQueryStub) UsageCost(context.Context, usagecost.UsageCostRequest) (usagecost.UsageCostResponse, error) {
	return usagecost.UsageCostResponse{}, nil
}

func (stub *helperUsageQueryStub) ListSessions(
	_ context.Context,
	request basequery.Request,
) (usagecost.SessionListResponse, error) {
	stub.request = request
	return stub.response, stub.err
}

func (*helperUsageQueryStub) SessionDetail(context.Context, usagecost.SessionDetailRequest) (usagecost.SessionDetailResponse, error) {
	return usagecost.SessionDetailResponse{}, nil
}

func (*helperUsageQueryStub) ListProjects(context.Context, basequery.Request) (usagecost.ProjectListResponse, error) {
	return usagecost.ProjectListResponse{}, nil
}

func (*helperUsageQueryStub) ProjectDetail(context.Context, usagecost.ProjectDetailRequest) (usagecost.ProjectDetailResponse, error) {
	return usagecost.ProjectDetailResponse{}, nil
}

type helperRuntimeQueryStub struct{}

func (helperRuntimeQueryStub) QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error) {
	return runtimeinfo.QuotaCurrentResponse{}, nil
}
func (helperRuntimeQueryStub) ListSources(context.Context, basequery.Request) (runtimeinfo.SourceListResponse, error) {
	return runtimeinfo.SourceListResponse{}, nil
}
func (helperRuntimeQueryStub) Source(context.Context, runtimeinfo.SourceDetailRequest) (runtimeinfo.SourceDetailResponse, error) {
	return runtimeinfo.SourceDetailResponse{}, nil
}
func (helperRuntimeQueryStub) ListJobs(context.Context, basequery.Request) (runtimeinfo.JobListResponse, error) {
	return runtimeinfo.JobListResponse{}, nil
}
func (helperRuntimeQueryStub) Job(context.Context, runtimeinfo.JobDetailRequest) (runtimeinfo.JobDetailResponse, error) {
	return runtimeinfo.JobDetailResponse{}, nil
}
func (helperRuntimeQueryStub) ListHealth(context.Context, basequery.Request) (runtimeinfo.HealthListResponse, error) {
	return runtimeinfo.HealthListResponse{}, nil
}
func (helperRuntimeQueryStub) Health(context.Context, runtimeinfo.HealthDetailRequest) (runtimeinfo.HealthDetailResponse, error) {
	return runtimeinfo.HealthDetailResponse{}, nil
}
func (helperRuntimeQueryStub) DataHealth(context.Context, int64) (runtimeinfo.DataHealthResponse, error) {
	return runtimeinfo.DataHealthResponse{}, nil
}
func (helperRuntimeQueryStub) Settings(context.Context) (runtimeinfo.SettingsResponse, error) {
	return runtimeinfo.SettingsResponse{}, nil
}

func helperUsageTotals(
	count basequery.NumericValue,
	tokens basequery.NumericValue,
	cost basequery.NumericValue,
	timestamp basequery.NumericValue,
) usagecost.UsageTotals {
	return usagecost.UsageTotals{
		TurnCount: count, InputTokens: tokens, CachedInputTokens: tokens, OutputTokens: tokens,
		ReasoningTokens: tokens, TotalTokens: tokens, EstimatedUSDMicros: cost,
		PricedTurnCount: count, UnpricedTurnCount: count,
		FirstActivityAtMS: timestamp, LastActivityAtMS: timestamp,
	}
}
