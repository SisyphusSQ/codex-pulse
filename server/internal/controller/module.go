package controller

import (
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(NewAccess, NewReporting, NewStatistics), fx.Invoke(func(access *Access, reporting *Reporting, statistics *Statistics, e *echo.Echo) {
		access.Register(e)
		reporting.Register(e)
		statistics.Register(e)
	}))
}
