package grokprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrBillingProtocol = errors.New("grok billing protocol is unavailable")

const (
	maxBillingResponseBytes = 1 << 20
	grokCreditsLimitID      = "grok.included_credits"
	grokOnDemandLimitID     = "grok.on_demand"
)

type TokenSource interface {
	ReadAccessToken() (AccessToken, error)
}

type contextualTokenSource interface {
	ReadAccessTokenContext(context.Context) (AccessToken, error)
}

type refreshingTokenSource interface {
	RefreshAccessToken(context.Context, string) (AccessToken, error)
}

type BillingClientConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	TokenSource TokenSource
}

type BillingClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	tokenSource TokenSource
}

type BillingCredits struct {
	UsedPercent      float64
	PeriodType       string
	PeriodStartMS    int64
	PeriodEndMS      int64
	OnDemandUsed     *float64
	OnDemandCap      *float64
	PrepaidBalance   *float64
	SubscriptionTier *string
	IsUnifiedBilling bool
}

func NewBillingClient(config BillingClientConfig) (*BillingClient, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || config.HTTPClient == nil || config.TokenSource == nil {
		return nil, ErrBillingProtocol
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &BillingClient{baseURL: parsed, httpClient: config.HTTPClient, tokenSource: config.TokenSource}, nil
}

func (client *BillingClient) GetCredits(ctx context.Context) (BillingCredits, error) {
	if client == nil || ctx == nil {
		return BillingCredits{}, ErrBillingProtocol
	}
	credential, err := client.accessToken(ctx)
	if err != nil {
		return BillingCredits{}, err
	}
	credits, err := client.getCredits(ctx, credential)
	if !errors.Is(err, ErrAuthExpired) {
		return credits, err
	}
	refresher, ok := client.tokenSource.(refreshingTokenSource)
	if !ok {
		return BillingCredits{}, err
	}
	credential, err = refresher.RefreshAccessToken(ctx, credential.Token)
	if err != nil {
		return BillingCredits{}, err
	}
	return client.getCredits(ctx, credential)
}

func (client *BillingClient) accessToken(ctx context.Context) (AccessToken, error) {
	return accessTokenFromSource(ctx, client.tokenSource)
}

func (client *BillingClient) getCredits(ctx context.Context, credential AccessToken) (BillingCredits, error) {
	target := *client.baseURL
	target.Path += "/billing"
	query := target.Query()
	query.Set("format", "credits")
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return BillingCredits{}, fmt.Errorf("%w: create request", ErrBillingProtocol)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("x-userid", credential.UserID)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return BillingCredits{}, fmt.Errorf("%w: request failed", ErrBillingProtocol)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBillingResponseBytes+1))
	if err != nil {
		return BillingCredits{}, fmt.Errorf("%w: read response", ErrBillingProtocol)
	}
	if len(body) > maxBillingResponseBytes {
		return BillingCredits{}, fmt.Errorf("%w: response too large", ErrBillingProtocol)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return BillingCredits{}, ErrAuthExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return BillingCredits{}, fmt.Errorf("%w: unexpected status", ErrBillingProtocol)
	}
	credits, err := decodeBillingCredits(body)
	if err != nil {
		return BillingCredits{}, err
	}
	return credits, nil
}

type billingCreditsDTO struct {
	CreditUsagePercent   json.RawMessage `json:"creditUsagePercent"`
	CurrentPeriod        *billingPeriod  `json:"currentPeriod"`
	OnDemandUsed         json.RawMessage `json:"onDemandUsed"`
	OnDemandCap          json.RawMessage `json:"onDemandCap"`
	PrepaidBalance       json.RawMessage `json:"prepaidBalance"`
	SubscriptionTier     *string         `json:"subscriptionTier"`
	SubscriptionTierWire *string         `json:"subscription_tier"`
	IsUnifiedBillingUser *bool           `json:"isUnifiedBillingUser"`
	MonthlyLimit         json.RawMessage `json:"monthlyLimit"`
	Used                 json.RawMessage `json:"used"`
}

type billingPeriod struct {
	Type  string          `json:"type"`
	Start json.RawMessage `json:"start"`
	End   json.RawMessage `json:"end"`
}

