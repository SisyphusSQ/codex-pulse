package grokprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthReaderRejectsCrossOriginTokenRedirect(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	var sinkRequests atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		sinkRequests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(sink.Close)
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": issuer.URL, "token_endpoint": issuer.URL + "/oauth/token",
			})
		case "/oauth/token":
			http.Redirect(writer, request, sink.URL+"/capture", http.StatusTemporaryRedirect)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(issuer.Close)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": issuer.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	})
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	if _, err := reader.ReadAccessTokenContext(context.Background()); !errors.Is(err, ErrAuthRefreshFailed) {
		t.Fatalf("ReadAccessTokenContext() error = %v, want ErrAuthRefreshFailed", err)
	}
	if got := sinkRequests.Load(); got != 0 {
		t.Fatalf("cross-origin token redirect requests = %d, want 0", got)
	}
}

func TestAuthReaderRefusesSymlinkBeforeRefreshing(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(writer, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	directory := t.TempDir()
	target := filepath.Join(directory, "real-auth.json")
	writeGrokAuthFixture(t, target, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	})
	authPath := filepath.Join(directory, "auth.json")
	if err := os.Symlink(target, authPath); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	if _, err := reader.ReadAccessTokenContext(context.Background()); !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("ReadAccessTokenContext() error = %v, want ErrAuthUnavailable", err)
	}
	info, err := os.Lstat(authPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("auth path after refresh = %#v, %v, want symlink", info, err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("IdP requests = %d, want 0", got)
	}
}

func TestAuthReaderRefusesSymlinkedRefreshLock(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	authPath := filepath.Join(directory, "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": "https://auth.example.com", "oidc_client_id": "grok-client",
			"expires_at": now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	})
	target := filepath.Join(directory, "unrelated")
	if err := os.WriteFile(target, []byte("do-not-change"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(unrelated) error = %v", err)
	}
	lockPath := filepath.Join(directory, "auth.json.lock")
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatalf("os.Symlink(lock) error = %v", err)
	}

	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	if _, err := reader.ReadAccessTokenContext(context.Background()); !errors.Is(err, ErrAuthRefreshFailed) {
		t.Fatalf("ReadAccessTokenContext() error = %v, want ErrAuthRefreshFailed", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "do-not-change" {
		t.Fatalf("unrelated file = %q, %v", content, err)
	}
}

func TestAuthReaderSerializesRotatingRefreshAcrossReaders(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	var refreshRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": server.URL, "token_endpoint": server.URL + "/oauth/token",
			})
		case "/oauth/token":
			attempt := refreshRequests.Add(1)
			time.Sleep(100 * time.Millisecond)
			if attempt > 1 {
				http.Error(writer, "refresh token already rotated", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "new-access", "refresh_token": "new-refresh",
				"token_type": "Bearer", "expires_in": 21_600,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(4 * time.Minute).Format(time.RFC3339Nano),
		},
	})
	readers := make([]*AuthReader, 2)
	for index := range readers {
		reader, err := NewAuthReader(authPath, func() time.Time { return now })
		if err != nil {
			t.Fatalf("NewAuthReader(%d) error = %v", index, err)
		}
		readers[index] = reader
	}

	start := make(chan struct{})
	results := make(chan struct {
		credential AccessToken
		err        error
	}, len(readers))
	var group sync.WaitGroup
	for _, reader := range readers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			credential, err := reader.ReadAccessTokenContext(context.Background())
			results <- struct {
				credential AccessToken
				err        error
			}{credential: credential, err: err}
		}()
	}
	close(start)
	group.Wait()
	close(results)

	for result := range results {
		if result.err != nil || result.credential.Token != "new-access" {
			t.Fatalf("credential = %#v, error = %v", result.credential, result.err)
		}
	}
	if got := refreshRequests.Load(); got != 1 {
		t.Fatalf("refresh requests = %d, want 1", got)
	}
}

func TestAuthReaderAdoptsNewlySelectedAccountWithoutRefreshingIt(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account-a": map[string]any{
			"email": "a@example.com", "user_id": "user-a", "auth_mode": "oidc",
			"key": "access-a", "refresh_token": "refresh-a",
			"oidc_issuer": "https://auth.example.com", "oidc_client_id": "grok-client",
			"expires_at": now.Add(4 * time.Minute).Format(time.RFC3339Nano),
		},
	})
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	observed, err := reader.loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() error = %v", err)
	}
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account-b": map[string]any{
			"email": "b@example.com", "user_id": "user-b", "auth_mode": "oidc",
			"key": "access-b", "refresh_token": "refresh-b",
			"oidc_issuer": "https://auth.example.com", "oidc_client_id": "grok-client",
			"expires_at": now.Add(2 * time.Minute).Format(time.RFC3339Nano),
		},
	})
	credential, err := reader.refreshAccessTokenLocked(context.Background(), observed, now, false, "")
	if err != nil || credential.Token != "access-b" || credential.UserID != "user-b" {
		t.Fatalf("credential = %#v, error = %v", credential, err)
	}
}

func TestAuthReaderKeepsHardValidCredentialWhenProactiveRefreshIsTransient(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "still-valid", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(4 * time.Minute).Format(time.RFC3339Nano),
		},
	})
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	credential, err := reader.ReadAccessTokenContext(context.Background())
	if err != nil || credential.Token != "still-valid" {
		t.Fatalf("credential = %#v, error = %v", credential, err)
	}
}

