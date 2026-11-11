-- TOO-702 / schema v8: official billing Credits; earned resets remain independent.
CREATE TABLE IF NOT EXISTS pulse_paid_credits (
    id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    client_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    account_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    local_scope VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    limit_id VARCHAR(512) COLLATE utf8mb4_bin NOT NULL,
    state VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    balance VARCHAR(49) CHARACTER SET ascii COLLATE ascii_bin NULL,
    has_credits BOOLEAN NULL,
    unlimited BOOLEAN NULL,
    observed_at_ms BIGINT NOT NULL,
    received_at_ms BIGINT NOT NULL,
PRIMARY KEY (id),
KEY idx_paid_credits_account_time (account_key,limit_id,observed_at_ms),
KEY idx_paid_credits_client_time (client_id,observed_at_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
