package apisubscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	deepSeekBaseURL   = "https://api.deepseek.com"
	openCodeGoBaseURL = "https://opencode.ai"
	maximumBodyBytes  = 1 << 20
	defaultTimeout    = 10 * time.Second
)

var moneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

type ClientConfig struct {
	BaseURL     string
	Transport   http.RoundTripper
	Credentials APIKeyProvider
	Timeout     time.Duration
}

type apiClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	credentials APIKeyProvider
	service     string
}

type DeepSeekClient struct {
	base apiClient
}

type OpenCodeGoClient struct {
	base apiClient
}

func NewDeepSeekClient(config ClientConfig) (*DeepSeekClient, error) {
	client, err := newAPIClient(config, ServiceDeepSeek, deepSeekBaseURL)
	if err != nil {
		return nil, err
	}
	return &DeepSeekClient{base: client}, nil
}

func NewOpenCodeGoClient(config ClientConfig) (*OpenCodeGoClient, error) {
	client, err := newAPIClient(config, ServiceOpenCodeGo, openCodeGoBaseURL)
	if err != nil {
		return nil, err
	}
	return &OpenCodeGoClient{base: client}, nil
}

func newAPIClient(config ClientConfig, service string, defaultBaseURL string) (apiClient, error) {
	if config.Credentials == nil {
		return apiClient{}, errors.New("API subscription credentials are required")
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return apiClient{}, errors.New("API subscription base URL is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return apiClient{}, errors.New("API subscription base URL must not contain a path")
	}
	parsed.Path = ""

	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < 0 {
		return apiClient{}, errors.New("API subscription timeout must not be negative")
	}
	transport := config.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return apiClient{
		baseURL: parsed,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		credentials: config.Credentials,
		service:     service,
	}, nil
}

func (client *DeepSeekClient) GetBalance(ctx context.Context) (Balance, error) {
	body, err := client.base.get(ctx, "/user/balance")
	if err != nil {
		return Balance{}, err
	}
	var response struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency        string `json:"currency"`
			TotalBalance    string `json:"total_balance"`
			GrantedBalance  string `json:"granted_balance"`
			ToppedUpBalance string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &response); err != nil || len(response.BalanceInfos) == 0 {
		return Balance{}, fmt.Errorf("%w: DeepSeek balance payload", ErrProtocol)
	}
	result := Balance{IsAvailable: response.IsAvailable, Balances: make([]CurrencyBalance, 0, len(response.BalanceInfos))}
	seenCurrencies := make(map[string]struct{}, len(response.BalanceInfos))
	for _, item := range response.BalanceInfos {
		if !validCurrency(item.Currency) || !validMoney(item.TotalBalance) ||
			!validMoney(item.GrantedBalance) || !validMoney(item.ToppedUpBalance) {
			return Balance{}, fmt.Errorf("%w: DeepSeek balance fields", ErrProtocol)
		}
		if _, exists := seenCurrencies[item.Currency]; exists {
			return Balance{}, fmt.Errorf("%w: duplicate DeepSeek currency", ErrProtocol)
		}
		seenCurrencies[item.Currency] = struct{}{}
		result.Balances = append(result.Balances, CurrencyBalance{
			Currency: item.Currency, Total: item.TotalBalance, Granted: item.GrantedBalance,
			ToppedUp: item.ToppedUpBalance,
		})
	}
	return result, nil
}

func (client *OpenCodeGoClient) GetQuota(ctx context.Context) (Quota, error) {
	body, err := client.base.get(ctx, "/zen/go/v1/usage")
	if err != nil {
		return Quota{}, err
	}
	type wireWindow struct {
		Status   string   `json:"status"`
		Percent  *float64 `json:"percent"`
		ResetsAt string   `json:"resetsAt"`
	}
	var response struct {
		Usage *struct {
			Rolling *wireWindow `json:"rolling"`
			Weekly  *wireWindow `json:"weekly"`
			Monthly *wireWindow `json:"monthly"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Usage == nil {
		return Quota{}, fmt.Errorf("%w: OpenCode Go quota payload", ErrProtocol)
	}
	windows := []struct {
		kind string
		wire *wireWindow
	}{
		{WindowFiveHour, response.Usage.Rolling},
		{WindowWeekly, response.Usage.Weekly},
		{WindowMonthly, response.Usage.Monthly},
	}
	result := Quota{Windows: make([]QuotaWindow, 0, len(windows))}
	for _, window := range windows {
		if window.wire == nil || window.wire.Percent == nil ||
			(window.wire.Status != StatusOK && window.wire.Status != StatusRateLimited) ||
			math.IsNaN(*window.wire.Percent) || math.IsInf(*window.wire.Percent, 0) ||
			*window.wire.Percent < 0 || *window.wire.Percent > 100 {
			return Quota{}, fmt.Errorf("%w: OpenCode Go quota window", ErrProtocol)
		}
		reset, err := time.Parse(time.RFC3339, window.wire.ResetsAt)
		if err != nil {
			return Quota{}, fmt.Errorf("%w: OpenCode Go reset time", ErrProtocol)
		}
		result.Windows = append(result.Windows, QuotaWindow{
			Kind: window.kind, Status: window.wire.Status, UsedPercent: *window.wire.Percent,
			RemainingPercent: 100 - *window.wire.Percent, ResetsAtMS: reset.UnixMilli(),
		})
	}
	return result, nil
}

func (client apiClient) get(ctx context.Context, path string) ([]byte, error) {
	key, ok := client.credentials.APIKey(client.service)
	if !ok || len(key) == 0 {
		return nil, ErrUnconfigured
	}
	defer clear(key)

	target := *client.baseURL
	target.Path = path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create API subscription request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+string(key))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request API subscription: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, classifyStatus(response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read API subscription response: %w", err)
	}
	if len(body) > maximumBodyBytes {
		return nil, fmt.Errorf("%w: API subscription response is too large", ErrProtocol)
	}
	return body, nil
}

func classifyStatus(status int) error {
	switch {
	case status == http.StatusUnauthorized:
		return ErrAuth
	case status == http.StatusForbidden:
		return ErrForbidden
	case status == http.StatusTooManyRequests:
		return ErrRateLimit
	case status >= 500:
		return ErrServer
	default:
		return fmt.Errorf("%w: unexpected HTTP status %d", ErrProtocol, status)
	}
}

func validCurrency(value string) bool {
	if len(value) != 3 || value != strings.ToUpper(value) {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validMoney(value string) bool {
	return moneyPattern.MatchString(value)
}
