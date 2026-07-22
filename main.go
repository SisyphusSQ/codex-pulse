package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/SisyphusSQ/codex-pulse/internal/helper"
)

var applicationVersion = "dev"

func main() {
	config, err := parseRuntimeConfig(os.Args[1:])
	if err != nil {
		log.Print("codex-pulse invalid_startup_configuration")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := helper.Run(ctx, config); err != nil {
		log.Print("codex-pulse helper_failed")
		os.Exit(1)
	}
}

func parseRuntimeConfig(arguments []string) (helper.RuntimeConfig, error) {
	flags := flag.NewFlagSet("codex-pulse", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	socketPath := flags.String("socket", "", "absolute Unix Domain Socket path")
	authFD := flags.Int("auth-fd", -1, "inherited authentication pipe descriptor")
	databasePath := flags.String("database-path", "", "optional isolated SQLite database path")
	preferencesPath := flags.String("preferences-path", "", "optional isolated preferences file path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *socketPath == "" || *authFD < 3 {
		return helper.RuntimeConfig{}, fmt.Errorf("invalid helper arguments")
	}
	return helper.RuntimeConfig{
		SocketPath: *socketPath, AuthFD: uintptr(*authFD), HelperVersion: applicationVersion,
		DatabasePath: *databasePath, PreferencesPath: *preferencesPath,
	}, nil
}
