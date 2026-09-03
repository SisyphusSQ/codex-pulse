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
	if report.TargetVersion != 31 {
		t.Fatalf("migration target = %d, want 31", report.TargetVersion)
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

func TestApplicationSchemaV30ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "1dff5679bb5c23d03a8c585805f10c73fe82b8af536a90e57ea6c8d2fd5229c4"
	if got := applicationSchemaV30Checksum(); got != want {
		t.Fatalf("applicationSchemaV30Checksum() = %q, want frozen %q", got, want)
	}
}

func TestApplicationSchemaV31ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "2ad86dfc7e17ca34217875545af500d9cd0607bfd4da36859f7b5e184757bc63"
	if got := applicationSchemaV31Checksum(); got != want {
		t.Fatalf("applicationSchemaV31Checksum() = %q, want frozen %q", got, want)
	}
}
