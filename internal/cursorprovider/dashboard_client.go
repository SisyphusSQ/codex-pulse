package cursorprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	DefaultDashboardBaseURL   = "https://api2.cursor.sh"
	maxDashboardResponseBytes = 8 << 20
)

var (
	ErrDashboardProtocol  = errors.New("cursor dashboard protocol is unavailable")
	ErrDashboardTransport = errors.New("cursor dashboard transport is unavailable")
)

type AccessTokenSource interface {
	ReadAccessToken(context.Context) (DesktopAccessToken, error)
}

type DashboardClientConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	TokenSource AccessTokenSource
}

type DashboardClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	tokenSource AccessTokenSource
}

type CurrentPeriodUsage struct {
	BillingCycleStartMS int64
	BillingCycleEndMS   int64
	PlanUsage           *CurrentPlanUsage
	Enabled             bool
}

type CurrentPlanUsage struct {
	TotalSpendCents         int64
	IncludedSpendCents      int64
	BonusSpendCents         int64
	RemainingCents          int64
	LimitCents              int64
	CursorModelsUsedPercent *float64
	OtherModelsUsedPercent  *float64
}

type UsageEventsRequest struct {
	StartAtMS int64
	EndAtMS   int64
	Page      int32
	PageSize  int32
}

type UsageEventsPage struct {
	TotalCount int64
	Events     []DashboardUsageEvent
}

type DashboardUsageEvent struct {
	OccurredAtMS        int64
	Model               string
	Kind                int32
	RequestCosts        float64
	TokenBased          bool
	TokenUsage          *DashboardTokenUsage
	CursorTokenFeeCents float64
	ChargedCents        float64
	ConversationID      string
}

type DashboardTokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
	TotalCents       float64
}

