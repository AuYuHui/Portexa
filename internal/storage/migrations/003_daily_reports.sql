ALTER TABLE accounts ADD COLUMN next_daily_sync_at_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE accounts ADD COLUMN daily_failure_count INTEGER NOT NULL DEFAULT 0
    CHECK (daily_failure_count >= 0);
ALTER TABLE accounts ADD COLUMN daily_sync_paused INTEGER NOT NULL DEFAULT 0
    CHECK (daily_sync_paused IN (0, 1));

CREATE TABLE daily_reports (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    config_version INTEGER NOT NULL,
    unit_revision INTEGER NOT NULL,
    time_ruleset_id TEXT NOT NULL,
    report_date TEXT NOT NULL,
    time_zone TEXT NOT NULL,
    window_start INTEGER NOT NULL,
    window_end INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN
        ('running', 'succeeded', 'failed', 'canceled', 'interrupted')),
    started_at_ms INTEGER NOT NULL,
    finished_at_ms INTEGER,
    native_currency TEXT,
    net_profit TEXT,
    balance_adjustment TEXT,
    balance_change TEXT,
    credit_change TEXT,
    closed_trades INTEGER NOT NULL DEFAULT 0 CHECK (closed_trades >= 0),
    error_code TEXT,
    raw_status INTEGER,
    completion_reason TEXT,
    FOREIGN KEY (account_id, server_id) REFERENCES accounts(id, server_id),
    FOREIGN KEY (server_id, endpoint_id) REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (server_id, time_ruleset_id) REFERENCES server_time_rule_sets(server_id, id),
    CHECK (window_start < window_end AND window_end <= 2147483647),
    CHECK (finished_at_ms IS NULL OR started_at_ms <= finished_at_ms),
    CHECK (state != 'succeeded' OR
        (finished_at_ms IS NOT NULL AND native_currency IS NOT NULL
         AND net_profit IS NOT NULL AND balance_adjustment IS NOT NULL
         AND balance_change IS NOT NULL AND credit_change IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX daily_one_running_account
    ON daily_reports(account_id) WHERE state = 'running';
CREATE INDEX daily_account_date
    ON daily_reports(account_id, report_date, time_ruleset_id, config_version,
        unit_revision, started_at_ms DESC, id DESC);
CREATE INDEX daily_success_date
    ON daily_reports(account_id, report_date, time_ruleset_id, unit_revision,
        started_at_ms DESC, id DESC) WHERE state = 'succeeded';
CREATE INDEX accounts_daily_due
    ON accounts(sync_enabled, daily_sync_paused, next_daily_sync_at_ms, id);

CREATE TRIGGER daily_running_excludes_snapshot
BEFORE INSERT ON daily_reports
WHEN NEW.state = 'running' AND EXISTS (
    SELECT 1 FROM sync_runs WHERE account_id = NEW.account_id AND state = 'running'
)
BEGIN
    SELECT RAISE(ABORT, 'account already has a running fetch');
END;
CREATE TRIGGER snapshot_running_excludes_daily
BEFORE INSERT ON sync_runs
WHEN NEW.state = 'running' AND EXISTS (
    SELECT 1 FROM daily_reports WHERE account_id = NEW.account_id AND state = 'running'
)
BEGIN
    SELECT RAISE(ABORT, 'account already has a running fetch');
END;
CREATE TRIGGER daily_terminal_is_immutable
BEFORE UPDATE ON daily_reports
WHEN OLD.state IN ('succeeded', 'failed', 'canceled', 'interrupted')
BEGIN
    SELECT RAISE(ABORT, 'completed daily report is immutable');
END;
