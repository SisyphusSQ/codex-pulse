package cmd

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	schema_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
	reporting_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
)

func reconcileCursorCommand() *cobra.Command {
	var apply bool
	var after string
	var pageSize int
	var timeout time.Duration
	command := &cobra.Command{Use: "reconcile-cursor", Short: "预览或重建已接收 Cursor 来源的中心投影", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if pageSize < 1 || pageSize > 128 || timeout <= 0 || timeout > 10*time.Minute {
			return fmt.Errorf("page-size must be 1..128 and timeout must be positive and <= 10m")
		}
		cfg, err := config.Load(configure)
		if err != nil {
			return err
		}
		cfg.ContextTimeout = timeout
		return withConfiguredDatabase(cmd.Context(), cfg, func(ctx context.Context, engine *gormv2.Engine) error {
			if err := schema_repo.NewSchema(engine).Check(ctx); err != nil {
				return err
			}
			s := reporting_srv.NewReporting(reporting_repo.NewReporting(engine))
			for {
				result, reconcileErr := s.ReconcileCursor(ctx, after, pageSize, apply)
				encoded, err := json.Marshal(result)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(encoded)); err != nil {
					return err
				}
				if reconcileErr != nil {
					return reconcileErr
				}
				if result.Processed < pageSize {
					return nil
				}
				after = result.Next
			}
		})
	}}
	command.Flags().BoolVar(&apply, "apply", false, "提交派生投影更新；默认只预览")
	command.Flags().StringVar(&after, "after", "", "从上次结果的 next 主键之后继续")
	command.Flags().IntVar(&pageSize, "page-size", 32, "每页会话数（1..128）")
	command.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "总执行期限（不超过 10m）")
	return command
}
