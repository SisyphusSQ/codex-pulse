package cursorprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrDesktopAuthUnavailable = errors.New("cursor desktop authentication is unavailable")
	ErrDesktopAuthExpired     = errors.New("cursor desktop authentication is expired")
)

// DesktopAccessToken is a validated Cursor Desktop credential kept only in memory.
type DesktopAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

// DesktopAuthReader reads Cursor's current access token from its WAL-aware state database.
type DesktopAuthReader struct {
	path string
	now  func() time.Time
}

func NewDesktopAuthReader(path string, now func() time.Time) (*DesktopAuthReader, error) {
	if !filepath.IsAbs(path) || now == nil {
		return nil, ErrDesktopAuthUnavailable
	}
	return &DesktopAuthReader{path: filepath.Clean(path), now: now}, nil
}

func (reader *DesktopAuthReader) ReadAccessToken(ctx context.Context) (DesktopAccessToken, error) {
	if reader == nil || ctx == nil {
		return DesktopAccessToken{}, ErrDesktopAuthUnavailable
	}
	database, transaction, _, err := openReadSnapshot(ctx, reader.path)
	if err != nil {
		return DesktopAccessToken{}, fmt.Errorf("%w: open state database", ErrDesktopAuthUnavailable)
	}
	defer database.Close()
	defer transaction.Rollback()
	if !hasColumns(ctx, transaction, "ItemTable", "key", "value") {
		return DesktopAccessToken{}, fmt.Errorf("%w: incompatible state schema", ErrDesktopAuthUnavailable)
	}

	var token string
	if err := transaction.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`,
	).Scan(&token); err != nil {
		return DesktopAccessToken{}, fmt.Errorf("%w: access token missing", ErrDesktopAuthUnavailable)
	}
	token = strings.TrimSpace(token)
	expiresAt, err := jwtExpiration(token)
	if err != nil {
		return DesktopAccessToken{}, fmt.Errorf("%w: malformed access token", ErrDesktopAuthUnavailable)
	}
	if !expiresAt.After(reader.now()) {
		return DesktopAccessToken{}, ErrDesktopAuthExpired
	}
	return DesktopAccessToken{Token: token, ExpiresAt: expiresAt}, nil
}

func jwtExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return time.Time{}, ErrDesktopAuthUnavailable
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ExpiresAt <= 0 {
		return time.Time{}, ErrDesktopAuthUnavailable
	}
	return time.Unix(claims.ExpiresAt, 0), nil
}