func NewDashboardClient(config DashboardClientConfig) (*DashboardClient, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || config.HTTPClient == nil || config.TokenSource == nil {
		return nil, ErrDashboardProtocol
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &DashboardClient{baseURL: parsed, httpClient: config.HTTPClient, tokenSource: config.TokenSource}, nil
}

func (client *DashboardClient) GetCurrentPeriodUsage(ctx context.Context) (CurrentPeriodUsage, error) {
	if client == nil || ctx == nil {
		return CurrentPeriodUsage{}, ErrDashboardProtocol
	}
	payload, err := client.call(ctx, "GetCurrentPeriodUsage", nil)
	if err != nil {
		return CurrentPeriodUsage{}, err
	}
	usage, err := decodeCurrentPeriodUsage(payload)
	if err != nil {
		return CurrentPeriodUsage{}, fmt.Errorf("%w: decode current period usage", ErrDashboardProtocol)
	}
	return usage, nil
}

type GrokBotUsageStatus struct {
	Included      bool
	UsagePercent  *float64
	PeriodStartMS int64
	NextResetAtMS int64
}

func (client *DashboardClient) GetSandUsageStatus(ctx context.Context) (GrokBotUsageStatus, error) {
	if client == nil || ctx == nil {
		return GrokBotUsageStatus{}, ErrDashboardProtocol
	}
	payload, err := client.call(ctx, "GetSandUsageStatus", nil)
	if err != nil {
		return GrokBotUsageStatus{}, err
	}
	status, err := decodeSandUsageStatus(payload)
	if err != nil {
		return GrokBotUsageStatus{}, fmt.Errorf("%w: decode sand usage status", ErrDashboardProtocol)
	}
	return status, nil
}

func (client *DashboardClient) GetFilteredUsageEvents(ctx context.Context, request UsageEventsRequest) (UsageEventsPage, error) {
	if client == nil || ctx == nil || request.StartAtMS <= 0 || request.EndAtMS <= request.StartAtMS || request.Page <= 0 || request.PageSize <= 0 {
		return UsageEventsPage{}, ErrDashboardProtocol
	}
	payload := appendVarint(nil, 2, uint64(request.StartAtMS))
	payload = appendVarint(payload, 3, uint64(request.EndAtMS))
	payload = appendVarint(payload, 6, uint64(request.Page))
	payload = appendVarint(payload, 7, uint64(request.PageSize))
	response, err := client.call(ctx, "GetFilteredUsageEvents", payload)
	if err != nil {
		return UsageEventsPage{}, err
	}
	page, err := decodeUsageEventsPage(response)
	if err != nil {
		return UsageEventsPage{}, fmt.Errorf("%w: decode usage events", ErrDashboardProtocol)
	}
	return page, nil
}

func (client *DashboardClient) call(ctx context.Context, method string, payload []byte) ([]byte, error) {
	credential, err := client.tokenSource.ReadAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	target := *client.baseURL
	target.Path += "/aiserver.v1.DashboardService/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: create request", ErrDashboardProtocol)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("Content-Type", "application/proto")
	request.Header.Set("Connect-Protocol-Version", "1")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", ErrDashboardTransport)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, ErrDashboardAuthRejected
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/proto") {
		return nil, fmt.Errorf("%w: unexpected response status %d", ErrDashboardTransport, response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxDashboardResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > maxDashboardResponseBytes {
		return nil, fmt.Errorf("%w: invalid response body", ErrDashboardTransport)
	}
	return body, nil
}

func decodeCurrentPeriodUsage(message []byte) (CurrentPeriodUsage, error) {
	var usage CurrentPeriodUsage
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return CurrentPeriodUsage{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		switch number {
		case 1, 2, 6:
			value, length := protowire.ConsumeVarint(message)
			if length < 0 {
				return CurrentPeriodUsage{}, ErrDashboardProtocol
			}
			message = message[length:]
			switch number {
			case 1:
				usage.BillingCycleStartMS = int64(value)
			case 2:
				usage.BillingCycleEndMS = int64(value)
			case 6:
				usage.Enabled = value != 0
			}
		case 3:
			value, length := protowire.ConsumeBytes(message)
			if length < 0 {
				return CurrentPeriodUsage{}, ErrDashboardProtocol
			}
			message = message[length:]
			plan, err := decodeCurrentPlanUsage(value)
			if err != nil {
				return CurrentPeriodUsage{}, err
			}
			usage.PlanUsage = &plan
		default:
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return CurrentPeriodUsage{}, ErrDashboardProtocol
			}
			message = message[length:]
		}
	}
	return usage, nil
}

func decodeCurrentPlanUsage(message []byte) (CurrentPlanUsage, error) {
	var usage CurrentPlanUsage
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return CurrentPlanUsage{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		if (number < 1 || number > 5 || wireType != protowire.VarintType) &&
			(number != 12 && number != 13 || wireType != protowire.Fixed64Type) {
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return CurrentPlanUsage{}, ErrDashboardProtocol
			}
			message = message[length:]
			continue
		}
		if number == 12 || number == 13 {
			value, length := protowire.ConsumeFixed64(message)
			if length < 0 {
				return CurrentPlanUsage{}, ErrDashboardProtocol
			}
			percent := math.Float64frombits(value)
			if number == 12 {
				usage.CursorModelsUsedPercent = &percent
			} else {
				usage.OtherModelsUsedPercent = &percent
			}
			message = message[length:]
			continue
		}
		value, length := protowire.ConsumeVarint(message)
		if length < 0 {
			return CurrentPlanUsage{}, ErrDashboardProtocol
		}
		message = message[length:]
		switch number {
		case 1:
			usage.TotalSpendCents = int64(value)
		case 2:
			usage.IncludedSpendCents = int64(value)
		case 3:
			usage.BonusSpendCents = int64(value)
		case 4:
			usage.RemainingCents = int64(value)
		case 5:
			usage.LimitCents = int64(value)
		}
	}
	return usage, nil
}

func appendVarint(message []byte, number protowire.Number, value uint64) []byte {
	message = protowire.AppendTag(message, number, protowire.VarintType)
	return protowire.AppendVarint(message, value)
}

