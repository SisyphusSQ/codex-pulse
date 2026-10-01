package http_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/health"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func testServer(t *testing.T, origin string) (*apphttp.Server, *service.Access) {
	t.Helper()
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Server.Origins = []string{origin}
	cfg.Database.Path = filepath.Join(t.TempDir(), "private", "center.sqlite")
	if err := log.New(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Sync() })
	var server *apphttp.Server
	var access *service.Access
	var reporting *service.Reporting
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(gormv2.New, repository.NewAccess, repository.NewReporting, service.NewAccess, service.NewReporting, health.New, apphttp.NewServer), fx.Invoke(func(lifecycle fx.Lifecycle, engine *gormv2.Engine) {
		lifecycle.Append(fx.Hook{OnStart: func(ctx context.Context) error { return repository.NewSchema(engine).Init(ctx) }})
	}), fx.Populate(&server, &access, &reporting))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	controller.NewAccess(access, cfg).Register(server.Echo)
	controller.NewReporting(reporting).Register(server.Echo)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return server, access
}

func admin(t *testing.T, access *service.Access, origin string) dto.PairedClient {
	t.Helper()
	code, err := access.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	paired, err := access.Pair(t.Context(), code.Code, "browser", origin)
	if err != nil {
		t.Fatal(err)
	}
	return paired
}

func request(server *apphttp.Server, method, origin, path, body string, paired *dto.PairedClient, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, origin+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-ID", "request-123")
	if paired != nil {
		r.AddCookie(&http.Cookie{Name: apphttp.SessionCookieName(paired.Principal.Origin), Value: paired.Credential})
	}
	if method != http.MethodGet {
		r.Header.Set("Origin", origin)
		if paired != nil && csrf {
			r.Header.Set("X-Pulse-CSRF", paired.CSRF)
		}
	}
	w := httptest.NewRecorder()
	server.Echo.ServeHTTP(w, r)
	return w
}

func TestHTTPErrorEnvelopeAndRequestID(t *testing.T) {
	origin := "http://example.com"
	s, access := testServer(t, origin)
	paired := admin(t, access, origin)
	s.Echo.GET("/failure", func(*echo.Context) error { return errors.New("private database failure") })
	s.Echo.GET("/bad", func(*echo.Context) error { return utils.ErrBadParamInput })
	for _, tt := range []struct {
		path   string
		status int
	}{{"/failure", 500}, {"/missing", 404}, {"/bad", 400}} {
		w := request(s, http.MethodGet, origin, tt.path, "", &paired, false)
		if w.Code != tt.status {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		var body vo.Response
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != tt.status || body.RequestID != "request-123" {
			t.Fatal("request ID or error status lost")
		}
		if tt.status == 500 && body.Message != "Internal Server Error" {
			t.Fatal("private error exposed")
		}
	}
}

func TestUnifiedPairingCSRFRevocationAndPermissions(t *testing.T) {
	origin := "http://example.com"
	s, access := testServer(t, origin)
	code, err := access.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(vo.PairRequest{Code: code.Code, Mode: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, http.MethodPost, origin, "/api/v1/pair", string(payload), nil, false)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("pair=%d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("private HTTP cookie policy")
	}
	if strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("browser credential in response body")
	}
	if request(s, http.MethodPost, origin, "/api/v1/pair", string(payload), nil, false).Code != 401 {
		t.Fatal("device code reused")
	}
	paired := admin(t, access, origin)
	if request(s, http.MethodPost, origin, "/api/v1/pairings", `{"purpose":"collector","name":"设备"}`, &paired, false).Code != 403 {
		t.Fatal("CSRF bypass")
	}
	if request(s, http.MethodPost, origin, "/api/v1/pairings", `{"purpose":"collector","name":"设备","role":"admin"}`, &paired, true).Code != 400 {
		t.Fatal("unknown role accepted")
	}
	collectorCode, err := access.Issue(t.Context(), paired.Principal, dto.PurposeCollector, "设备")
	if err != nil {
		t.Fatal(err)
	}
	collector, err := access.Pair(t.Context(), collectorCode.Code, "collector", "")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, origin+"/api/v1/clients", nil)
	r.Header.Set("Authorization", "Bearer "+collector.Credential)
	denied := httptest.NewRecorder()
	s.Echo.ServeHTTP(denied, r)
	if denied.Code != 403 {
		t.Fatalf("collector privilege=%d", denied.Code)
	}
	if request(s, http.MethodPost, origin, "/api/v1/logout", "{}", &paired, true).Code != 200 {
		t.Fatal("logout failed")
	}
	if request(s, http.MethodGet, origin, "/api/v1/session", "", &paired, false).Code != 401 {
		t.Fatal("logout did not revoke")
	}
}

func TestAuthProtectsMetricsAndStatusIncludesActualErrors(t *testing.T) {
	origin := "http://example.com"
	s, access := testServer(t, origin)
	paired := admin(t, access, origin)
	s.Echo.GET("/bad", func(*echo.Context) error { return utils.ErrBadParamInput })
	for _, path := range []string{"/metrics", "/api/v1/clients", "/api/v1/session"} {
		if request(s, http.MethodGet, origin, path, "", nil, false).Code != 401 {
			t.Fatalf("public %s", path)
		}
	}
	if request(s, http.MethodGet, origin, "/bad", "", &paired, false).Code != 400 {
		t.Fatal("bad request missing")
	}
	if request(s, http.MethodGet, origin, "/bad", "", nil, false).Code != 401 {
		t.Fatal("auth failure missing")
	}
	metrics := request(s, http.MethodGet, origin, "/metrics", "", &paired, false).Body.String()
	for _, code := range []string{"400", "401"} {
		found := false
		for line := range strings.SplitSeq(metrics, "\n") {
			if strings.Contains(line, "requests_total{") && strings.Contains(line, `code="`+code+`"`) && strings.Contains(line, `url="/bad"`) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("actual error status absent: %s", code)
		}
	}
	if request(s, http.MethodGet, origin, "/health", "", nil, false).Code != 200 {
		t.Fatal("health failed")
	}
	if request(s, http.MethodGet, origin, "/ready", "", nil, false).Code != 200 {
		t.Fatal("ready failed")
	}
}

func TestHTTPSUsesSamePairingAndSecureSession(t *testing.T) {
	tlsServer := httptest.NewUnstartedServer(nil)
	origin := "https://" + tlsServer.Listener.Addr().String()
	s, access := testServer(t, origin)
	tlsServer.Config.Handler = s.Echo
	tlsServer.StartTLS()
	t.Cleanup(tlsServer.Close)
	code, err := access.Bootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(vo.PairRequest{Code: code.Code, Mode: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+"/api/v1/pair", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	response, err := tlsServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatalf("HTTPS pair=%d", response.StatusCode)
	}
	cookie := response.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Name != "__Host-pulse_session" {
		t.Fatal("HTTPS cookie flags")
	}
}
