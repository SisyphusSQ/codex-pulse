package grokprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

var ErrAuthRefreshFailed = errors.New("grok authentication refresh failed")

var errCredentialChanged = errors.New("grok credential changed during refresh")

const (
	maxAuthResponseBytes = 1 << 20
	authLockTimeout      = 15 * time.Second
	authLockPollInterval = 50 * time.Millisecond
)

type authFileLock struct {
	file      *os.File
	closeFile func() error
}

type oidcMetadata struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
}

type oidcTokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    json.RawMessage `json:"expires_in"`
}

func accessToken(account grokAuthAccount) AccessToken {
	return AccessToken{Token: account.Key, UserID: account.UserID, ExpiresAt: account.ExpiresAt}
}

func (reader *AuthReader) refreshAccessTokenLocked(
	ctx context.Context,
	observed grokAuthAccount,
	now time.Time,
	force bool,
	rejectedToken string,
) (AccessToken, error) {
	lock, err := acquireAuthFileLock(ctx, reader.path)
	if err != nil {
		return AccessToken{}, ErrAuthRefreshFailed
	}
	defer lock.release()

	current, err := reader.loadAccount()
	if err != nil {
		return AccessToken{}, err
	}
	if current.Key == "" || current.UserID == "" {
		return AccessToken{}, ErrAuthUnavailable
	}
	if current.ScopeKey != observed.ScopeKey {
		if current.ExpiresAt.IsZero() || current.ExpiresAt.After(now) {
			return accessToken(current), nil
		}
		return AccessToken{}, ErrAuthExpired
	}
	if force && rejectedToken != "" && current.Key != rejectedToken &&
		(current.ExpiresAt.IsZero() || current.ExpiresAt.After(now)) {
		return accessToken(current), nil
	}
	if !force && (current.ExpiresAt.IsZero() || current.ExpiresAt.After(now.Add(reader.refreshBefore))) {
		return accessToken(current), nil
	}
	refreshed, err := reader.refreshOIDCCredential(ctx, current, now)
	if err != nil {
		return AccessToken{}, err
	}
	if ctx.Err() != nil {
		return AccessToken{}, ctx.Err()
	}
	if !reader.refreshEnabled() {
		return AccessToken{}, ErrAuthRefreshFailed
	}
	if err := reader.persistRefreshedCredential(current, refreshed); err != nil {
		if errors.Is(err, errCredentialChanged) {
			latest, readErr := reader.loadAccount()
			if readErr == nil && latest.Key != "" && latest.UserID != "" && (latest.ExpiresAt.IsZero() || latest.ExpiresAt.After(now)) && (!force || latest.Key != rejectedToken) {
				return accessToken(latest), nil
			}
		}
		return AccessToken{}, ErrAuthRefreshFailed
	}
	return accessToken(refreshed), nil
}

func acquireAuthFileLock(ctx context.Context, authPath string) (*authFileLock, error) {
	if ctx == nil || authPath == "" {
		return nil, ErrAuthRefreshFailed
	}
	lockPath := filepath.Join(filepath.Dir(authPath), "auth.json.lock")
	deadline := time.Now().Add(authLockTimeout)
	for {
		file, closeFile, err := openAuthLockFile(lockPath)
		if err != nil {
			return nil, ErrAuthRefreshFailed
		}
		locked, err := platformfs.TryLock(file)
		if locked && authLockInodeIsLive(file, lockPath) {
			if writeAuthLockHolder(file) != nil {
				_ = platformfs.Unlock(file)
				_ = closeFile()
				return nil, ErrAuthRefreshFailed
			}
			return &authFileLock{file: file, closeFile: closeFile}, nil
		}
		if locked {
			_ = platformfs.Unlock(file)
		}
		_ = closeFile()
		if err != nil {
			return nil, ErrAuthRefreshFailed
		}
		if !time.Now().Before(deadline) {
			return nil, ErrAuthRefreshFailed
		}
		timer := time.NewTimer(authLockPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ErrAuthRefreshFailed
		case <-timer.C:
		}
	}
}

func authLockInodeIsLive(file *os.File, path string) bool {
	fileInfo, fileErr := file.Stat()
	pathInfo, pathErr := os.Stat(path)
	return fileErr == nil && pathErr == nil && os.SameFile(fileInfo, pathInfo)
}

func writeAuthLockHolder(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "%d:%d", os.Getpid(), time.Now().Unix()); err != nil {
		return err
	}
	return file.Sync()
}

func (lock *authFileLock) release() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = platformfs.Unlock(lock.file)
	_ = lock.closeFile()
}

