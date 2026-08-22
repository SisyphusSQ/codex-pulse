package cursorprovider

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

type fixedAccessTokenSource struct {
	credential DesktopAccessToken
}

func (source fixedAccessTokenSource) ReadAccessToken(context.Context) (DesktopAccessToken, error) {
	return source.credential, nil
}

func TestDashboardClientAuthenticatesConnectRPCAndDecodesCurrentPeriodUsage(t *testing.T) {
	t.Parallel()

	var requestFailure string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/aiserver.v1.DashboardService/GetCurrentPeriodUsage"; got != want {
			requestFailure = "path = " + got
		}
		if got, want := request.Header.Get("Authorization"), "Bearer fixture-access-token"; got != want {
			requestFailure = "authorization header mismatch"
		}
		if got, want := request.Header.Get("Content-Type"), "application/proto"; got != want {
			requestFailure = "content type = " + got
		}
		if got, want := request.Header.Get("Connect-Protocol-Version"), "1"; got != want {
			requestFailure = "connect protocol version = " + got
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			requestFailure = "read request body"
		}
		if len(body) != 0 {
			requestFailure = "request body was not empty"
		}

		response.Header().Set("Content-Type", "application/proto")
		_, _ = response.Write(currentPeriodFixture())
	}))
	t.Cleanup(server.Close)

	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token:     "fixture-access-token",
			ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}

	usage, err := client.GetCurrentPeriodUsage(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentPeriodUsage() error = %v", err)
	}
	if requestFailure != "" {
		t.Fatal(requestFailure)
	}
	if usage.BillingCycleStartMS != 1_780_000_000_000 || usage.BillingCycleEndMS != 1_782_592_000_000 {
		t.Fatalf("billing cycle = %d..%d", usage.BillingCycleStartMS, usage.BillingCycleEndMS)
	}
	if usage.PlanUsage == nil || usage.PlanUsage.TotalSpendCents != 1_250 || usage.PlanUsage.IncludedSpendCents != 1_000 || usage.PlanUsage.LimitCents != 2_000 {
		t.Fatalf("plan usage = %#v", usage.PlanUsage)
	}
	if usage.PlanUsage.CursorModelsUsedPercent == nil || *usage.PlanUsage.CursorModelsUsedPercent != 7 ||
		usage.PlanUsage.OtherModelsUsedPercent == nil || *usage.PlanUsage.OtherModelsUsedPercent != 0 {
		t.Fatalf("model quota percentages = %#v", usage.PlanUsage)
	}
	if !usage.Enabled {
		t.Fatal("usage display must be enabled")
	}
}

func TestDashboardClientEncodesUsageWindowAndDropsPrivateDisplayFields(t *testing.T) {
	t.Parallel()

	var requestFailure string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			requestFailure = "read request body"
		} else if got, want := usageRequestFields(body), map[protowire.Number]uint64{
			2: 1_780_000_000_000,
			3: 1_780_086_400_000,
			6: 2,
			7: 100,
		}; !equalWireFields(got, want) {
			requestFailure = "usage request fields mismatch"
		}
		response.Header().Set("Content-Type", "application/proto")
		_, _ = response.Write(filteredUsageFixture())
	}))
	t.Cleanup(server.Close)

	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: "fixture-access-token", ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	page, err := client.GetFilteredUsageEvents(context.Background(), UsageEventsRequest{
		StartAtMS: 1_780_000_000_000, EndAtMS: 1_780_086_400_000, Page: 2, PageSize: 100,
	})
	if err != nil {
		t.Fatalf("GetFilteredUsageEvents() error = %v", err)
	}
	if requestFailure != "" {
		t.Fatal(requestFailure)
	}
	if page.TotalCount != 1 || len(page.Events) != 1 {
		t.Fatalf("usage page = %#v", page)
	}
	event := page.Events[0]
	if event.OccurredAtMS != 1_780_000_001_000 || event.Model != "cursor-model" || event.ConversationID != "conversation-id" {
		t.Fatalf("usage event identity = %#v", event)
	}
	if event.TokenUsage == nil || event.TokenUsage.InputTokens != 10 || event.TokenUsage.OutputTokens != 20 ||
		event.TokenUsage.CacheWriteTokens != 30 || event.TokenUsage.CacheReadTokens != 40 {
		t.Fatalf("token usage = %#v", event.TokenUsage)
	}
	if event.ChargedCents != 1.5 || event.CursorTokenFeeCents != 0.25 {
		t.Fatalf("reported charge = %v + %v", event.ChargedCents, event.CursorTokenFeeCents)
	}
	if !event.TokenBased {
		t.Fatal("usage event must retain token-based semantics")
	}
}

