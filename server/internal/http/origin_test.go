package http

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

func TestForwardedOriginRequiresTrustedProxy(t *testing.T) {
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.AllowHTTP = false
	cfg.Server.Origins = []string{"https://pulse.example.com"}
	r := httptest.NewRequest("GET", "http://internal.local/api/v1/session", nil)
	r.RemoteAddr = "127.0.0.1:10000"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "pulse.example.com")
	if _, err := RequestOrigin(r, cfg); err == nil {
		t.Fatal("untrusted proxy changed origin")
	}
	cfg.Server.TrustedProxies = []string{"127.0.0.1/32"}
	if origin, err := RequestOrigin(r, cfg); err != nil || origin != cfg.Server.Origins[0] {
		t.Fatalf("trusted origin=%s err=%v", origin, err)
	}
	r.Header.Set("X-Forwarded-Proto", "https,http")
	if _, err := RequestOrigin(r, cfg); err == nil {
		t.Fatal("ambiguous protocol accepted")
	}
}

func TestPairingAttemptsAreBoundedAndExpire(t *testing.T) {
	limiter := newPairingLimiter()
	now := time.Now()
	for range 30 {
		if !limiter.Allow("127.0.0.1:1234", now) {
			t.Fatal("premature rate limit")
		}
	}
	if limiter.Allow("127.0.0.1:5678", now) {
		t.Fatal("changing source port evaded limit")
	}
	if !limiter.Allow("127.0.0.1:5678", now.Add(time.Minute)) {
		t.Fatal("limit did not expire")
	}
}
