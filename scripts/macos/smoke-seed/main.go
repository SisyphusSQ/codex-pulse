package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func main() {
	preferencesPath := flag.String("preferences", "", "isolated preferences path")
	homePath := flag.String("home", "", "isolated empty Codex Home")
	flag.Parse()
	if err := seed(*preferencesPath, *homePath); err != nil {
		fmt.Fprintln(os.Stderr, "smoke preferences seed failed")
		os.Exit(1)
	}
	fmt.Println("smoke preferences seed passed: isolated_home=yes")
}

func seed(preferencesPath, homePath string) error {
	if preferencesPath == "" || homePath == "" || !filepath.IsAbs(preferencesPath) || !filepath.IsAbs(homePath) ||
		filepath.Clean(preferencesPath) != preferencesPath || filepath.Clean(homePath) != homePath {
		return fmt.Errorf("invalid path")
	}
	root := filepath.Dir(preferencesPath)
	if filepath.Base(preferencesPath) != "preferences.json" || homePath != filepath.Join(root, "codex-home") ||
		!(strings.HasPrefix(root, "/private/tmp/cp-app-smoke.") || strings.HasPrefix(root, "/tmp/cp-app-smoke.")) {
		return fmt.Errorf("path outside isolated smoke root")
	}
	if err := os.MkdirAll(homePath, 0o700); err != nil {
		return err
	}
	directory, err := os.Open(homePath)
	if err != nil {
		return err
	}
	defer directory.Close()
	identity, err := homeidentity.FromDescriptor(int(directory.Fd()))
	if err != nil {
		return err
	}
	store, err := preferences.NewFileStore(preferencesPath)
	if err != nil {
		return err
	}
	return store.Confirm(context.Background(), preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: homePath, DeviceID: identity.DeviceID, Inode: identity.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
		OnlineQuotaEnabled: false, ResetCreditsEnabled: false,
	})
}
