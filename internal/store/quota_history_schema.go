package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

const quotaHistoryAssociationMigrationName = "codex-quota-history-association"

var quotaHistoryAssociationSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "codex_quota_history_association_generations", Statement: `CREATE TABLE IF NOT EXISTS codex_quota_history_association_generations (
  legacy_account_scope TEXT PRIMARY KEY CHECK (legacy_account_scope = 'default'),
  revision INTEGER NOT NULL CHECK (revision > 0),
  updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
) STRICT`},
	{ObjectType: "table", Name: "codex_quota_history_associations", Statement: `CREATE TABLE IF NOT EXISTS codex_quota_history_associations (
  legacy_account_scope TEXT PRIMARY KEY CHECK (legacy_account_scope = 'default'),
  account_scope TEXT NOT NULL UNIQUE
    REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision > 0),
  linked_at_ms INTEGER NOT NULL CHECK (linked_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= linked_at_ms)
) STRICT`},
}
