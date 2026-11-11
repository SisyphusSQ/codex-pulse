-- TOO-702 / schema v7: official billing Credits; earned resets remain independent.
CREATE TABLE IF NOT EXISTS pulse_paid_credits (
    id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_key TEXT NOT NULL,
    local_scope TEXT NOT NULL,
    limit_id TEXT NOT NULL,
    state TEXT NOT NULL,
    balance TEXT NULL,
    has_credits INTEGER NULL,
    unlimited INTEGER NULL,
    observed_at_ms INTEGER NOT NULL,
    received_at_ms INTEGER NOT NULL,
PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS idx_paid_credits_account_time ON pulse_paid_credits(account_key,limit_id,observed_at_ms);
CREATE INDEX IF NOT EXISTS idx_paid_credits_client_time ON pulse_paid_credits(client_id,observed_at_ms);
