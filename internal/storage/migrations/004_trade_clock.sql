ALTER TABLE accounts ADD COLUMN next_clock_sync_at_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE accounts ADD COLUMN clock_failure_count INTEGER NOT NULL DEFAULT 0 CHECK (clock_failure_count >= 0);
ALTER TABLE accounts ADD COLUMN clock_sync_paused INTEGER NOT NULL DEFAULT 0 CHECK (clock_sync_paused IN (0, 1));
CREATE INDEX accounts_clock_due ON accounts(sync_enabled, clock_sync_paused, next_clock_sync_at_ms, id);

-- Only disappearances between two complete, nearby position snapshots qualify.
-- These private observations contain no credentials or protocol payloads.
CREATE TABLE trade_clock_observations (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    config_version INTEGER NOT NULL,
    before_snapshot_id TEXT NOT NULL,
    after_snapshot_id TEXT NOT NULL,
    ticket INTEGER NOT NULL CHECK (ticket > 0),
    command INTEGER NOT NULL CHECK (command IN (0, 1)),
    symbol TEXT NOT NULL,
    open_time_wall INTEGER NOT NULL,
    window_from_ms INTEGER NOT NULL,
    window_to_ms INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'accepted', 'rejected')),
    close_time_wall INTEGER,
    offset_minutes INTEGER CHECK (offset_minutes BETWEEN -720 AND 840 AND offset_minutes % 15 = 0),
    checked_at_ms INTEGER,
    UNIQUE (account_id, config_version, ticket),
    FOREIGN KEY (account_id, server_id) REFERENCES accounts(id, server_id),
    FOREIGN KEY (server_id, endpoint_id) REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (account_id, before_snapshot_id) REFERENCES account_snapshots(account_id, id),
    FOREIGN KEY (account_id, after_snapshot_id) REFERENCES account_snapshots(account_id, id),
    CHECK (window_from_ms <= window_to_ms AND window_to_ms - window_from_ms <= 180000),
    CHECK (state != 'accepted' OR (close_time_wall > 0 AND offset_minutes IS NOT NULL))
) STRICT;
CREATE INDEX clock_pending_account ON trade_clock_observations(account_id, window_to_ms) WHERE state = 'pending';
CREATE INDEX clock_server_evidence ON trade_clock_observations(server_id, window_to_ms DESC) WHERE state = 'accepted';
CREATE TRIGGER clock_observation_terminal_is_immutable
BEFORE UPDATE ON trade_clock_observations WHEN OLD.state != 'pending'
BEGIN SELECT RAISE(ABORT, 'checked clock observation is immutable'); END;

CREATE TABLE server_clock_calibrations (
    server_id TEXT PRIMARY KEY REFERENCES servers(id),
    status TEXT NOT NULL CHECK (status IN ('learning', 'calibrated', 'conflict')),
    offset_minutes INTEGER,
    evidence_count INTEGER NOT NULL DEFAULT 0,
    event_window_count INTEGER NOT NULL DEFAULT 0,
    observed_at_ms INTEGER,
    expires_at_ms INTEGER,
    CHECK (offset_minutes IS NULL OR (offset_minutes BETWEEN -720 AND 840 AND offset_minutes % 15 = 0))
) STRICT;

-- Bootstrap history does not require an already confirmed calendar day.
CREATE TABLE clock_calibration_runs (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    config_version INTEGER NOT NULL,
    window_start INTEGER NOT NULL,
    window_end INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('running', 'succeeded', 'failed', 'canceled', 'interrupted')),
    started_at_ms INTEGER NOT NULL,
    finished_at_ms INTEGER,
    error_code TEXT,
    completion_reason TEXT,
    FOREIGN KEY (account_id, server_id) REFERENCES accounts(id, server_id),
    FOREIGN KEY (server_id, endpoint_id) REFERENCES server_endpoints(server_id, id),
    CHECK (window_start < window_end AND window_end <= 2147483647),
    CHECK (finished_at_ms IS NULL OR started_at_ms <= finished_at_ms)
) STRICT;
CREATE UNIQUE INDEX clock_one_running_account ON clock_calibration_runs(account_id) WHERE state = 'running';
CREATE TRIGGER clock_running_excludes_fetch
BEFORE INSERT ON clock_calibration_runs
WHEN NEW.state = 'running' AND (
    EXISTS (SELECT 1 FROM sync_runs WHERE account_id = NEW.account_id AND state = 'running') OR
    EXISTS (SELECT 1 FROM daily_reports WHERE account_id = NEW.account_id AND state = 'running')
)
BEGIN SELECT RAISE(ABORT, 'account already has a running fetch'); END;
CREATE TRIGGER snapshot_running_excludes_clock
BEFORE INSERT ON sync_runs
WHEN NEW.state = 'running' AND EXISTS (
    SELECT 1 FROM clock_calibration_runs WHERE account_id = NEW.account_id AND state = 'running'
)
BEGIN SELECT RAISE(ABORT, 'account already has a running fetch'); END;
CREATE TRIGGER daily_running_excludes_clock
BEFORE INSERT ON daily_reports
WHEN NEW.state = 'running' AND EXISTS (
    SELECT 1 FROM clock_calibration_runs WHERE account_id = NEW.account_id AND state = 'running'
)
BEGIN SELECT RAISE(ABORT, 'account already has a running fetch'); END;
CREATE TRIGGER clock_terminal_is_immutable
BEFORE UPDATE ON clock_calibration_runs
WHEN OLD.state != 'running'
BEGIN SELECT RAISE(ABORT, 'completed calibration run is immutable'); END;
