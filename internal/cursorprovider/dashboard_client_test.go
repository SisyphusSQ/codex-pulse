package cursorprovider

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
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