func TestDashboardClientGetSandUsageStatusAuthenticatesAndDecodesAllowlistedFields(t *testing.T) {
	t.Parallel()

	const token = "fixture-access-token"
	var requestFailure string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/aiserver.v1.DashboardService/GetSandUsageStatus"; got != want {
			requestFailure = "path = " + got
		}
		if got, want := request.Header.Get("Authorization"), "Bearer "+token; got != want {
			requestFailure = "authorization header mismatch"
		}
		if got, want := request.Header.Get("Content-Type"), "application/proto"; got != want {
			requestFailure = "content type = " + got
		}
		if got, want := request.Header.Get("Connect-Protocol-Version"), "1"; got != want {
			requestFailure = "connect protocol version = " + got
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			requestFailure = "read request body"
		}
		if len(body) != 0 {
			requestFailure = "request body was not empty"
		}
		response.Header().Set("Content-Type", "application/proto")
		_, _ = response.Write(sandUsageFixture(sandUsageFixtureOptions{
			included: true, percent: pointerFloat64ForDashboardClientTest(0),
		}))
	}))
	t.Cleanup(server.Close)

	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: token, ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	status, err := client.GetSandUsageStatus(context.Background())
	if err != nil {
		t.Fatalf("GetSandUsageStatus() error = %v", err)
	}
	if requestFailure != "" {
		t.Fatal(requestFailure)
	}
	if !status.Included || status.UsagePercent == nil || *status.UsagePercent != 0 {
		t.Fatalf("zero percent status = %#v", status)
	}
	if status.PeriodStartMS != 1_780_000_000_000 || status.NextResetAtMS != 1_780_561_600_000 {
		t.Fatalf("sand period = %d..%d", status.PeriodStartMS, status.NextResetAtMS)
	}
}

func TestDecodeSandUsageStatusFixtureMatrix(t *testing.T) {
	t.Parallel()

	zero := sandUsageFixture(sandUsageFixtureOptions{included: true, percent: pointerFloat64ForDashboardClientTest(0)})
	full := sandUsageFixture(sandUsageFixtureOptions{included: true, percent: pointerFloat64ForDashboardClientTest(100)})
	missing := sandUsageFixture(sandUsageFixtureOptions{included: true})
	excluded := sandUsageFixture(sandUsageFixtureOptions{included: false, includedLimitZero: true})
	malformedNanos := sandUsageFixture(sandUsageFixtureOptions{
		included: true, percent: pointerFloat64ForDashboardClientTest(12), nanos: 1_000_000_000,
	})
	outOfRange := sandUsageFixture(sandUsageFixtureOptions{
		included: true, percent: pointerFloat64ForDashboardClientTest(100.1),
	})
	nanBits := math.Float64bits(math.NaN())
	nanMessage := sandUsageTimestamps(nil, 0)
	nanMessage = appendFixed64Field(nanMessage, 3, nanBits)
	nanMessage = appendVarintField(nanMessage, 8, 1)
	duplicate := append(zero, zero...)
	nonProto := []byte("not-a-proto-body")

	status, err := decodeSandUsageStatus(zero)
	if err != nil || status.UsagePercent == nil || *status.UsagePercent != 0 || !status.Included {
		t.Fatalf("decode 0%% = %#v, %v", status, err)
	}
	status, err = decodeSandUsageStatus(full)
	if err != nil || status.UsagePercent == nil || *status.UsagePercent != 100 {
		t.Fatalf("decode 100%% = %#v, %v", status, err)
	}
	status, err = decodeSandUsageStatus(missing)
	if err != nil || !status.Included || status.UsagePercent != nil {
		t.Fatalf("missing percent must stay nil, got %#v, %v", status, err)
	}
	status, err = decodeSandUsageStatus(excluded)
	if err != nil || status.Included || status.UsagePercent != nil {
		t.Fatalf("excluded plan = %#v, %v", status, err)
	}
	if _, err = decodeSandUsageStatus(malformedNanos); err == nil {
		t.Fatal("malformed timestamp nanos must fail closed")
	}
	if _, err = decodeSandUsageStatus(outOfRange); err == nil {
		t.Fatal("out of range percent must fail closed")
	}
	if _, err = decodeSandUsageStatus(nanMessage); err == nil {
		t.Fatal("NaN percent must fail closed")
	}
	if _, err = decodeSandUsageStatus(duplicate); err == nil {
		t.Fatal("duplicate protobuf fields must fail closed")
	}
	if _, err = decodeSandUsageStatus(nonProto); err == nil {
		t.Fatal("non-proto body must fail closed")
	}

	repeatedUnknown := sandUsageFixture(sandUsageFixtureOptions{
		included: true, percent: pointerFloat64ForDashboardClientTest(0),
	})
	repeatedUnknown = appendMessageField(repeatedUnknown, 12, []byte{0x08, 0x01})
	repeatedUnknown = appendMessageField(repeatedUnknown, 12, []byte{0x08, 0x02})
	status, err = decodeSandUsageStatus(repeatedUnknown)
	if err != nil || !status.Included || status.UsagePercent == nil || *status.UsagePercent != 0 {
		t.Fatalf("repeated unknown field 12 must be skipped, got %#v, %v", status, err)
	}
}