func TestAuthReaderDoesNotMutateCredentialOnInvalidGrant(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": server.URL, "token_endpoint": server.URL + "/oauth/token",
			})
		case "/oauth/token":
			http.Error(writer, "invalid_grant", http.StatusBadRequest)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "expired-access", "refresh_token": "invalid-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	})
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("os.ReadFile(before) error = %v", err)
	}
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	if _, err := reader.ReadAccessTokenContext(context.Background()); !errors.Is(err, ErrAuthExpired) {
		t.Fatalf("ReadAccessTokenContext() error = %v, want ErrAuthExpired", err)
	}
	after, err := os.ReadFile(authPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("auth.json changed after invalid_grant: error=%v", err)
	}
}

func TestAuthReaderDoesNotRefreshWhenDisabled(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(writer, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(4 * time.Minute).Format(time.RFC3339Nano),
		},
	})

	reader, err := NewAuthReader(authPath, func() time.Time { return now }, AuthReaderConfig{
		RefreshEnabled: func() bool { return false },
	})
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	credential, err := reader.ReadAccessToken()
	if err != nil {
		t.Fatalf("ReadAccessToken() error = %v", err)
	}
	if credential.Token != "old-access" || requests != 0 {
		t.Fatalf("credential = %#v, IdP requests = %d", credential, requests)
	}
}

func TestBillingClientRefreshesRejectedOIDCCredentialOnce(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	billingRequests := 0
	refreshRequests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": server.URL, "token_endpoint": server.URL + "/oauth/token",
			})
		case "/oauth/token":
			refreshRequests++
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "new-access", "refresh_token": "new-refresh",
				"token_type": "Bearer", "expires_in": 21_600,
			})
		case "/billing":
			billingRequests++
			switch request.Header.Get("Authorization") {
			case "Bearer old-access":
				http.Error(writer, "expired", http.StatusUnauthorized)
			case "Bearer new-access":
				_, _ = writer.Write([]byte(`{
					"creditUsagePercent": 44,
					"currentPeriod": {
						"type": "WEEKLY",
						"start": "2026-08-19T00:00:00Z",
						"end": "2026-08-26T00:00:00Z"
					}
				}`))
			default:
				http.Error(writer, "unexpected credential", http.StatusForbidden)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at": now.Add(time.Hour).Format(time.RFC3339Nano),
		},
	})
	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	client, err := NewBillingClient(BillingClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(), TokenSource: reader,
	})
	if err != nil {
		t.Fatalf("NewBillingClient() error = %v", err)
	}
	credits, err := client.GetCredits(context.Background())
	if err != nil {
		t.Fatalf("GetCredits() error = %v", err)
	}
	if credits.UsedPercent != 44 || billingRequests != 2 || refreshRequests != 1 {
		t.Fatalf("credits = %#v, billing requests = %d, refresh requests = %d",
			credits, billingRequests, refreshRequests)
	}
}

func TestAuthReaderRefreshesExpiringOIDCCredential(t *testing.T) {
	now := time.Date(2026, time.August, 19, 2, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": server.URL, "token_endpoint": server.URL + "/oauth/token",
			})
		case "/oauth/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
				http.Error(writer, "invalid form", http.StatusBadRequest)
				return
			}
			want := url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"old-refresh"},
				"client_id":     {"grok-client"},
			}
			if request.Form.Encode() != want.Encode() {
				t.Errorf("token form = %q, want %q", request.Form.Encode(), want.Encode())
				http.Error(writer, "invalid form", http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "new-access", "refresh_token": "new-refresh",
				"token_type": "Bearer", "expires_in": 21_600,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeGrokAuthFixture(t, authPath, map[string]any{
		"account": map[string]any{
			"email": "person@example.com", "user_id": "user-1", "auth_mode": "oidc",
			"key": "old-access", "refresh_token": "old-refresh",
			"oidc_issuer": server.URL, "oidc_client_id": "grok-client",
			"expires_at":   now.Add(4 * time.Minute).Format(time.RFC3339Nano),
			"future_field": map[string]any{"preserved": true},
		},
		"other-account": map[string]any{
			"email": "other@example.com", "user_id": "user-2", "auth_mode": "api_key",
			"key": "unrelated-secret", "future_field": "unchanged",
		},
	})

	reader, err := NewAuthReader(authPath, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewAuthReader() error = %v", err)
	}
	credential, err := reader.ReadAccessToken()
	if err != nil {
		t.Fatalf("ReadAccessToken() error = %v", err)
	}
	if credential.Token != "new-access" || credential.UserID != "user-1" ||
		!credential.ExpiresAt.Equal(now.Add(6*time.Hour)) {
		t.Fatalf("credential = %#v", credential)
	}

	content, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("os.ReadFile(auth.json) error = %v", err)
	}
	var persisted map[string]map[string]any
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(auth.json) error = %v", err)
	}
	account := persisted["account"]
	if account["key"] != "new-access" || account["refresh_token"] != "new-refresh" ||
		account["future_field"] == nil {
		t.Fatalf("persisted account = %#v", account)
	}
	other := persisted["other-account"]
	if other["key"] != "unrelated-secret" || other["future_field"] != "unchanged" {
		t.Fatalf("unrelated account changed = %#v", other)
	}
	if mode := fileMode(t, authPath); mode != 0o600 {
		t.Fatalf("auth.json mode = %04o, want 0600", mode)
	}
}

func writeGrokAuthFixture(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth.json) error = %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	return info.Mode().Perm()
}
