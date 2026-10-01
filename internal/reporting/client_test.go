package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

func TestPrivateHTTPAndNoRedirectOrTLSFallback(t *testing.T) {
	for _, u := range []string{"http://203.0.113.1", "http://169.254.169.254", "https://user:secret@example.com", "https://example.com?token=x", "http://127.0.0.1/path"} {
		if _, err := NewClient(u, true); !errors.Is(err, ErrSettings) {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if _, err := NewClient("http://127.0.0.1", false); !errors.Is(err, ErrSettings) {
		t.Fatal("implicit HTTP accepted")
	}
	for _, ip := range []string{"203.0.113.1", "169.254.169.254", "::ffff:169.254.169.254", "fe80::1"} {
		if privateAddress(netip.MustParseAddr(ip)) {
			t.Fatal("unsafe address accepted")
		}
	}
	for _, ip := range []string{"100.100.100.100", "192.168.1.1", "127.0.0.1", "::1"} {
		if !privateAddress(netip.MustParseAddr(ip)) {
			t.Fatal("private address rejected")
		}
	}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, err := NewClient(redirect.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Pair(context.Background(), "AAAA-BBBB-CCCC-DDDD"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("redirect error %v", err)
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("accepted untrusted certificate") }))
	defer tls.Close()
	client, err = NewClient(tls.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Pair(context.Background(), "AAAA-BBBB-CCCC-DDDD"); !errors.Is(err, ErrTransport) {
		t.Fatalf("TLS error %v", err)
	}
}
func TestCollectorAuthAndMatchingResponseEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/batches" || r.Header.Get("Authorization") != "Bearer pulse-owned" {
			t.Error("auth missing")
		}
		_ = json.NewEncoder(w).Encode(struct {
			Code int                 `json:"code"`
			Data reportingv1.Receipt `json:"data"`
		}{200, reportingv1.Receipt{Version: 1, BatchID: "expected", ReceivedAtMS: 10}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	receipt, err := client.Upload(context.Background(), "pulse-owned", queued{Body: []byte(`{}`)})
	if err != nil || receipt.BatchID != "expected" {
		t.Fatal("invalid receipt")
	}
}
func TestFiniteNetworkErrorsNeverReturnResponseBody(t *testing.T) {
	for _, status := range []int{400, 401, 409, 429, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("sensitive-source-body"))
		}))
		client, err := NewClient(server.URL, true)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Pair(context.Background(), "AAAA-BBBB-CCCC-DDDD")
		if err == nil || strings.Contains(err.Error(), "sensitive-source-body") {
			t.Fatal("raw body error")
		}
		client.Close()
		server.Close()
	}
}
