-- Replace the table-level login constraint with an index on active accounts.
CREATE TABLE accounts_v16 (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    pinned_endpoint_id TEXT NOT NULL,
    login INTEGER NOT NULL CHECK (login BETWEEN 1 AND 9007199254740991),
    label TEXT NOT NULL,
    currency TEXT,
    usd_scale TEXT,
    unit_revision INTEGER NOT NULL DEFAULT 1 CHECK (unit_revision > 0),
    unit_verified_at_ms INTEGER,
    credential_envelope BLOB NOT NULL,
    credential_key_id TEXT NOT NULL,
    credential_version INTEGER NOT NULL CHECK (credential_version > 0),
    config_version INTEGER NOT NULL DEFAULT 1 CHECK (config_version > 0),
    sync_enabled INTEGER NOT NULL DEFAULT 0 CHECK (sync_enabled IN (0, 1)),
    next_sync_at_ms INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    current_snapshot_id TEXT,
    included_at_ms INTEGER,
    initial_statistical_equity TEXT,
    removed_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    next_daily_sync_at_ms INTEGER NOT NULL DEFAULT 0,
    daily_failure_count INTEGER NOT NULL DEFAULT 0
        CHECK (daily_failure_count >= 0),
    daily_sync_paused INTEGER NOT NULL DEFAULT 0
        CHECK (daily_sync_paused IN (0, 1)),
    next_clock_sync_at_ms INTEGER NOT NULL DEFAULT 0,
    clock_failure_count INTEGER NOT NULL DEFAULT 0 CHECK (clock_failure_count >= 0),
    clock_sync_paused INTEGER NOT NULL DEFAULT 0 CHECK (clock_sync_paused IN (0, 1)),
    next_history_sync_at_ms INTEGER NOT NULL DEFAULT 0,
    history_sync_paused INTEGER NOT NULL DEFAULT 0
        CHECK (history_sync_paused IN (0, 1)),
    rest_blocked_until_ms INTEGER NOT NULL DEFAULT 0,
    UNIQUE (id, server_id),
    FOREIGN KEY (server_id, pinned_endpoint_id)
        REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (id, current_snapshot_id)
        REFERENCES account_snapshots(account_id, id)
) STRICT;

INSERT INTO accounts_v16 (
    id, server_id, pinned_endpoint_id, login, label, currency, usd_scale, unit_revision, unit_verified_at_ms,
    credential_envelope, credential_key_id, credential_version, config_version, sync_enabled, next_sync_at_ms,
    failure_count, current_snapshot_id, included_at_ms, initial_statistical_equity, removed_at_ms, created_at_ms,
    next_daily_sync_at_ms, daily_failure_count, daily_sync_paused, next_clock_sync_at_ms, clock_failure_count,
    clock_sync_paused, next_history_sync_at_ms, history_sync_paused, rest_blocked_until_ms
) SELECT
    id, server_id, pinned_endpoint_id, login, label, currency, usd_scale, unit_revision, unit_verified_at_ms,
    credential_envelope, credential_key_id, credential_version, config_version, sync_enabled, next_sync_at_ms,
    failure_count, current_snapshot_id, included_at_ms, initial_statistical_equity, removed_at_ms, created_at_ms,
    next_daily_sync_at_ms, daily_failure_count, daily_sync_paused, next_clock_sync_at_ms, clock_failure_count,
    clock_sync_paused, next_history_sync_at_ms, history_sync_paused, rest_blocked_until_ms
FROM accounts;
DROP TABLE accounts;
ALTER TABLE accounts_v16 RENAME TO accounts;
