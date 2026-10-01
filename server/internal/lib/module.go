package libs

import (
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
)

// Module 仅装配显式启用的外部组件。
func Module(cfg config.Config) fx.Option {
	var options []fx.Option

	if cfg.Database.Enabled {
		options = append(options, fx.Provide(gormv2.New, fx.Annotate(gormv2.Readiness, fx.ResultTags(`group:"readiness"`))))
	}

	return fx.Options(options...)
}
