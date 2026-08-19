package grokprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var ErrAccountProtocol = errors.New("grok account protocol is unavailable")

const maxAccountResponseBytes = 1 << 20

type AccountClientConfig struct {
	BaseURL     string
	HTTPClient  *http.Client
	TokenSource TokenSource
}

type AccountClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	tokenSource TokenSource
}

func NewAccountClient(config AccountClientConfig) (*AccountClient, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || config.HTTPClient == nil || config.TokenSource == nil {
		return nil, ErrAccountProtocol
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &AccountClient{
		baseURL: parsed, httpClient: config.HTTPClient, tokenSource: config.TokenSource,
	}, nil
}

func (client *AccountClient) GetAccount(ctx context.Context) (AccountSnapshot, error) {
	if client == nil || ctx == nil {
		return AccountSnapshot{}, ErrAccountProtocol
	}
	credential, err := accessTokenFromSource(ctx, client.tokenSource)
	if err != nil {
		return AccountSnapshot{}, err
	}
	account, err := client.getAccount(ctx, credential)
	if !errors.Is(err, ErrAuthExpired) {
		return account, err
	}
	refresher, ok := client.tokenSource.(refreshingTokenSource)
	if !ok {
		return AccountSnapshot{}, err
	}
	credential, err = refresher.RefreshAccessToken(ctx, credential.Token)
	if err != nil {
		return AccountSnapshot{}, err
	}
	return client.getAccount(ctx, credential)
}

func (client *AccountClient) getAccount(ctx context.Context, credential AccessToken) (AccountSnapshot, error) {
	target := *client.baseURL
	target.Path += "/user"
	query := target.Query()
	query.Set("include", "subscription")
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("%w: create request", ErrAccountProtocol)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("x-userid", credential.UserID)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("%w: request failed", ErrAccountProtocol)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return AccountSnapshot{}, ErrAuthExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AccountSnapshot{}, fmt.Errorf("%w: unexpected status", ErrAccountProtocol)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAccountResponseBytes+1))
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("%w: read response", ErrAccountProtocol)
	}
	if len(body) > maxAccountResponseBytes {
		return AccountSnapshot{}, fmt.Errorf("%w: response too large", ErrAccountProtocol)
	}
	var payload struct {
		Email            string `json:"email"`
		PrincipalType    string `json:"principalType"`
		SubscriptionTier string `json:"subscriptionTier"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AccountSnapshot{}, fmt.Errorf("%w: decode response", ErrAccountProtocol)
	}
	account := AccountSnapshot{
		Email:         strings.TrimSpace(payload.Email),
		PrincipalType: strings.TrimSpace(payload.PrincipalType),
		Subscription:  normalizeLabel(payload.SubscriptionTier),
	}
	if account.Email == "" && account.Subscription == "" {
		return AccountSnapshot{}, fmt.Errorf("%w: identity missing", ErrAccountProtocol)
	}
	return account, nil
}

func accessTokenFromSource(ctx context.Context, source TokenSource) (AccessToken, error) {
	if contextual, ok := source.(contextualTokenSource); ok {
		return contextual.ReadAccessTokenContext(ctx)
	}
	return source.ReadAccessToken()
}