func decodeBillingCredits(body []byte) (BillingCredits, error) {
	payload, tier, err := unwrapBillingPayload(body)
	if err != nil {
		return BillingCredits{}, err
	}
	used, ok := creditUsedPercent(payload)
	if !ok {
		return BillingCredits{}, fmt.Errorf("%w: credit percent missing", ErrBillingProtocol)
	}
	periodType, startMS, endMS, ok := decodeBillingPeriod(payload.CurrentPeriod)
	if !ok {
		return BillingCredits{}, fmt.Errorf("%w: billing period missing", ErrBillingProtocol)
	}
	if payloadTier := firstBillingSubscriptionTier(
		payload.SubscriptionTierWire,
		payload.SubscriptionTier,
	); payloadTier != nil {
		tier = payloadTier
	}
	result := BillingCredits{
		UsedPercent: used, PeriodType: periodType, PeriodStartMS: startMS, PeriodEndMS: endMS,
		OnDemandUsed:   decodeFlexibleAmount(payload.OnDemandUsed),
		OnDemandCap:    decodeFlexibleAmount(payload.OnDemandCap),
		PrepaidBalance: decodeFlexibleAmount(payload.PrepaidBalance),
	}
	if tier != nil {
		if label := normalizeLabel(*tier); label != "" {
			result.SubscriptionTier = &label
		}
	}
	if payload.IsUnifiedBillingUser != nil {
		result.IsUnifiedBilling = *payload.IsUnifiedBillingUser
	}
	return result, nil
}

func unwrapBillingPayload(body []byte) (billingCreditsDTO, *string, error) {
	var envelope struct {
		Config               json.RawMessage `json:"config"`
		SubscriptionTier     *string         `json:"subscriptionTier"`
		SubscriptionTierWire *string         `json:"subscription_tier"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return billingCreditsDTO{}, nil, fmt.Errorf("%w: decode credits", ErrBillingProtocol)
	}
	payloadBytes := body
	if len(envelope.Config) > 0 && string(envelope.Config) != "null" {
		payloadBytes = envelope.Config
	}
	var payload billingCreditsDTO
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return billingCreditsDTO{}, nil, fmt.Errorf("%w: decode credits", ErrBillingProtocol)
	}
	return payload, firstBillingSubscriptionTier(
		envelope.SubscriptionTierWire,
		envelope.SubscriptionTier,
	), nil
}

func firstBillingSubscriptionTier(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

func creditUsedPercent(payload billingCreditsDTO) (float64, bool) {
	if percent := decodeFlexibleNumber(payload.CreditUsagePercent); percent != nil {
		return finitePercent(*percent)
	}
	limit := decodeFlexibleNumber(payload.MonthlyLimit)
	used := decodeFlexibleNumber(payload.Used)
	if limit != nil && used != nil && *limit > 0 && *used >= 0 {
		return finitePercent(*used / *limit * 100)
	}
	return 0, false
}

func decodeFlexibleNumber(raw json.RawMessage) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return &number
	}
	var object struct {
		Val float64 `json:"val"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return &object.Val
	}
	return nil
}

func decodeFlexibleAmount(raw json.RawMessage) *float64 {
	return finiteOptional(decodeFlexibleNumber(raw))
}

func decodeBillingPeriod(period *billingPeriod) (string, int64, int64, bool) {
	if period == nil {
		return "", 0, 0, false
	}
	kind := strings.ToUpper(strings.TrimSpace(period.Type))
	mapped := ""
	switch kind {
	case "USAGE_PERIOD_TYPE_WEEKLY", "WEEKLY", "WEEK":
		mapped = "weekly"
	case "USAGE_PERIOD_TYPE_MONTHLY", "MONTHLY", "MONTH":
		mapped = "monthly"
	default:
		return "", 0, 0, false
	}
	startMS := parseJSONTimestamp(period.Start)
	endMS := parseJSONTimestamp(period.End)
	if startMS <= 0 || endMS <= startMS {
		return "", 0, 0, false
	}
	return mapped, startMS, endMS, true
}

func finitePercent(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return 0, false
	}
	return value, true
}

func finiteOptional(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil
	}
	copied := *value
	return &copied
}

func billingFailureCode(err error) string {
	if errors.Is(err, ErrAuthExpired) || errors.Is(err, ErrAuthUnavailable) {
		return "auth_expired"
	}
	if errors.Is(err, ErrBillingProtocol) {
		return "schema_incompatible"
	}
	return "read_failed"
}

func billingHTTPTimeout() time.Duration { return 15 * time.Second }
