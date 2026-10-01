package service

import (
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/statistics_srv"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Provide(access_srv.NewAccess, reporting_srv.NewReporting, statistics_srv.NewStatistics)
}
