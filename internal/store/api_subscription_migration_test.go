package store

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestApplicationMigrationAddsAPISubscriptionBalanceHistory(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	report, err := NewRepository(database).MigrateApplicationSchema(context.Background())
	if err != nil {
		t.Fatalf("MigrateApplicationSchema() error = %v", err)
	}
	if report.TargetVersion != 29 {
		t.Fatalf("migration target = %d, want 29", report.TargetVersion)
	}
	if err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		if !connection.WithContext(ctx).Migrator().HasTable("api_subscription_balance_observations") {
			t.Fatal("api_subscription_balance_observations table is missing")
		}
		if !connection.WithContext(ctx).Migrator().HasTable("api_subscription_quota_observations") {
			t.Fatal("api_subscription_quota_observations table is missing")
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect migrated API subscription history: %v", err)
	}
}

func TestApplicationSchemaV28ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "d032f66d5729aaf113f8a3d296c8f6d898108e679fdeb6972aba2df8cfd16552"
	if got := applicationSchemaV28Checksum(); got != want {
		t.Fatalf("applicationSchemaV28Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV29ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "40481592995465335fc080b2de3b7b98818fa0db68edf5376418a9e5988376bb"
	if got := applicationSchemaV29Checksum(); got != want {
		t.Fatalf("applicationSchemaV29Checksum() = %q, want frozen %q", got, want)
	}
}
