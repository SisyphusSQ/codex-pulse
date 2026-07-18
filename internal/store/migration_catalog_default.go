//go:build !upgradee2e

package store

import (
	"context"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func applicationMigrationCatalog() []migrationDefinition {
	return applicationMigrations
}

func applicationMigrationVerifier() func(context.Context, storesqlite.WriteTx) error {
	return verifyApplicationSchema
}
