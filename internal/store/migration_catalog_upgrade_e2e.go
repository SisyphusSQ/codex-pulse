//go:build upgradee2e

package store

import (
	"context"
	"fmt"
	"strconv"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

// These values are accepted only by binaries explicitly built with the
// upgradee2e tag. Ordinary production builds do not contain this seam.
var (
	upgradeE2ESchemaLimit   string
	upgradeE2EFailMigration string
)

func applicationMigrationCatalog() []migrationDefinition {
	catalog := append([]migrationDefinition(nil), applicationMigrations...)
	if upgradeE2ESchemaLimit != "" {
		limit, err := strconv.Atoi(upgradeE2ESchemaLimit)
		if err != nil || limit < 1 || limit > len(catalog) {
			return nil
		}
		catalog = catalog[:limit]
	}
	if upgradeE2EFailMigration == "" {
		return catalog
	}
	failing, err := strconv.Atoi(upgradeE2EFailMigration)
	if err != nil || failing < 1 || failing > len(catalog) {
		return nil
	}
	original := catalog[failing-1].apply
	catalog[failing-1].apply = func(ctx context.Context, transaction *gorm.DB) error {
		if err := original(ctx, transaction); err != nil {
			return err
		}
		return fmt.Errorf("upgrade e2e injected migration %d failure", failing)
	}
	return catalog
}

func applicationMigrationVerifier() func(context.Context, storesqlite.WriteTx) error {
	if upgradeE2ESchemaLimit == "" {
		return verifyApplicationSchema
	}
	limit, err := strconv.Atoi(upgradeE2ESchemaLimit)
	if err != nil || limit != applicationSchemaV13Version {
		return func(context.Context, storesqlite.WriteTx) error {
			return fmt.Errorf("%w: invalid upgrade E2E schema verifier", ErrMigrationContract)
		}
	}
	return verifyApplicationSchemaV13ForUpgradeE2E
}

func verifyApplicationSchemaV13ForUpgradeE2E(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjectsThroughV13(), retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects, quotaSchemaObjects, quotaProjectionSchemaObjects,
		quotaScheduleSchemaObjects, metricsSchemaObjects,
	} {
		for _, object := range objects {
			exists, err := verifySchemaObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: missing %s %q", ErrSchemaContract, object.objectType, object.name)
			}
		}
	}
	if err := verifySourceFailureColumns(transaction); err != nil {
		return err
	}
	return verifyMetricsMigrationColumns(transaction)
}

// UpgradeE2ESchemaVersion performs a read-only ledger and catalog readback for
// the tagged application fixture. It does not apply migrations at runtime.
func (repository *Repository) UpgradeE2ESchemaVersion(ctx context.Context) (int, error) {
	if repository == nil || repository.database == nil {
		return 0, ErrInvalidRepository
	}
	catalog := applicationMigrationCatalog()
	target, err := validateMigrationCatalog(catalog)
	if err != nil {
		return 0, err
	}
	version := 0
	err = repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		state, err := inspectMigrationState(ctx, connection)
		if err != nil {
			return err
		}
		if err := validateMigrationState(state, catalog, target); err != nil {
			return err
		}
		version = state.version
		return nil
	})
	return version, err
}
