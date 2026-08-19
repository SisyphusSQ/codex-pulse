package store

import storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"

var apiSubscriptionSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "api_subscription_balance_observations", Statement: `CREATE TABLE IF NOT EXISTS api_subscription_balance_observations (
		service TEXT NOT NULL CHECK (service = 'deepseek'),
		credential_epoch TEXT NOT NULL CHECK (length(credential_epoch) BETWEEN 1 AND 128),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
		is_available INTEGER NOT NULL CHECK (is_available IN (0,1)),
		total_balance TEXT NOT NULL CHECK (length(total_balance) BETWEEN 1 AND 128),
		granted_balance TEXT NOT NULL CHECK (length(granted_balance) BETWEEN 1 AND 128),
		topped_up_balance TEXT NOT NULL CHECK (length(topped_up_balance) BETWEEN 1 AND 128),
		PRIMARY KEY (service, credential_epoch, observed_at_ms, currency)
	) STRICT`},
	{ObjectType: "index", Name: "idx_api_subscription_balance_period", Statement: `CREATE INDEX IF NOT EXISTS idx_api_subscription_balance_period
		ON api_subscription_balance_observations(service, credential_epoch, currency, observed_at_ms)`},
}

var apiSubscriptionQuotaSchemaObjects = []storeschema.Object{
	{ObjectType: "table", Name: "api_subscription_quota_observations", Statement: `CREATE TABLE IF NOT EXISTS api_subscription_quota_observations (
		service TEXT NOT NULL CHECK (service = 'opencode_go'),
		credential_epoch TEXT NOT NULL CHECK (length(credential_epoch) BETWEEN 1 AND 128),
		observed_at_ms INTEGER NOT NULL CHECK (observed_at_ms >= 0),
		window_kind TEXT NOT NULL CHECK (window_kind IN ('five_hour','weekly','monthly')),
		status TEXT NOT NULL CHECK (status IN ('ok','rate-limited')),
		used_percent REAL NOT NULL CHECK (used_percent >= 0 AND used_percent <= 100),
		remaining_percent REAL NOT NULL CHECK (remaining_percent >= 0 AND remaining_percent <= 100),
		resets_at_ms INTEGER NOT NULL CHECK (resets_at_ms >= 0),
		PRIMARY KEY (service, credential_epoch, observed_at_ms, window_kind)
	) STRICT`},
	{ObjectType: "index", Name: "idx_api_subscription_quota_period", Statement: `CREATE INDEX IF NOT EXISTS idx_api_subscription_quota_period
		ON api_subscription_quota_observations(service, credential_epoch, window_kind, observed_at_ms)`},
}
