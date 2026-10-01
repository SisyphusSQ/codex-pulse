package controller

import (
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/access_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/reporting_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/statistics_controller"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(access_controller.NewAccess, reporting_controller.NewReporting, statistics_controller.NewStatistics), fx.Invoke(func(access *access_controller.Access, reporting *reporting_controller.Reporting, statistics *statistics_controller.Statistics, e *echo.Echo) {
		access.Register(e)
		reporting.Register(e)
		statistics.Register(e)
	}))
}
