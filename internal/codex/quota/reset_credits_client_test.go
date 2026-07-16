package quota

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestResetCreditsClientFetchesTypedReadOnlySnapshot(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	var retained *http.Request
	client := mustResetCreditsClient(t, ResetCreditsClientConfig{
		Credentials: provider, Now: fixedClock(1_784_000_000_000),
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			retained = request
			if request.Method != http.MethodGet || request.URL.String() != WhamResetCreditsEndpoint {
				t.Fatalf("request = %s %s", request.Method, request.URL)
			}
			if request.Header.Get("Authorization") != "Bearer "+syntheticAccessToken ||
				request.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("headers = %#v", request.Header)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"available_count": 2,
					"credits": [
						{"id":"RateLimitResetCredit_private-a","status":"available","reset_type":"codex_rate_limits","granted_at":"2026-07-15T10:00:00Z","expires_at":"2026-07-15T13:00:00Z","title":"private-title","profile_user_id":"private-user"},
						{"id":"RateLimitResetCredit_private-b","status":"available","type":"codex_rate_limits","created_at":"2026-07-15T10:00:00Z","expires_at":"2026-07-15T14:00:00Z","description":"private-description"},
						{"id":"RateLimitResetCredit_private-c","status":"redeemed","reset_type":"codex_rate_limits","granted_at":"2026-07-14T10:00:00Z","expires_at":"2026-07-16T10:00:00Z","redeemed_at":"2026-07-15T10:30:00Z"}
					]
				}`)),
			}, nil
		}),
	})
	result, err := client.Fetch(context.Background(), "reset-client-success")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil || result.Snapshot == nil || result.AttemptCount != 1 ||
		result.HTTPStatus == nil || *result.HTTPStatus != 200 || len(result.Snapshot.Credits) != 3 ||
		result.Snapshot.AvailableCount != 2 || result.PayloadSHA256 == nil {
		t.Fatalf("result = %#v", result)
	}
	if got := result.Snapshot.Credits[0].CreditIDHash.String(); got == "" || strings.Contains(got, "private") {
		t.Fatalf("credit digest = %q", got)
	}
	if retained.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization retained after callback: %#v", retained.Header)
	}
	serialized := fmt.Sprintf("%#v", result)
	for _, marker := range []string{"private-a", "private-b", "private-c", "private-title", "private-user", "private-description", syntheticAccessToken} {
		if strings.Contains(serialized, marker) {
			t.Fatalf("result leaked %q: %s", marker, serialized)
		}
	}
}

func TestResetCreditsClientFailsClosedOnInvalidPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate key", body: `{"available_count":0,"available_count":1,"credits":[]}`},
		{name: "count mismatch", body: `{"available_count":1,"credits":[]}`},
		{name: "missing id", body: `{"available_count":1,"credits":[{"status":"available","reset_type":"codex_rate_limits","granted_at":"2026-07-15T10:00:00Z","expires_at":"2026-07-16T10:00:00Z"}]}`},
		{name: "invalid expiry", body: `{"available_count":1,"credits":[{"id":"a","status":"available","reset_type":"codex_rate_limits","granted_at":"2026-07-15T10:00:00Z","expires_at":"not-a-time"}]}`},
		{name: "unknown status", body: `{"available_count":0,"credits":[{"id":"a","status":"private-future","reset_type":"codex_rate_limits","granted_at":"2026-07-15T10:00:00Z","expires_at":"2026-07-16T10:00:00Z"}]}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := resetCreditsClientForBody(t, testCase.body)
			result, err := client.Fetch(context.Background(), "reset-invalid-"+strings.ReplaceAll(testCase.name, " ", "-"))
			if err != nil || result.Snapshot != nil || result.Failure == nil ||
				result.Failure.Code != store.SourceFailureSchemaIncompatible {
				t.Fatalf("Fetch() = %#v, %v", result, err)
			}
		})
	}
}

func mustResetCreditsClient(t *testing.T, config ResetCreditsClientConfig) *ResetCreditsClient {
	t.Helper()
	client, err := NewResetCreditsClient(config)
	if err != nil {
		t.Fatalf("NewResetCreditsClient() error = %v", err)
	}
	return client
}

func resetCreditsClientForBody(t *testing.T, body string) *ResetCreditsClient {
	t.Helper()
	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	return mustResetCreditsClient(t, ResetCreditsClientConfig{
		Credentials: provider, Now: fixedClock(1_784_000_000_000),
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	})
}
