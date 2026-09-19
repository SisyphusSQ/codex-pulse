package store

import (
	"context"
	"testing"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func TestQuotaHistoryAssociationMigrationCreatesReversibleLinkOnly(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	if err := NewRepository(database).EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	if applicationSchemaVersion != 34 {
		t.Fatalf("applicationSchemaVersion = %d, want 34", applicationSchemaVersion)
	}
	assertMigrationVersionAndHistory(t, database, applicationSchemaVersion, int64(applicationSchemaVersion))
	var observationCount int64
	err := database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
		objects := []storeschema.Object{{
			ObjectType: "table",
			Name:       "codex_quota_history_association_generations",
			Statement: `CREATE TABLE IF NOT EXISTS codex_quota_history_association_generations (
  legacy_account_scope TEXT PRIMARY KEY CHECK (legacy_account_scope = 'default'),
  revision INTEGER NOT NULL CHECK (revision > 0),
  updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
) STRICT`,
		}, {
			ObjectType: "table",
			Name:       "codex_quota_history_associations",
			Statement: `CREATE TABLE IF NOT EXISTS codex_quota_history_associations (
  legacy_account_scope TEXT PRIMARY KEY CHECK (legacy_account_scope = 'default'),
  account_scope TEXT NOT NULL UNIQUE
    REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision > 0),
  linked_at_ms INTEGER NOT NULL CHECK (linked_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= linked_at_ms)
) STRICT`,
		}}
		for _, object := range objects {
			if exists, verifyErr := storeschema.VerifyObject(ctx, connection, object); verifyErr != nil {
				return verifyErr
			} else if !exists {
				t.Fatalf("quota history schema object %q is missing", object.Name)
			}
		}
		return connection.WithContext(ctx).Table("quota_observations").Count(&observationCount).Error
	})
	if err != nil {
		t.Fatalf("read quota history schema: %v", err)
	}
	if observationCount != 0 {
		t.Fatalf("migration copied or rewrote quota observations: count=%d", observationCount)
	}
}

func TestApplicationSchemaV34ChecksumIsFrozen(t *testing.T) {
	t.Parallel()
	const want = "74e453f11f46fb98dc052c1d7ac77ce80b4b103a5963ea7fd8f8a032c3c9784e"
	if got := applicationSchemaV34Checksum(); got != want {
		t.Fatalf("applicationSchemaV34Checksum() = %q, want frozen %q", got, want)
	}
}
