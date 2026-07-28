//go:build !upgradee2e

package store

import (
	"context"

	"gorm.io/gorm"
)

func applicationMigrationCatalog() []migrationDefinition {
	return applicationMigrations
}

func applicationMigrationVerifier() func(context.Context, *gorm.DB) error {
	return verifyApplicationSchema
}
