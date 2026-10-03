package http

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	prom "github.com/labstack/echo-contrib/v5/echoprometheus"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/health"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/web"
)

// Server 由 Fx 按依赖顺序启动，并在退出时停止接受业务请求。
type Dependencies struct {
	fx.In
	Access    *access_srv.Access        `optional:"true"`
	Reporting *reporting_repo.Reporting `optional:"true"`
}

type Server struct {
	Echo   *echo.Echo
	HTTP   *stdhttp.Server
	health *health.Registry
	web    *webFiles
}

var Module = fx.Options(fx.Provide(NewServer, func(s *Server) *echo.Echo { return s.Echo }))

func NewServer(lifecycle fx.Lifecycle, cfg config.Config, registry *health.Registry, shutdown fx.Shutdowner, deps Dependencies) *Server {
	e := echo.New()
	m := InitMiddleware(cfg, deps.Access)
	e.HTTPErrorHandler = m.ErrorHandler
	if cfg.Server.Metrics {
		metrics := prometheus.NewRegistry()
		metrics.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		if deps.Reporting != nil {
			metrics.MustRegister(newCenterMetrics(deps.Reporting))
		}
		uploads := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "pulse_reporting_uploads_total", Help: "Upload requests including idempotent retries, by response code."}, []string{"code"})
		failures := prometheus.NewCounter(prometheus.CounterOpts{Name: "pulse_reporting_upload_failures_total", Help: "Upload requests with error responses."})
		duration := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "pulse_reporting_upload_duration_seconds", Help: "Completed upload request duration.", Buckets: prometheus.DefBuckets})
		metrics.MustRegister(uploads, failures, duration)
		e.Use(prom.NewMiddlewareWithConfig(prom.MiddlewareConfig{Namespace: "app", Registerer: metrics, DoNotUseRequestPathFor404: true, LabelFuncs: map[string]prom.LabelValueFunc{"method": func(c *echo.Context, _ error) string {
			switch method := c.Request().Method; method {
			case stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodPost, stdhttp.MethodPut, stdhttp.MethodPatch, stdhttp.MethodDelete, stdhttp.MethodOptions, stdhttp.MethodConnect, stdhttp.MethodTrace:
				return method
			default:
				return "other"
			}
		}, "url": func(c *echo.Context, _ error) string {
			if c.Path() == "" {
				return "unmatched"
			}
			return c.Path()
		}}, BeforeNext: func(c *echo.Context) {
			if c.Path() == "/api/v1/batches" {
				c.Set("pulse.upload.started", time.Now())
			}
		}, AfterNext: func(c *echo.Context, _ error) {
			if c.Path() != "/api/v1/batches" {
				return
			}
			response, _ := echo.UnwrapResponse(c.Response())
			code := 200
			if response != nil {
				code = response.Status
			}
			uploads.WithLabelValues(strconv.Itoa(code)).Inc()
			if code >= 400 {
				failures.Inc()
			}
			if started, ok := c.Get("pulse.upload.started").(time.Time); ok {
				duration.Observe(time.Since(started).Seconds())
			}
		}}))
		e.GET("/metrics", prom.NewHandlerWithConfig(prom.HandlerConfig{Gatherer: metrics}))
	}
	// Metrics 在最外层观察已完成的响应，包含鉴权拒绝和业务错误。
	e.Use(m.Logger, m.Recover, m.Deadline, m.CORS, middleware.BodyLimit(cfg.Server.MaxBodyBytes), m.Auth)
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{MinLength: 1024, Level: 1, Skipper: func(c *echo.Context) bool {
		path := c.Request().URL.Path
		return c.Request().Method != stdhttp.MethodGet || !(strings.HasPrefix(path, "/api/v1/statistics/") || path == "/api/v1/quotas" || strings.HasPrefix(path, "/api/v1/quotas/") || path == "/api/v1/catalog" || path == "/api/v1/sessions" || strings.HasPrefix(path, "/api/v1/sessions/") || path == "/api/v1/projects" || strings.HasPrefix(path, "/api/v1/projects/"))
	}}))
	e.GET("/health", func(c *echo.Context) error { return vo.CommSuccResp(c, map[string]string{"status": "alive"}) })
	e.GET("/ready", func(c *echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), cfg.Server.ReadinessTimeout)
		defer cancel()
		if err := registry.Ready(ctx); err != nil {
			return echo.ErrServiceUnavailable
		}
		return vo.CommSuccResp(c, map[string]string{"status": "ready"})
	})
	server := &stdhttp.Server{Addr: cfg.Server.Address, Handler: e, ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout, ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout, IdleTimeout: cfg.Server.IdleTimeout}
	s := &Server{Echo: e, HTTP: server, health: registry}
	e.GET("/", s.webIndex)
	e.HEAD("/", s.webIndex)
	e.GET("/assets/*", s.webAsset)
	e.HEAD("/assets/*", s.webAsset)
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if err := m.loadMetricsCredential(); err != nil {
				return err
			}
			var err error
			s.web, err = openWeb(web.Files)
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", cfg.Server.Address)
			if err != nil {
				return err
			}
			registry.SetReady(true)
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
					registry.SetReady(false)
					log.Logger.Error("http server stopped unexpectedly")
					_ = shutdown.Shutdown(fx.ExitCode(1))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			registry.SetReady(false)
			if err := server.Shutdown(ctx); err != nil {
				_ = server.Close()
				return err
			}
			return nil
		},
	})
	return s
}
