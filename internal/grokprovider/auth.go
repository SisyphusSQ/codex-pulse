package grokprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrAuthUnavailable     = errors.New("grok authentication is unavailable")
	ErrAuthExpired         = errors.New("grok authentication is expired")
	ErrBillingAuthRejected = errors.New("grok billing rejected credentials")
)

type AccountSnapshot struct {
	Email         string
	PrincipalType string
	AuthMode      string
	Subscription  string
}

type AccessToken struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

type AuthReader struct {
	path           string
	now            func() time.Time
	httpClient     *http.Client
	refreshBefore  time.Duration
	refreshEnabled func() bool
}

type AuthReaderConfig struct {
	RefreshEnabled func() bool
}

func NewAuthReader(path string, now func() time.Time, configs ...AuthReaderConfig) (*AuthReader, error) {
	if !filepath.IsAbs(path) || now == nil || len(configs) > 1 {
		return nil, ErrAuthUnavailable
	}
	enabled := func() bool { return true }
	if len(configs) == 1 && configs[0].RefreshEnabled != nil {
		enabled = configs[0].RefreshEnabled
	}
	return &AuthReader{
		path: filepath.Clean(path), now: now,
		httpClient: &http.Client{Timeout: billingHTTPTimeout()}, refreshBefore: 5 * time.Minute,
		refreshEnabled: enabled,
	}, nil
}

func (reader *AuthReader) ReadAccountSnapshot() (AccountSnapshot, error) {
	account, err := reader.loadAccount()
	if err != nil {
		return AccountSnapshot{}, err
	}
	return AccountSnapshot{
		Email:         account.Email,
		PrincipalType: account.PrincipalType,
		AuthMode:      account.AuthMode,
	}, nil
}

func (reader *AuthReader) ReadAccessToken() (AccessToken, error) {
	return reader.ReadAccessTokenContext(context.Background())
}

func (reader *AuthReader) ReadAccessTokenContext(ctx context.Context) (AccessToken, error) {
	return reader.resolveAccessToken(ctx, false, "")
}

func (reader *AuthReader) RefreshAccessToken(ctx context.Context, rejectedToken string) (AccessToken, error) {
	return reader.resolveAccessToken(ctx, true, rejectedToken)
}

func (reader *AuthReader) resolveAccessToken(
	ctx context.Context,
	force bool,
	rejectedToken string,
) (AccessToken, error) {
	if ctx == nil {
		return AccessToken{}, ErrAuthUnavailable
	}
	account, err := reader.loadAccount()
	if err != nil {
		return AccessToken{}, err
	}
	if account.Key == "" || account.UserID == "" {
		return AccessToken{}, ErrAuthUnavailable
	}
	now := reader.now()
	if force && rejectedToken != "" && account.Key != rejectedToken &&
		(account.ExpiresAt.IsZero() || account.ExpiresAt.After(now)) {
		return accessToken(account), nil
	}
	if !force && (account.ExpiresAt.IsZero() || account.ExpiresAt.After(now.Add(reader.refreshBefore))) {
		return accessToken(account), nil
	}
	if !reader.refreshEnabled() {
		if !force && account.ExpiresAt.After(now) {
			return accessToken(account), nil
		}
		return AccessToken{}, ErrAuthExpired
	}
	credential, err := reader.refreshAccessTokenLocked(ctx, account, now, force, rejectedToken)
	if err != nil && !force && ctx.Err() == nil && account.ExpiresAt.After(now) {
		return accessToken(account), nil
	}
	return credential, err
}

type grokAuthAccount struct {
	ScopeKey      string
	Email         string
	PrincipalType string
	AuthMode      string
	UserID        string
	Key           string
	RefreshToken  string
	OIDCIssuer    string
	OIDCClientID  string
	ExpiresAt     time.Time
}

func (reader *AuthReader) loadAccount() (grokAuthAccount, error) {
	if reader == nil || reader.path == "" {
		return grokAuthAccount{}, ErrAuthUnavailable
	}
	content, err := readRegularAuthFile(reader.path)
	if err != nil {
		return grokAuthAccount{}, ErrAuthUnavailable
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil || len(root) == 0 {
		return grokAuthAccount{}, ErrAuthUnavailable
	}
	var selected grokAuthAccount
	var selectedExpiry time.Time
	found := false
	for scopeKey, raw := range root {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		account := grokAuthAccount{
			ScopeKey:      scopeKey,
			Email:         whitelistString(payload, "email"),
			PrincipalType: whitelistString(payload, "principal_type"),
			AuthMode:      whitelistString(payload, "auth_mode"),
			UserID:        whitelistString(payload, "user_id"),
			Key:           whitelistString(payload, "key"),
			RefreshToken:  whitelistString(payload, "refresh_token"),
			OIDCIssuer:    whitelistString(payload, "oidc_issuer"),
			OIDCClientID:  whitelistString(payload, "oidc_client_id"),
		}
		if expiry := whitelistString(payload, "expires_at"); expiry != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, expiry); parseErr == nil {
				account.ExpiresAt = parsed
			}
		}
		if account.Email == "" && account.Key == "" {
			continue
		}
		if !found || account.ExpiresAt.After(selectedExpiry) {
			selected = account
			selectedExpiry = account.ExpiresAt
			found = true
		}
	}
	if !found {
		return grokAuthAccount{}, ErrAuthUnavailable
	}
	return selected, nil
}

func readRegularAuthFile(path string) ([]byte, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrAuthUnavailable
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, ErrAuthUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrAuthUnavailable
	}
	content, err := io.ReadAll(io.LimitReader(file, maxAuthResponseBytes+1))
	if err != nil || len(content) > maxAuthResponseBytes {
		return nil, ErrAuthUnavailable
	}
	return content, nil
}

func whitelistString(payload map[string]json.RawMessage, key string) string {
	raw, ok := payload[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}
