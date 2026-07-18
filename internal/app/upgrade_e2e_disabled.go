//go:build !upgradee2e

package app

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func loadUpgradeE2EOverrides() (upgradeE2EOverrides, bool, error) {
	return upgradeE2EOverrides{}, false, nil
}

func prepareUpgradeE2EPreferences(context.Context, *preferences.FileStore) error {
	return nil
}

func startUpgradeE2EAutomation(
	context.Context,
	*storesqlite.Store,
	*applicationUpdaterRuntime,
	func(),
) error {
	return nil
}

func finishUpgradeE2ERecovery(*migrationRecoveryController, func()) error {
	return nil
}
