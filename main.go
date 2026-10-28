package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	_ "time/tzdata"

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
	pipePath := flags.String("pipe", "", "local Windows Named Pipe path")
	runtimeDirectory := flags.String("runtime-directory", "", "private application runtime directory")
	authFD := flags.Int("auth-fd", -1, "inherited authentication pipe descriptor")
	databasePath := flags.String("database-path", "", "optional isolated SQLite database path")
	preferencesPath := flags.String("preferences-path", "", "optional isolated preferences file path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || (*socketPath == "") == (*pipePath == "") || *authFD < 3 {
		return helper.RuntimeConfig{}, fmt.Errorf("invalid helper arguments")
	}
	endpoint, err := platformEndpoint(*socketPath, *pipePath, *runtimeDirectory)
	if err != nil {
		return helper.RuntimeConfig{}, err
	}
	if *pipePath != "" {
		if *databasePath == "" {
			*databasePath = filepath.Join(*runtimeDirectory, "codex-pulse.db")
		}
		if *preferencesPath == "" {
			*preferencesPath = filepath.Join(*runtimeDirectory, "preferences.json")
		}
	}
	defaultCodexHome, err := resolveDefaultCodexHome()
	if err != nil {
		return helper.RuntimeConfig{}, fmt.Errorf("resolve default Codex Home: %w", err)
	}
	return helper.RuntimeConfig{
		SocketPath: endpoint, AuthFD: uintptr(*authFD), HelperVersion: applicationVersion,
		DatabasePath: *databasePath, PreferencesPath: *preferencesPath,
		DefaultCodexHome: defaultCodexHome,
		RuntimeDirectory: *runtimeDirectory,
	}, nil
}

func resolveDefaultCodexHome() (string, error) {
	if configured := os.Getenv("CODEX_HOME"); strings.TrimSpace(configured) != "" {
		return configured, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".codex"), nil
}
