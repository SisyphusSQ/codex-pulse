package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/SisyphusSQ/codex-pulse/server/vars"
)

var (
	rootCmd = &cobra.Command{
		Use:     "codex-pulse-server",
		Version: vars.AppVersion,
		Short:   "codex-pulse-server Management CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return httpCmd.RunE(cmd, args)
		},
	}
)

func Execute() {
	initAll()
	if err := rootCmd.Execute(); err != nil {
		println(err)
		os.Exit(1)
	}
}

func initAll() {
	rootCmd.PersistentFlags().StringVarP(&configure, "config", "c", "./config/config.yml", "config file path")
	rootCmd.AddCommand(httpCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(databaseCommand())
}
