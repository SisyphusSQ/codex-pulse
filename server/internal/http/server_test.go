package http

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/health"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"
)

func testServer(t *testing.T, auth string) (*Server, *fx.App) {
	t.Helper()
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Key.Type = auth
	cfg.Key.AK.AccessKey = "test-access"
	cfg.Key.AK.SecretKey = "test-secret"
	if err := log.New(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Sync() })
	var server *Server
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(health.New, NewServer), fx.Populate(&server))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	return server, app
}

func TestHTTPErrorEnvelopeAndRequestID(t *testing.T) {
	s, _ := testServer(t, "none")
	s.Echo.GET("/failure", func(c *echo.Context) error { return errors.New("private database failure") })
	s.Echo.GET("/bad", func(c *echo.Context) error { return utils.ErrBadParamInput })
	for _, tt := range []struct {
		path   string
		status int
	}{{"/failure", 500}, {"/missing", 404}, {"/bad", 400}} {
		t.Run(tt.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.Header.Set("X-Request-ID", "request-123")
			w := httptest.NewRecorder()
			s.Echo.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			var body vo.Response
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tt.status || body.RequestID != "request-123" || w.Header().Get("X-Request-ID") != "request-123" {
				t.Fatalf("unexpected response: %+v", body)
			}
			if tt.status == 500 && body.Message != "Internal Server Error" {
				t.Fatalf("private error disclosed: %s", body.Message)
			}
		})
	}
}

func TestAuthProtectsMetricsAndDoesNotExposeLogin(t *testing.T) {
	s, _ := testServer(t, "key")
	for _, path := range []string{"/metrics", "/login", "/swagger/anything"} {
		w := httptest.NewRecorder()
		s.Echo.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s unexpectedly public: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Header.Set("access_key", "test-access")
	r.Header.Set("secret_key", "test-secret")
	s.Echo.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("valid credentials rejected: %d", w.Code)
	}
}

func TestReadinessTracksLifecycle(t *testing.T) {
	s, app := testServer(t, "none")
	probe := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		s.Echo.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d want %d", path, w.Code, want)
		}
	}
	probe("/health", 200)
	probe("/ready", 503)
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	probe("/ready", 200)
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	probe("/ready", 503)
}

func TestMetricsUseActualErrorStatusIncludingAuth(t *testing.T) {
	s, _ := testServer(t, "key")
	s.Echo.GET("/bad", func(*echo.Context) error { return utils.ErrBadParamInput })
	request := func(path string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorized {
			req.Header.Set("access_key", "test-access")
			req.Header.Set("secret_key", "test-secret")
		}
		rec := httptest.NewRecorder()
		s.Echo.ServeHTTP(rec, req)
		return rec
	}
	if got := request("/bad", true).Code; got != 400 {
		t.Fatalf("bad request=%d", got)
	}
	if got := request("/bad", false).Code; got != 401 {
		t.Fatalf("auth rejection=%d", got)
	}
	metrics := request("/metrics", true).Body.String()
	for _, code := range []string{"400", "401"} {
		found := false
		for line := range strings.SplitSeq(metrics, "\n") {
			if strings.Contains(line, "requests_total{") && strings.Contains(line, `code="`+code+`"`) && strings.Contains(line, `url="/bad"`) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("metrics did not record /bad with status %s", code)
		}
	}
}
