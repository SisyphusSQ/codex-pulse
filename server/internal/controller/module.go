package controller

import (
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"go.uber.org/fx"
)

func Module(cfg config.Config) fx.Option {
	var options []fx.Option

	return fx.Options(options...)
}
