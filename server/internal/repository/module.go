package repository

import (
	"context"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/quota_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(schema_repo.NewSchema, access_repo.NewAccess, reporting_repo.NewReporting, statistics_repo.NewStatistics, quota_repo.NewQuota), fx.Invoke(func(lifecycle fx.Lifecycle, schema *schema_repo.Schema) {
		lifecycle.Append(fx.Hook{OnStart: func(ctx context.Context) error { return schema.Check(ctx) }})
	}))
}
