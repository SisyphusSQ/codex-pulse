package service

import (
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"go.uber.org/fx"
)

func Module(cfg config.Config) fx.Option {
	options := []fx.Option{fx.Provide()}

	return fx.Options(options...)
}
