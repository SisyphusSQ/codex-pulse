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
)

type TokenSource interface {
	ReadAccessToken() (AccessToken, error)
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
	credential, err := client.tokenSource.ReadAccessToken()
	if err != nil {
		return BillingCredits{}, err
	}
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
	CreditUsagePercent   *float64       `json:"creditUsagePercent"`
	CurrentPeriod        *billingPeriod `json:"currentPeriod"`
	OnDemandUsed         *float64       `json:"onDemandUsed"`
	OnDemandCap          *float64       `json:"onDemandCap"`
	PrepaidBalance       *float64       `json:"prepaidBalance"`
	SubscriptionTier     *string        `json:"subscriptionTier"`
	IsUnifiedBillingUser *bool          `json:"isUnifiedBillingUser"`
	MonthlyLimit         *float64       `json:"monthlyLimit"`
	Used                 *float64       `json:"used"`
}

type billingPeriod struct {
	Type  string          `json:"type"`
	Start json.RawMessage `json:"start"`
	End   json.RawMessage `json:"end"`
}

func decodeBillingCredits(body []byte) (BillingCredits, error) {
	var payload billingCreditsDTO
	if err := json.Unmarshal(body, &payload); err != nil {
		return BillingCredits{}, fmt.Errorf("%w: decode credits", ErrBillingProtocol)
	}
	used, ok := creditUsedPercent(payload)
	if !ok {
		return BillingCredits{}, fmt.Errorf("%w: credit percent missing", ErrBillingProtocol)
	}
	periodType, startMS, endMS, ok := decodeBillingPeriod(payload.CurrentPeriod)
	if !ok {
		return BillingCredits{}, fmt.Errorf("%w: billing period missing", ErrBillingProtocol)
	}
	result := BillingCredits{
		UsedPercent: used, PeriodType: periodType, PeriodStartMS: startMS, PeriodEndMS: endMS,
		OnDemandUsed:   finiteOptional(payload.OnDemandUsed),
		OnDemandCap:    finiteOptional(payload.OnDemandCap),
		PrepaidBalance: finiteOptional(payload.PrepaidBalance),
	}
	if payload.SubscriptionTier != nil {
		if label := normalizeLabel(*payload.SubscriptionTier); label != "" {
			result.SubscriptionTier = &label
		}
	}
	if payload.IsUnifiedBillingUser != nil {
		result.IsUnifiedBilling = *payload.IsUnifiedBillingUser
	}
	return result, nil
}

func creditUsedPercent(payload billingCreditsDTO) (float64, bool) {
	if payload.CreditUsagePercent != nil {
		return finitePercent(*payload.CreditUsagePercent)
	}
	if payload.MonthlyLimit != nil && payload.Used != nil && *payload.MonthlyLimit > 0 &&
		!math.IsNaN(*payload.MonthlyLimit) && !math.IsInf(*payload.MonthlyLimit, 0) &&
		!math.IsNaN(*payload.Used) && !math.IsInf(*payload.Used, 0) && *payload.Used >= 0 {
		return finitePercent(*payload.Used / *payload.MonthlyLimit * 100)
	}
	return 0, false
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
