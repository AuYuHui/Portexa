CREATE TABLE auth_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    auth_version INTEGER NOT NULL CHECK (auth_version > 0),
    changed_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    token_sha256 BLOB PRIMARY KEY CHECK (length(token_sha256) = 32),
    auth_version INTEGER NOT NULL CHECK (auth_version > 0),
    created_at_ms INTEGER NOT NULL,
    last_interactive_at_ms INTEGER NOT NULL,
    idle_expires_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    revoked_at_ms INTEGER,
    CHECK (created_at_ms < expires_at_ms),
    CHECK (idle_expires_at_ms <= expires_at_ms)
) STRICT;
CREATE INDEX sessions_expiry ON sessions(expires_at_ms);

CREATE TABLE servers (
    id TEXT PRIMARY KEY,
    protocol TEXT NOT NULL CHECK (protocol = 'mt4'),
    broker_name TEXT NOT NULL,
    server_name TEXT NOT NULL,
    active_time_ruleset_id TEXT,
    created_at_ms INTEGER NOT NULL,
    retired_at_ms INTEGER,
    FOREIGN KEY (id, active_time_ruleset_id)
        REFERENCES server_time_rule_sets(server_id, id)
) STRICT;

CREATE TABLE server_endpoints (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    ip_address TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    state TEXT NOT NULL CHECK (state IN ('pending', 'reviewed', 'retired')),
    evidence_ref TEXT,
    evidence_sha256 BLOB,
    reviewed_at_ms INTEGER,
    risk_accepted_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    UNIQUE (server_id, revision),
    UNIQUE (server_id, id),
    CHECK (state != 'reviewed' OR
        (evidence_ref IS NOT NULL AND evidence_sha256 IS NOT NULL
         AND length(evidence_sha256) = 32 AND reviewed_at_ms IS NOT NULL)),
    CHECK (risk_accepted_at_ms IS NULL OR state IN ('reviewed', 'retired'))
) STRICT;
CREATE TRIGGER endpoint_address_is_immutable
BEFORE UPDATE OF server_id, revision, ip_address, port ON server_endpoints
BEGIN
    SELECT RAISE(ABORT, 'create a new endpoint revision');
END;

CREATE TABLE server_time_rule_sets (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    rules_json TEXT NOT NULL CHECK (json_valid(rules_json)),
    evidence_ref TEXT,
    verified_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    UNIQUE (server_id, revision),
    UNIQUE (server_id, id)
) STRICT;

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    pinned_endpoint_id TEXT NOT NULL,
    login INTEGER NOT NULL CHECK (login BETWEEN 1 AND 4294967295),
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
    UNIQUE (server_id, login),
    UNIQUE (id, server_id),
    FOREIGN KEY (server_id, pinned_endpoint_id)
        REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (id, current_snapshot_id)
        REFERENCES account_snapshots(account_id, id)
) STRICT;
CREATE INDEX accounts_due ON accounts(sync_enabled, next_sync_at_ms, id);

CREATE TABLE sync_runs (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    credential_version INTEGER NOT NULL,
    config_version INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'snapshot'),
    state TEXT NOT NULL CHECK (state IN
        ('queued', 'running', 'succeeded', 'failed', 'canceled', 'interrupted')),
    due_at_ms INTEGER NOT NULL,
    not_before_ms INTEGER NOT NULL,
    started_at_ms INTEGER,
    finished_at_ms INTEGER,
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    error_code TEXT,
    error_operation TEXT,
    raw_status INTEGER,
    completion_reason TEXT,
    UNIQUE (id, account_id),
    FOREIGN KEY (account_id, server_id) REFERENCES accounts(id, server_id),
    FOREIGN KEY (server_id, endpoint_id) REFERENCES server_endpoints(server_id, id)
) STRICT;
CREATE UNIQUE INDEX sync_one_live_kind
    ON sync_runs(account_id, kind) WHERE state IN ('queued', 'running');
CREATE UNIQUE INDEX sync_one_running_account
    ON sync_runs(account_id) WHERE state = 'running';
CREATE INDEX sync_due ON sync_runs(state, not_before_ms, due_at_ms);
CREATE INDEX sync_account_time ON sync_runs(account_id, started_at_ms DESC, id DESC);
CREATE TRIGGER sync_terminal_state_is_immutable
BEFORE UPDATE OF state ON sync_runs
WHEN OLD.state IN ('succeeded', 'failed', 'canceled', 'interrupted')
 AND NEW.state != OLD.state
BEGIN
    SELECT RAISE(ABORT, 'terminal run cannot change state');
END;

CREATE TABLE account_snapshots (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    sync_run_id TEXT NOT NULL UNIQUE,
    observed_from_ms INTEGER NOT NULL,
    observed_to_ms INTEGER NOT NULL,
    native_currency TEXT NOT NULL,
    usd_scale TEXT,
    unit_revision INTEGER NOT NULL,
    balance TEXT NOT NULL,
    credit TEXT NOT NULL,
    equity TEXT NOT NULL,
    floating_net_profit TEXT NOT NULL,
    margin TEXT,
    margin_available INTEGER NOT NULL CHECK (margin_available IN (0, 1)),
    leverage INTEGER NOT NULL,
    CHECK (observed_from_ms <= observed_to_ms),
    CHECK ((margin_available = 1 AND margin IS NOT NULL) OR
        (margin_available = 0 AND margin IS NULL)),
    UNIQUE (account_id, id),
    FOREIGN KEY (sync_run_id, account_id) REFERENCES sync_runs(id, account_id)
) STRICT;
CREATE INDEX snapshots_account_time
    ON account_snapshots(account_id, observed_to_ms DESC, id);
CREATE INDEX snapshots_retention ON account_snapshots(observed_to_ms, id);

CREATE TABLE positions (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    snapshot_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    command INTEGER NOT NULL CHECK (command BETWEEN 0 AND 5),
    digits INTEGER NOT NULL,
    volume_hundredths INTEGER NOT NULL,
    open_time_wall INTEGER NOT NULL,
    close_time_wall INTEGER NOT NULL,
    open_price TEXT NOT NULL,
    current_price TEXT NOT NULL,
    sl TEXT NOT NULL,
    tp TEXT NOT NULL,
    profit TEXT NOT NULL,
    commission TEXT NOT NULL,
    swap TEXT NOT NULL,
    taxes TEXT NOT NULL,
    PRIMARY KEY (account_id, ticket),
    FOREIGN KEY (account_id, snapshot_id)
        REFERENCES account_snapshots(account_id, id)
) STRICT;
CREATE INDEX positions_open_time ON positions(account_id, open_time_wall, ticket);
