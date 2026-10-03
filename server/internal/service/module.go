package service

import (
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/catalog_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/maintenance_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/quota_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/statistics_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/subscription_srv"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(access_srv.NewAccess, reporting_srv.NewReporting, statistics_srv.NewStatistics, quota_srv.NewQuota, subscription_srv.NewSubscription, catalog_srv.NewCatalog, maintenance_srv.NewMaintenance), fx.Invoke(func(*maintenance_srv.Maintenance) {}))
}
