package http_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/runtime_controller"
	"github.com/SisyphusSQ/codex-pulse/server/web"
)

func TestSameOriginServerWithoutCORSRetainsAuthorization(t *testing.T) {
	origin := "http://example.com"
	server, access := testServer(t, origin, func(cfg *config.Config) {
		cfg.Server.CORSOrigins = nil
	})
	for _, endpoint := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/api/v1/session", 401}, {"/api/v1/clients", 401}} {
		response := request(server, http.MethodGet, origin, endpoint.path, "", nil, false)
		if response.Code != endpoint.status || response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("same-origin endpoint %s: status=%d", endpoint.path, response.Code)
		}
	}
	paired := admin(t, access, origin)
	if response := request(server, http.MethodGet, origin, "/api/v1/session", "", &paired, false); response.Code != 200 {
		t.Fatalf("authorized session: %d", response.Code)
	}
	body := `{"purpose":"collector","name":"synthetic device"}`
	if response := request(server, http.MethodPost, origin, "/api/v1/pairings", body, &paired, false); response.Code != 403 {
		t.Fatalf("missing CSRF: %d", response.Code)
	}
	foreign := httptest.NewRequest(http.MethodPost, origin+"/api/v1/pairings", strings.NewReader(body))
	foreign.Header.Set("Content-Type", "application/json")
	foreign.Header.Set("Origin", "http://foreign.example")
	foreign.Header.Set("X-Pulse-CSRF", paired.CSRF)
	foreign.AddCookie(&http.Cookie{Name: "pulse_session", Value: paired.Credential})
	response := httptest.NewRecorder()
	server.Echo.ServeHTTP(response, foreign)
	if response.Code != 403 || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign mutation: %d", response.Code)
	}
}

func TestStaticWebRootAssetsAndAPIAuthorization(t *testing.T) {
	origin := "http://example.com"
	s, access := testServer(t, origin)
	assets, err := fs.ReadDir(web.Files, "dist/assets")
	if err != nil || len(assets) == 0 {
		t.Fatalf("embedded assets missing: %v", err)
	}
	assetPath := "/assets/" + assets[0].Name()
	runtime_controller.Register(s.Echo)
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/", 200}, {"HEAD", "/", 200}, {"GET", assetPath, 200}, {"HEAD", assetPath, 200}, {"GET", "/assets/.secret.js", 404}, {"GET", "/assets/source.map", 404}, {"GET", "/assets/../index.html", 404}, {"GET", "/assets/%2e%2e/index.html", 404}, {"GET", "/assets/missing.js", 404}, {"GET", "/config/config.yml", 401}, {"GET", "/index.html", 401}, {"GET", "/api/v1/version", 401}, {"GET", "/api/v1/clients", 401}} {
		w := request(s, tt.method, origin, tt.path, "", nil, false)
		if w.Code != tt.status {
			t.Fatalf("%s %s = %d", tt.method, tt.path, w.Code)
		}
		if tt.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("security headers missing")
		}
		if tt.path == assetPath && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal("hashed asset cache missing")
		}
	}
	index := request(s, http.MethodGet, origin, "/", "", nil, false)
	if !strings.Contains(index.Body.String(), `id="root"`) || !strings.Contains(index.Body.String(), `/assets/`) {
		t.Fatal("compiled application shell missing")
	}
	for _, asset := range assets {
		if asset.IsDir() {
			continue
		}
		w := request(s, http.MethodGet, origin, "/assets/"+asset.Name(), "", nil, false)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("embedded asset %s: %d", asset.Name(), w.Code)
		}
	}
	paired := admin(t, access, origin)
	w := request(s, http.MethodGet, origin, "/api/v1/version", "", &paired, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"reporting_protocol":1`) || strings.Contains(w.Body.String(), "GitRemote") {
		t.Fatalf("version view = %d", w.Code)
	}
}

func TestWildcardAndExactCORSKeepAuthorization(t *testing.T) {
	origin := "http://example.com"
	foreignOrigin := "https://foreign.example"
	for _, tt := range []struct {
		name             string
		origins          []string
		allowOrigin      string
		allowCredentials string
	}{
		{"wildcard", []string{"*"}, "*", ""},
		{"exact", []string{foreignOrigin}, foreignOrigin, "true"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, access := testServer(t, origin, func(cfg *config.Config) { cfg.Server.CORSOrigins = tt.origins })
			preflight := httptest.NewRequest(http.MethodOptions, origin+"/api/v1/pairings", nil)
			preflight.Header.Set("Origin", foreignOrigin)
			preflight.Header.Set("Access-Control-Request-Method", "POST")
			preflight.Header.Set("Access-Control-Request-Headers", "content-type,x-pulse-csrf")
			w := httptest.NewRecorder()
			s.Echo.ServeHTTP(w, preflight)
			if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != tt.allowOrigin || w.Header().Get("Access-Control-Allow-Credentials") != tt.allowCredentials {
				t.Fatalf("preflight: status=%d origin=%q credentials=%q", w.Code, w.Header().Get("Access-Control-Allow-Origin"), w.Header().Get("Access-Control-Allow-Credentials"))
			}
			anonymous := httptest.NewRequest(http.MethodGet, origin+"/api/v1/clients", nil)
			anonymous.Header.Set("Origin", foreignOrigin)
			w = httptest.NewRecorder()
			s.Echo.ServeHTTP(w, anonymous)
			if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != tt.allowOrigin {
				t.Fatalf("anonymous API: %d", w.Code)
			}
			paired := admin(t, access, origin)
			body := `{"purpose":"collector","name":"synthetic device"}`
			if request(s, http.MethodPost, origin, "/api/v1/pairings", body, &paired, false).Code != 403 {
				t.Fatal("missing CSRF accepted")
			}
			if request(s, http.MethodPost, origin, "/api/v1/pairings", body, &paired, true).Code != 200 {
				t.Fatal("same-origin Cookie and CSRF authorization failed")
			}
			foreign := httptest.NewRequest(http.MethodPost, origin+"/api/v1/pairings", strings.NewReader(body))
			foreign.Header.Set("Content-Type", "application/json")
			foreign.Header.Set("Origin", foreignOrigin)
			foreign.Header.Set("X-Pulse-CSRF", paired.CSRF)
			foreign.AddCookie(&http.Cookie{Name: "pulse_session", Value: paired.Credential})
			w = httptest.NewRecorder()
			s.Echo.ServeHTTP(w, foreign)
			if w.Code != 403 {
				t.Fatalf("foreign mutation: %d", w.Code)
			}
		})
	}
}
