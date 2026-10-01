package repository

import (
	"context"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Options(fx.Provide(NewSchema, NewAccess), fx.Invoke(func(lifecycle fx.Lifecycle, schema *Schema) {
		lifecycle.Append(fx.Hook{OnStart: func(ctx context.Context) error { return schema.Check(ctx) }})
	}))
}
