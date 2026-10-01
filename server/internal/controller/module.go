package controller

import (
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(NewAccess), fx.Invoke(func(access *Access, e *echo.Echo) { access.Register(e) }))
}
