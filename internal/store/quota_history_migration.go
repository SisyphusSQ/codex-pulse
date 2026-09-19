package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func migrateQuotaHistoryAssociationForV34(ctx context.Context, transaction *gorm.DB) error {
	if transaction == nil {
		return fmt.Errorf("%w: invalid quota history association migration database", ErrMigrationContract)
	}
	if err := storeschema.EnsureObjects(ctx, transaction, quotaHistoryAssociationSchemaObjects); err != nil {
		return err
	}
	return verifyNoForeignKeyViolations(ctx, transaction)
}
