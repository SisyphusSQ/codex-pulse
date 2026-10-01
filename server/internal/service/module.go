package service

import (
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"go.uber.org/fx"
)

func Module(cfg config.Config) fx.Option {
	if !cfg.Database.Enabled {
		return fx.Options()
	}
	return fx.Provide(NewAccess)
}
