package grokprovider

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrAuthUnavailable = errors.New("grok authentication is unavailable")
	ErrAuthExpired     = errors.New("grok authentication is expired")
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
	path string
	now  func() time.Time
}

func NewAuthReader(path string, now func() time.Time) (*AuthReader, error) {
	if !filepath.IsAbs(path) || now == nil {
		return nil, ErrAuthUnavailable
	}
	return &AuthReader{path: filepath.Clean(path), now: now}, nil
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
	account, err := reader.loadAccount()
	if err != nil {
		return AccessToken{}, err
	}
	if account.Key == "" || account.UserID == "" {
		return AccessToken{}, ErrAuthUnavailable
	}
	if !account.ExpiresAt.IsZero() && !account.ExpiresAt.After(reader.now()) {
		return AccessToken{}, ErrAuthExpired
	}
	return AccessToken{Token: account.Key, UserID: account.UserID, ExpiresAt: account.ExpiresAt}, nil
}

type grokAuthAccount struct {
	Email         string
	PrincipalType string
	AuthMode      string
	UserID        string
	Key           string
	ExpiresAt     time.Time
}

func (reader *AuthReader) loadAccount() (grokAuthAccount, error) {
	if reader == nil || reader.path == "" {
		return grokAuthAccount{}, ErrAuthUnavailable
	}
	content, err := os.ReadFile(reader.path)
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
	for _, raw := range root {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		account := grokAuthAccount{
			Email:         whitelistString(payload, "email"),
			PrincipalType: whitelistString(payload, "principal_type"),
			AuthMode:      whitelistString(payload, "auth_mode"),
			UserID:        whitelistString(payload, "user_id"),
			Key:           whitelistString(payload, "key"),
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
