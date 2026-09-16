package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

const codexSubscriptionAccountsMigrationName = "codex-subscription-accounts"

var codexSubscriptionSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "codex_subscription_detected_accounts", Statement: `CREATE TABLE IF NOT EXISTS codex_subscription_detected_accounts (
  account_scope TEXT PRIMARY KEY
    REFERENCES codex_account_scopes(account_scope) ON DELETE RESTRICT,
  detected_account_id TEXT NOT NULL UNIQUE,
  detected_email TEXT,
  email_match_key TEXT,
  detected_email_observed_at_ms INTEGER,
  automatic_plan TEXT,
  automatic_plan_state TEXT NOT NULL,
  automatic_plan_observed_at_ms INTEGER,
  revision INTEGER NOT NULL,
  CHECK (length(account_scope) = 64 AND account_scope NOT GLOB '*[^0-9a-f]*'),
  CHECK (length(detected_account_id) = 36),
  CHECK (detected_email IS NULL OR length(detected_email) BETWEEN 1 AND 320),
  CHECK (email_match_key IS NULL OR length(email_match_key) BETWEEN 1 AND 320),
  CHECK ((detected_email IS NULL) = (email_match_key IS NULL)),
  CHECK ((detected_email IS NULL) = (detected_email_observed_at_ms IS NULL)),
  CHECK (detected_email_observed_at_ms IS NULL OR detected_email_observed_at_ms >= 0),
  CHECK (automatic_plan IS NULL OR automatic_plan IN (
    'free','go','plus','pro_5x','pro_20x','team','business','enterprise','edu'
  )),
  CHECK (automatic_plan_state IN ('unavailable','known','unknown','conflict')),
  CHECK ((automatic_plan_state = 'known') = (automatic_plan IS NOT NULL)),
  CHECK ((automatic_plan_state = 'unavailable') = (automatic_plan_observed_at_ms IS NULL)),
  CHECK (automatic_plan_observed_at_ms IS NULL OR automatic_plan_observed_at_ms >= 0),
  CHECK (revision > 0)
) STRICT`},
	{ObjectType: "table", Name: "codex_subscription_manual_entries", Statement: `CREATE TABLE IF NOT EXISTS codex_subscription_manual_entries (
  manual_entry_id TEXT PRIMARY KEY,
  email TEXT,
  email_match_key TEXT,
  alias TEXT,
  manual_plan TEXT,
  membership_date TEXT,
  date_kind TEXT,
  revision INTEGER NOT NULL,
  created_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK (length(manual_entry_id) = 36),
  CHECK (email IS NULL OR length(email) BETWEEN 1 AND 320),
  CHECK (email_match_key IS NULL OR length(email_match_key) BETWEEN 1 AND 320),
  CHECK (alias IS NULL OR length(alias) BETWEEN 1 AND 128),
  CHECK (manual_plan IS NULL OR manual_plan IN (
    'free','go','plus','pro_5x','pro_20x','team','business','enterprise','edu'
  )),
  CHECK (membership_date IS NULL OR (
    length(membership_date) = 10 AND
    membership_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
  )),
  CHECK (date_kind IS NULL OR date_kind IN ('next_renewal','membership_expiry')),
  CHECK ((membership_date IS NULL) = (date_kind IS NULL)),
  CHECK (revision > 0),
  CHECK (created_at_ms >= 0 AND updated_at_ms >= created_at_ms)
) STRICT`},
	{ObjectType: "table", Name: "codex_subscription_links", Statement: `CREATE TABLE IF NOT EXISTS codex_subscription_links (
  account_scope TEXT PRIMARY KEY
    REFERENCES codex_subscription_detected_accounts(account_scope) ON DELETE RESTRICT,
  manual_entry_id TEXT NOT NULL UNIQUE
    REFERENCES codex_subscription_manual_entries(manual_entry_id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL,
  linked_at_ms INTEGER NOT NULL,
  updated_at_ms INTEGER NOT NULL,
  CHECK (revision > 0),
  CHECK (linked_at_ms >= 0 AND updated_at_ms >= linked_at_ms)
) STRICT`},
	{ObjectType: "index", Name: "idx_codex_subscription_detected_email_match", Statement: `CREATE INDEX IF NOT EXISTS idx_codex_subscription_detected_email_match
ON codex_subscription_detected_accounts(email_match_key)
WHERE email_match_key IS NOT NULL`},
	{ObjectType: "index", Name: "idx_codex_subscription_manual_email_match", Statement: `CREATE INDEX IF NOT EXISTS idx_codex_subscription_manual_email_match
ON codex_subscription_manual_entries(email_match_key)
WHERE email_match_key IS NOT NULL`},
	{ObjectType: "index", Name: "idx_codex_subscription_manual_updated", Statement: `CREATE INDEX IF NOT EXISTS idx_codex_subscription_manual_updated
ON codex_subscription_manual_entries(updated_at_ms DESC, manual_entry_id)`},
}