func (reader *AuthReader) refreshOIDCCredential(
	ctx context.Context,
	account grokAuthAccount,
	now time.Time,
) (grokAuthAccount, error) {
	if account.RefreshToken == "" || account.OIDCIssuer == "" || account.OIDCClientID == "" ||
		reader.httpClient == nil {
		return grokAuthAccount{}, ErrAuthExpired
	}
	issuer, err := parseOIDCEndpoint(account.OIDCIssuer)
	if err != nil {
		return grokAuthAccount{}, ErrAuthExpired
	}
	discovery := *issuer
	discovery.Path = strings.TrimRight(discovery.Path, "/") + "/.well-known/openid-configuration"
	discovery.RawQuery = ""
	discovery.Fragment = ""

	var metadata oidcMetadata
	if err := reader.getAuthJSON(ctx, discovery.String(), &metadata); err != nil {
		return grokAuthAccount{}, err
	}
	if strings.TrimRight(metadata.Issuer, "/") != strings.TrimRight(account.OIDCIssuer, "/") {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	tokenEndpoint, err := parseOIDCEndpoint(metadata.TokenEndpoint)
	if err != nil {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {account.RefreshToken},
		"client_id":     {account.OIDCClientID},
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, tokenEndpoint.String(), strings.NewReader(form.Encode()),
	)
	if err != nil {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := reader.doAuthRequest(request)
	if err != nil {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden {
		return grokAuthAccount{}, ErrAuthExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	var token oidcTokenResponse
	if err := decodeLimitedAuthJSON(response.Body, &token); err != nil {
		return grokAuthAccount{}, err
	}
	expiresIn, ok := decodeExpiresIn(token.ExpiresIn)
	if strings.TrimSpace(token.AccessToken) == "" || !strings.EqualFold(strings.TrimSpace(token.TokenType), "Bearer") || !ok {
		return grokAuthAccount{}, ErrAuthRefreshFailed
	}
	account.Key = strings.TrimSpace(token.AccessToken)
	if replacement := strings.TrimSpace(token.RefreshToken); replacement != "" {
		account.RefreshToken = replacement
	}
	account.ExpiresAt = now.Add(expiresIn)
	return account, nil
}

func (reader *AuthReader) getAuthJSON(ctx context.Context, target string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ErrAuthRefreshFailed
	}
	request.Header.Set("Accept", "application/json")
	response, err := reader.doAuthRequest(request)
	if err != nil {
		return ErrAuthRefreshFailed
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrAuthRefreshFailed
	}
	return decodeLimitedAuthJSON(response.Body, destination)
}

func (reader *AuthReader) doAuthRequest(request *http.Request) (*http.Response, error) {
	if reader == nil || reader.httpClient == nil || request == nil || request.URL == nil {
		return nil, ErrAuthRefreshFailed
	}
	client := *reader.httpClient
	previous := client.CheckRedirect
	origin := request.URL
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next == nil || next.URL == nil || !sameOrigin(origin, next.URL) {
			return http.ErrUseLastResponse
		}
		if previous != nil {
			return previous(next, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return client.Do(request)
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}

func decodeLimitedAuthJSON(body io.Reader, destination any) error {
	content, err := io.ReadAll(io.LimitReader(body, maxAuthResponseBytes+1))
	if err != nil || len(content) > maxAuthResponseBytes || json.Unmarshal(content, destination) != nil {
		return ErrAuthRefreshFailed
	}
	return nil
}

func decodeExpiresIn(raw json.RawMessage) (time.Duration, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var seconds int64
	if json.Unmarshal(raw, &seconds) != nil {
		var encoded string
		if json.Unmarshal(raw, &encoded) != nil {
			return 0, false
		}
		parsed, err := strconv.ParseInt(encoded, 10, 64)
		if err != nil {
			return 0, false
		}
		seconds = parsed
	}
	if seconds <= 0 || seconds > int64((30*24*time.Hour)/time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func parseOIDCEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrAuthRefreshFailed
	}
	if parsed.Scheme == "https" {
		return parsed, nil
	}
	host := parsed.Hostname()
	if parsed.Scheme == "http" && (strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()) {
		return parsed, nil
	}
	return nil, ErrAuthRefreshFailed
}

func (reader *AuthReader) persistRefreshedCredential(expected grokAuthAccount, account grokAuthAccount) error {
	content, err := readRegularAuthFile(reader.path)
	if err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(content, &root) != nil {
		return ErrAuthRefreshFailed
	}
	var payload map[string]json.RawMessage
	raw, ok := root[expected.ScopeKey]
	if !ok {
		return errCredentialChanged
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ErrAuthRefreshFailed
	}
	// Re-read under the cross-process lock: a CLI can rotate credentials while
	// the network request is in flight without using this reader instance.
	for key, value := range map[string]string{"key": expected.Key, "refresh_token": expected.RefreshToken, "user_id": expected.UserID, "oidc_issuer": expected.OIDCIssuer, "oidc_client_id": expected.OIDCClientID} {
		if whitelistString(payload, key) != value {
			return errCredentialChanged
		}
	}
	expiry := whitelistString(payload, "expires_at")
	if expiry != "" {
		parsed, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil || !parsed.Equal(expected.ExpiresAt) {
			return errCredentialChanged
		}
	} else if !expected.ExpiresAt.IsZero() {
		return errCredentialChanged
	}
	payload["key"] = mustMarshalAuthString(account.Key)
	payload["refresh_token"] = mustMarshalAuthString(account.RefreshToken)
	payload["expires_at"] = mustMarshalAuthString(account.ExpiresAt.UTC().Format(time.RFC3339Nano))
	root[expected.ScopeKey], err = json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeAuthFileAtomic(reader.path, append(encoded, '\n'))
}

func mustMarshalAuthString(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
