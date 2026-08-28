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
	ErrDashboardAuthRejected  = errors.New("cursor dashboard rejected credentials")
)

// DesktopAccessToken is a validated Cursor Desktop credential kept only in memory.
type DesktopAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

// DesktopAccountSnapshot contains only user-facing identity and subscription
// facts. Tokens and other credential material are deliberately excluded.
type DesktopAccountSnapshot struct {
	Email              string
	MembershipType     string
	SubscriptionStatus string
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

func (reader *DesktopAuthReader) ReadAccountSnapshot(ctx context.Context) (DesktopAccountSnapshot, error) {
	if reader == nil || ctx == nil {
		return DesktopAccountSnapshot{}, ErrDesktopAuthUnavailable
	}
	database, transaction, _, err := openReadSnapshot(ctx, reader.path)
	if err != nil {
		return DesktopAccountSnapshot{}, fmt.Errorf("%w: open state database", ErrDesktopAuthUnavailable)
	}
	defer database.Close()
	defer transaction.Rollback()
	if !hasColumns(ctx, transaction, "ItemTable", "key", "value") {
		return DesktopAccountSnapshot{}, fmt.Errorf("%w: incompatible state schema", ErrDesktopAuthUnavailable)
	}
	values := make(map[string]string, 3)
	rows, err := transaction.QueryContext(ctx, `
		SELECT key, value FROM ItemTable
		WHERE key IN (
			'cursorAuth/cachedEmail',
			'cursorAuth/stripeMembershipType',
			'cursorAuth/stripeSubscriptionStatus'
		)`)
	if err != nil {
		return DesktopAccountSnapshot{}, fmt.Errorf("%w: account fields unavailable", ErrDesktopAuthUnavailable)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return DesktopAccountSnapshot{}, fmt.Errorf("%w: read account fields", ErrDesktopAuthUnavailable)
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := rows.Err(); err != nil {
		return DesktopAccountSnapshot{}, fmt.Errorf("%w: read account fields", ErrDesktopAuthUnavailable)
	}
	return DesktopAccountSnapshot{
		Email:              values["cursorAuth/cachedEmail"],
		MembershipType:     values["cursorAuth/stripeMembershipType"],
		SubscriptionStatus: values["cursorAuth/stripeSubscriptionStatus"],
	}, nil
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
