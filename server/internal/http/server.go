package http

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"

	prom "github.com/labstack/echo-contrib/v5/echoprometheus"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/health"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service"

	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
)

// Server 由 Fx 按依赖顺序启动，并在退出时停止接受业务请求。
type Dependencies struct {
	fx.In
	Access *service.Access `optional:"true"`
}

type Server struct {
	Echo   *echo.Echo
	HTTP   *stdhttp.Server
	health *health.Registry
}

var Module = fx.Options(fx.Provide(NewServer, func(s *Server) *echo.Echo { return s.Echo }))

func NewServer(lifecycle fx.Lifecycle, cfg config.Config, registry *health.Registry, shutdown fx.Shutdowner, deps Dependencies) *Server {
	e := echo.New()
	m := InitMiddleware(cfg, deps.Access)
	e.HTTPErrorHandler = m.ErrorHandler
	if cfg.Server.Metrics {
		metrics := prometheus.NewRegistry()
		e.Use(prom.NewMiddlewareWithConfig(prom.MiddlewareConfig{Namespace: "app", Registerer: metrics}))
		e.GET("/metrics", prom.NewHandlerWithConfig(prom.HandlerConfig{Gatherer: metrics}))
	}
	// Metrics 在最外层观察已完成的响应，包含鉴权拒绝和业务错误。
	e.Use(m.Logger, m.Recover, m.Deadline, m.CORS, middleware.BodyLimit(cfg.Server.MaxBodyBytes), m.Auth)
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
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
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