func TestDashboardClientGetSandUsageStatusMapsAuthFailureWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	const token = "secret-sand-token"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: token, ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	_, err = client.GetSandUsageStatus(context.Background())
	if err == nil || !errors.Is(err, ErrDesktopAuthExpired) {
		t.Fatalf("GetSandUsageStatus() error = %v, want auth expired", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("auth error leaked token: %v", err)
	}
}

func TestDashboardClientGetSandUsageStatusRejectsNonProtoResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"usage_percent":0}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: "fixture-access-token", ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	_, err = client.GetSandUsageStatus(context.Background())
	if err == nil || !errors.Is(err, ErrDashboardTransport) || errors.Is(err, ErrDashboardProtocol) {
		t.Fatalf("GetSandUsageStatus() error = %v, want transport error", err)
	}
	if strings.Contains(err.Error(), "fixture-access-token") {
		t.Fatalf("transport error leaked token: %v", err)
	}
}

func TestDashboardClientGetSandUsageStatusMapsServerErrorAsTransport(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: "fixture-access-token", ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	_, err = client.GetSandUsageStatus(context.Background())
	if err == nil || !errors.Is(err, ErrDashboardTransport) || errors.Is(err, ErrDashboardProtocol) {
		t.Fatalf("GetSandUsageStatus() error = %v, want transport error", err)
	}
}

func TestDashboardClientGetSandUsageStatusMapsTimeoutAsTransport(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	httpClient := server.Client()
	httpClient.Timeout = 40 * time.Millisecond
	client, err := NewDashboardClient(DashboardClientConfig{
		BaseURL:    server.URL,
		HTTPClient: httpClient,
		TokenSource: fixedAccessTokenSource{credential: DesktopAccessToken{
			Token: "fixture-access-token", ExpiresAt: time.Unix(1_800_000_000, 0),
		}},
	})
	if err != nil {
		t.Fatalf("NewDashboardClient() error = %v", err)
	}
	_, err = client.GetSandUsageStatus(context.Background())
	if err == nil || !errors.Is(err, ErrDashboardTransport) || errors.Is(err, ErrDashboardProtocol) {
		t.Fatalf("GetSandUsageStatus() error = %v, want transport error", err)
	}
}

type sandUsageFixtureOptions struct {
	included          bool
	includedLimitZero bool
	percent           *float64
	nanos             uint64
}

func sandUsageFixture(options sandUsageFixtureOptions) []byte {
	message := sandUsageTimestamps(nil, options.nanos)
	if options.percent != nil {
		message = appendFixed64Field(message, 3, math.Float64bits(*options.percent))
	}
	if options.includedLimitZero {
		message = appendVarintField(message, 4, 1)
	}
	if options.included {
		message = appendVarintField(message, 8, 1)
	}
	message = appendVarintField(message, 5, 2)
	message = appendVarintField(message, 7, 1)
	return message
}

