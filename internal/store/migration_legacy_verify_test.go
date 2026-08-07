package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	storeretention "github.com/SisyphusSQ/codex-pulse/internal/store/retention"
	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func verifyApplicationSchemaV18(ctx context.Context, transaction *gorm.DB) error {
	for _, objects := range [][]storeschema.Object{
		migrationSchemaObjects, coreSchemaObjects, currentRuntimeSchemaObjects(), storeretention.SchemaObjects(),
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects,
		quotaSchemaObjects, quotaProjectionSchemaObjects, quotaScheduleSchemaObjects,
		metricsSchemaObjects, quotaPerformanceSchemaObjects,
		storelight.SchemaObjectsThroughV19(),
	} {
		for _, object := range objects {
			exists, err := storeschema.VerifyObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: missing %s %q", storeschema.ErrContract, object.ObjectType, object.Name)
			}
		}
	}
	if err := verifySourceFailureColumns(transaction); err != nil {
		return err
	}
	if err := verifyMetricsMigrationColumns(transaction); err != nil {
		return err
	}
	return verifyLightModelAttributionColumns(transaction)
}

func verifyApplicationSchemaV16(ctx context.Context, transaction *gorm.DB) error {
	for _, objects := range [][]storeschema.Object{
		migrationSchemaObjects, coreSchemaObjects, currentRuntimeSchemaObjects(), storeretention.SchemaObjects(),
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects,
		quotaSchemaObjects, quotaProjectionSchemaObjects, quotaScheduleSchemaObjects,
		metricsSchemaObjects, quotaPerformanceSchemaObjects,
		storelight.SchemaObjects(),
	} {
		for _, object := range objects {
			exists, err := storeschema.VerifyObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: missing %s %q", storeschema.ErrContract, object.ObjectType, object.Name)
			}
		}
	}
	if err := verifySourceFailureColumns(transaction); err != nil {
		return err
	}
	return verifyMetricsMigrationColumns(transaction)
}

func verifyApplicationSchemaV15(ctx context.Context, transaction *gorm.DB) error {
	for _, objects := range [][]storeschema.Object{
		migrationSchemaObjects, coreSchemaObjects, currentRuntimeSchemaObjects(), storeretention.SchemaObjects(),
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects, lifecycleSchemaObjects,
		quotaSchemaObjects, quotaProjectionSchemaObjects, quotaScheduleSchemaObjects,
		metricsSchemaObjects, quotaPerformanceSchemaObjects,
	} {
		for _, object := range objects {
			exists, err := storeschema.VerifyObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: missing %s %q", storeschema.ErrContract, object.ObjectType, object.Name)
			}
		}
	}
	if err := verifySourceFailureColumns(transaction); err != nil {
		return err
	}
	return verifyMetricsMigrationColumns(transaction)
}
