package service

import (
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Provide(NewAccess)
}