func sandUsageTimestamps(message []byte, nanos uint64) []byte {
	start := appendVarintField(nil, 1, 1_780_000_000)
	reset := appendVarintField(nil, 1, 1_780_561_600)
	if nanos != 0 {
		reset = appendVarintField(reset, 2, nanos)
	}
	message = appendMessageField(message, 1, start)
	return appendMessageField(message, 2, reset)
}

func pointerFloat64ForDashboardClientTest(value float64) *float64 { return &value }

func currentPeriodFixture() []byte {
	plan := protowire.AppendTag(nil, 1, protowire.VarintType)
	plan = protowire.AppendVarint(plan, 1_250)
	plan = protowire.AppendTag(plan, 2, protowire.VarintType)
	plan = protowire.AppendVarint(plan, 1_000)
	plan = protowire.AppendTag(plan, 5, protowire.VarintType)
	plan = protowire.AppendVarint(plan, 2_000)
	plan = appendFixed64Field(plan, 12, math.Float64bits(7))
	plan = appendFixed64Field(plan, 13, math.Float64bits(0))

	message := protowire.AppendTag(nil, 1, protowire.VarintType)
	message = protowire.AppendVarint(message, 1_780_000_000_000)
	message = protowire.AppendTag(message, 2, protowire.VarintType)
	message = protowire.AppendVarint(message, 1_782_592_000_000)
	message = protowire.AppendTag(message, 3, protowire.BytesType)
	message = protowire.AppendBytes(message, plan)
	message = protowire.AppendTag(message, 6, protowire.VarintType)
	return protowire.AppendVarint(message, 1)
}

func filteredUsageFixture() []byte {
	tokens := appendVarintField(nil, 1, 10)
	tokens = appendVarintField(tokens, 2, 20)
	tokens = appendVarintField(tokens, 3, 30)
	tokens = appendVarintField(tokens, 4, 40)

	event := appendVarintField(nil, 1, 1_780_000_001_000)
	event = appendStringField(event, 2, "cursor-model")
	event = appendVarintField(event, 3, 1)
	event = appendVarintField(event, 8, 1)
	event = appendMessageField(event, 9, tokens)
	event = appendStringField(event, 10, "private-owner-must-be-dropped")
	event = appendStringField(event, 12, "private-email-must-be-dropped@example.invalid")
	event = appendFixed32Field(event, 13, math.Float32bits(0.25))
	event = appendFixed32Field(event, 18, math.Float32bits(1.5))
	event = appendStringField(event, 23, "conversation-id")

	response := appendVarintField(nil, 2, 1)
	return appendMessageField(response, 3, event)
}

func appendVarintField(message []byte, number protowire.Number, value uint64) []byte {
	message = protowire.AppendTag(message, number, protowire.VarintType)
	return protowire.AppendVarint(message, value)
}

func appendStringField(message []byte, number protowire.Number, value string) []byte {
	message = protowire.AppendTag(message, number, protowire.BytesType)
	return protowire.AppendString(message, value)
}

func appendMessageField(message []byte, number protowire.Number, value []byte) []byte {
	message = protowire.AppendTag(message, number, protowire.BytesType)
	return protowire.AppendBytes(message, value)
}

func appendFixed32Field(message []byte, number protowire.Number, value uint32) []byte {
	message = protowire.AppendTag(message, number, protowire.Fixed32Type)
	return protowire.AppendFixed32(message, value)
}

func appendFixed64Field(message []byte, number protowire.Number, value uint64) []byte {
	message = protowire.AppendTag(message, number, protowire.Fixed64Type)
	return protowire.AppendFixed64(message, value)
}

func usageRequestFields(message []byte) map[protowire.Number]uint64 {
	fields := make(map[protowire.Number]uint64)
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 || wireType != protowire.VarintType {
			return nil
		}
		message = message[tagLength:]
		value, valueLength := protowire.ConsumeVarint(message)
		if valueLength < 0 {
			return nil
		}
		fields[number] = value
		message = message[valueLength:]
	}
	return fields
}

func equalWireFields(left, right map[protowire.Number]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}
