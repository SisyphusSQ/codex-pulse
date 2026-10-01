package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/access_repo"
	schema_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

func databaseCommand() *cobra.Command {
	command := &cobra.Command{Use: "db", Short: "显式初始化或检查中心结构"}
	for _, action := range []string{"init", "check"} {
		command.AddCommand(&cobra.Command{Use: action, RunE: func(cmd *cobra.Command, _ []string) error {
			return withDatabase(cmd.Context(), func(ctx context.Context, engine *gormv2.Engine) error {
				s := schema_repo.NewSchema(engine)
				var err error
				if action == "init" {
					err = s.Init(ctx)
				} else {
					err = s.Check(ctx)
				}
				if err == nil {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), "center schema ready")
				}
				return err
			})
		}})
	}
	command.AddCommand(&cobra.Command{Use: "bootstrap", Short: "生成短期首次管理员码（只在受信任终端显示）", RunE: func(cmd *cobra.Command, _ []string) error {
		return withDatabase(cmd.Context(), func(ctx context.Context, engine *gormv2.Engine) error {
			if err := schema_repo.NewSchema(engine).Check(ctx); err != nil {
				return err
			}
			code, err := access_srv.NewAccess(access_repo.NewAccess(engine)).Bootstrap(ctx)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s (expires %s)\n", code.Code, time.UnixMilli(code.ExpiresAtMS).UTC().Format(time.RFC3339))
			return err
		})
	}})
	command.AddCommand(backupCommands()...)
	return command
}

// withDatabase 用相同 Fx 连接生命周期执行 CLI，不启动 HTTP 或本机采集。
func withDatabase(ctx context.Context, action func(context.Context, *gormv2.Engine) error) (err error) {
	cfg, err := config.Load(configure)
	if err != nil {
		return err
	}
	return withConfiguredDatabase(ctx, cfg, action)
}

func withConfiguredDatabase(ctx context.Context, cfg config.Config, action func(context.Context, *gormv2.Engine) error) (err error) {
	if !cfg.Database.Enabled {
		return fmt.Errorf("enable center database before using db commands")
	}
	var engine *gormv2.Engine
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(gormv2.New), fx.Populate(&engine))
	if err := app.Err(); err != nil {
		return err
	}
	start, cancel := context.WithTimeout(ctx, cfg.ContextTimeout)
	defer cancel()
	if err := app.Start(start); err != nil {
		return err
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if closeErr := app.Stop(stop); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	return action(start, engine)
}
