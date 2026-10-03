package controller

import (
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/access_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/catalog_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/quota_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/reporting_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/runtime_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/statistics_controller"
	"github.com/SisyphusSQ/codex-pulse/server/internal/controller/subscription_controller"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(access_controller.NewAccess, reporting_controller.NewReporting, statistics_controller.NewStatistics, quota_controller.NewQuota, subscription_controller.NewSubscription, catalog_controller.NewCatalog), fx.Invoke(func(access *access_controller.Access, reporting *reporting_controller.Reporting, statistics *statistics_controller.Statistics, quota *quota_controller.Quota, catalog *catalog_controller.Catalog, subscription *subscription_controller.Subscription, e *echo.Echo) {
		runtime_controller.Register(e)
		access.Register(e)
		reporting.Register(e)
		statistics.Register(e)
		quota.Register(e)
		catalog.Register(e)
		subscription.Register(e)
	}))
}