func decodeSandUsageStatus(message []byte) (GrokBotUsageStatus, error) {
	var status GrokBotUsageStatus
	var usagePercent float64
	var hasPeriodStart, hasNextReset, hasPercent, includedLimitZero, hasNonZeroIncludedLimit bool
	seen := make(map[protowire.Number]struct{}, 8)
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return GrokBotUsageStatus{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		if sandUsageSingularAllowlistField(number) {
			if _, duplicated := seen[number]; duplicated {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			seen[number] = struct{}{}
		}
		switch number {
		case 1, 2:
			if wireType != protowire.BytesType {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			value, length := protowire.ConsumeBytes(message)
			if length < 0 {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			timestampMS, err := decodeProtobufTimestamp(value)
			if err != nil {
				return GrokBotUsageStatus{}, err
			}
			if number == 1 {
				status.PeriodStartMS = timestampMS
				hasPeriodStart = true
			} else {
				status.NextResetAtMS = timestampMS
				hasNextReset = true
			}
			message = message[length:]
		case 3:
			if wireType != protowire.Fixed64Type {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			value, length := protowire.ConsumeFixed64(message)
			if length < 0 {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			usagePercent = math.Float64frombits(value)
			if math.IsNaN(usagePercent) || math.IsInf(usagePercent, 0) ||
				usagePercent < 0 || usagePercent > 100 {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			hasPercent = true
			message = message[length:]
		case 4, 8:
			if wireType != protowire.VarintType {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			value, length := protowire.ConsumeVarint(message)
			if length < 0 {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			if number == 4 {
				includedLimitZero = value != 0
			} else {
				hasNonZeroIncludedLimit = value != 0
			}
			message = message[length:]
		default:
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return GrokBotUsageStatus{}, ErrDashboardProtocol
			}
			message = message[length:]
		}
	}
	if !hasPeriodStart || !hasNextReset || status.NextResetAtMS <= status.PeriodStartMS {
		return GrokBotUsageStatus{}, ErrDashboardProtocol
	}
	if includedLimitZero && hasNonZeroIncludedLimit {
		return GrokBotUsageStatus{}, ErrDashboardProtocol
	}
	status.Included = hasNonZeroIncludedLimit && !includedLimitZero
	if status.Included && hasPercent {
		percent := usagePercent
		status.UsagePercent = &percent
	}
	return status, nil
}

func sandUsageSingularAllowlistField(number protowire.Number) bool {
	switch number {
	case 1, 2, 3, 4, 8:
		return true
	default:
		return false
	}
}

func decodeProtobufTimestamp(message []byte) (int64, error) {
	var seconds int64
	var nanos int32
	var hasSeconds bool
	seen := make(map[protowire.Number]struct{}, 2)
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return 0, ErrDashboardProtocol
		}
		message = message[tagLength:]
		if _, duplicated := seen[number]; duplicated {
			return 0, ErrDashboardProtocol
		}
		seen[number] = struct{}{}
		if number != 1 && number != 2 {
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return 0, ErrDashboardProtocol
			}
			message = message[length:]
			continue
		}
		if wireType != protowire.VarintType {
			return 0, ErrDashboardProtocol
		}
		value, length := protowire.ConsumeVarint(message)
		if length < 0 {
			return 0, ErrDashboardProtocol
		}
		message = message[length:]
		if number == 1 {
			if value > math.MaxInt64 {
				return 0, ErrDashboardProtocol
			}
			seconds = int64(value)
			hasSeconds = true
			continue
		}
		if value >= 1_000_000_000 {
			return 0, ErrDashboardProtocol
		}
		nanos = int32(value)
	}
	if !hasSeconds || seconds < 0 || seconds > math.MaxInt64/1000 {
		return 0, ErrDashboardProtocol
	}
	return seconds*1000 + int64(nanos)/1_000_000, nil
}

func decodeUsageEventsPage(message []byte) (UsageEventsPage, error) {
	var page UsageEventsPage
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return UsageEventsPage{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		switch number {
		case 2:
			value, length := protowire.ConsumeVarint(message)
			if length < 0 {
				return UsageEventsPage{}, ErrDashboardProtocol
			}
			page.TotalCount = int64(value)
			message = message[length:]
		case 3:
			value, length := protowire.ConsumeBytes(message)
			if length < 0 {
				return UsageEventsPage{}, ErrDashboardProtocol
			}
			event, err := decodeDashboardUsageEvent(value)
			if err != nil {
				return UsageEventsPage{}, err
			}
			page.Events = append(page.Events, event)
			message = message[length:]
		default:
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return UsageEventsPage{}, ErrDashboardProtocol
			}
			message = message[length:]
		}
	}
	return page, nil
}

func decodeDashboardUsageEvent(message []byte) (DashboardUsageEvent, error) {
	var event DashboardUsageEvent
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return DashboardUsageEvent{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		switch number {
		case 1, 3, 8:
			value, length := protowire.ConsumeVarint(message)
			if length < 0 {
				return DashboardUsageEvent{}, ErrDashboardProtocol
			}
			if number == 1 {
				event.OccurredAtMS = int64(value)
			} else if number == 3 {
				event.Kind = int32(value)
			} else {
				event.TokenBased = value != 0
			}
			message = message[length:]
		case 2, 23:
			value, length := protowire.ConsumeString(message)
			if length < 0 {
				return DashboardUsageEvent{}, ErrDashboardProtocol
			}
			if number == 2 {
				event.Model = value
			} else {
				event.ConversationID = value
			}
			message = message[length:]
		case 9:
			value, length := protowire.ConsumeBytes(message)
			if length < 0 {
				return DashboardUsageEvent{}, ErrDashboardProtocol
			}
			tokens, err := decodeDashboardTokenUsage(value)
			if err != nil {
				return DashboardUsageEvent{}, err
			}
			event.TokenUsage = &tokens
			message = message[length:]
		case 6, 13, 18:
			value, length := protowire.ConsumeFixed32(message)
			if length < 0 {
				return DashboardUsageEvent{}, ErrDashboardProtocol
			}
			amount := float64(math.Float32frombits(value))
			switch number {
			case 6:
				event.RequestCosts = amount
			case 13:
				event.CursorTokenFeeCents = amount
			case 18:
				event.ChargedCents = amount
			}
			message = message[length:]
		default:
			length := protowire.ConsumeFieldValue(number, wireType, message)
			if length < 0 {
				return DashboardUsageEvent{}, ErrDashboardProtocol
			}
			message = message[length:]
		}
	}
	return event, nil
}

func decodeDashboardTokenUsage(message []byte) (DashboardTokenUsage, error) {
	var usage DashboardTokenUsage
	for len(message) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(message)
		if tagLength < 0 {
			return DashboardTokenUsage{}, ErrDashboardProtocol
		}
		message = message[tagLength:]
		if number >= 1 && number <= 4 && wireType == protowire.VarintType {
			value, length := protowire.ConsumeVarint(message)
			if length < 0 {
				return DashboardTokenUsage{}, ErrDashboardProtocol
			}
			message = message[length:]
			switch number {
			case 1:
				usage.InputTokens = int64(value)
			case 2:
				usage.OutputTokens = int64(value)
			case 3:
				usage.CacheWriteTokens = int64(value)
			case 4:
				usage.CacheReadTokens = int64(value)
			}
			continue
		}
		if number == 5 && wireType == protowire.Fixed32Type {
			value, length := protowire.ConsumeFixed32(message)
			if length < 0 {
				return DashboardTokenUsage{}, ErrDashboardProtocol
			}
			usage.TotalCents = float64(math.Float32frombits(value))
			message = message[length:]
			continue
		}
		length := protowire.ConsumeFieldValue(number, wireType, message)
		if length < 0 {
			return DashboardTokenUsage{}, ErrDashboardProtocol
		}
		message = message[length:]
	}
	return usage, nil
}
