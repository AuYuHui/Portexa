-- NULL preserves the distinction between old reports and newly measured zeros.
ALTER TABLE daily_reports ADD COLUMN winning_trades INTEGER CHECK (winning_trades >= 0);
ALTER TABLE daily_reports ADD COLUMN losing_trades INTEGER CHECK (losing_trades >= 0);
ALTER TABLE daily_reports ADD COLUMN breakeven_trades INTEGER CHECK (breakeven_trades >= 0);
ALTER TABLE daily_reports ADD COLUMN gross_profit TEXT;
ALTER TABLE daily_reports ADD COLUMN gross_loss TEXT;

ALTER TABLE accounts ADD COLUMN next_history_sync_at_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE accounts ADD COLUMN history_sync_paused INTEGER NOT NULL DEFAULT 0
    CHECK (history_sync_paused IN (0, 1));

CREATE TABLE history_coverage (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    unit_revision INTEGER NOT NULL,
    time_ruleset_id TEXT NOT NULL REFERENCES server_time_rule_sets(id),
    parent_id TEXT,
    start_wall INTEGER NOT NULL,
    end_wall INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending','running','complete','failed','split','blocked')),
    checked_at_ms INTEGER,
    next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    UNIQUE (account_id, unit_revision, time_ruleset_id, start_wall, end_wall),
    UNIQUE (id, account_id),
    FOREIGN KEY (parent_id, account_id) REFERENCES history_coverage(id, account_id),
    CHECK (start_wall < end_wall AND start_wall >= 0 AND end_wall <= 2147483647)
) STRICT;
CREATE INDEX history_coverage_due ON history_coverage(account_id,unit_revision,time_ruleset_id,next_attempt_at_ms);

CREATE TABLE history_runs (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    coverage_id TEXT NOT NULL,
    config_version INTEGER NOT NULL,
    unit_revision INTEGER NOT NULL,
    native_currency TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('running','succeeded','failed','canceled','interrupted')),
    started_at_ms INTEGER NOT NULL,
    finished_at_ms INTEGER,
    error_code TEXT,
    raw_status INTEGER,
    completion_reason TEXT,
    UNIQUE (id, account_id),
    FOREIGN KEY (account_id,server_id) REFERENCES accounts(id,server_id),
    FOREIGN KEY (server_id,endpoint_id) REFERENCES server_endpoints(server_id,id),
    FOREIGN KEY (coverage_id,account_id) REFERENCES history_coverage(id,account_id),
    CHECK (finished_at_ms IS NULL OR finished_at_ms >= started_at_ms)
) STRICT;
CREATE UNIQUE INDEX history_one_running ON history_runs(account_id) WHERE state='running';
CREATE INDEX history_runs_latest ON history_runs(account_id,started_at_ms DESC,id DESC);
CREATE TRIGGER history_terminal_immutable BEFORE UPDATE ON history_runs
WHEN OLD.state != 'running'
BEGIN SELECT RAISE(ABORT,'completed history run is immutable'); END;
CREATE TRIGGER history_excludes_other_fetch BEFORE INSERT ON history_runs
WHEN NEW.state='running' AND (
    EXISTS (SELECT 1 FROM sync_runs WHERE account_id=NEW.account_id AND state='running') OR
    EXISTS (SELECT 1 FROM daily_reports WHERE account_id=NEW.account_id AND state='running') OR
    EXISTS (SELECT 1 FROM clock_calibration_runs WHERE account_id=NEW.account_id AND state='running'))
BEGIN SELECT RAISE(ABORT,'account already has a running fetch'); END;
CREATE TRIGGER snapshot_excludes_history BEFORE INSERT ON sync_runs
WHEN NEW.state='running' AND EXISTS (SELECT 1 FROM history_runs WHERE account_id=NEW.account_id AND state='running')
BEGIN SELECT RAISE(ABORT,'account already has a running fetch'); END;
CREATE TRIGGER daily_excludes_history BEFORE INSERT ON daily_reports
WHEN NEW.state='running' AND EXISTS (SELECT 1 FROM history_runs WHERE account_id=NEW.account_id AND state='running')
BEGIN SELECT RAISE(ABORT,'account already has a running fetch'); END;
CREATE TRIGGER clock_excludes_history BEFORE INSERT ON clock_calibration_runs
WHEN NEW.state='running' AND EXISTS (SELECT 1 FROM history_runs WHERE account_id=NEW.account_id AND state='running')
BEGIN SELECT RAISE(ABORT,'account already has a running fetch'); END;

CREATE TABLE history_order_revisions (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    ticket INTEGER NOT NULL CHECK (ticket > 0),
    revision INTEGER NOT NULL CHECK (revision > 0),
    source_run_id TEXT NOT NULL,
    source_hash BLOB NOT NULL CHECK (length(source_hash)=32),
    unit_revision INTEGER NOT NULL,
    native_currency TEXT NOT NULL,
    command INTEGER NOT NULL CHECK (command BETWEEN 0 AND 7),
    symbol TEXT NOT NULL,
    digits INTEGER NOT NULL,
    volume_hundredths INTEGER NOT NULL,
    open_time_wall INTEGER NOT NULL,
    close_time_wall INTEGER NOT NULL,
    event_time_wall INTEGER NOT NULL,
    open_price TEXT NOT NULL,
    close_price TEXT NOT NULL,
    sl TEXT NOT NULL,
    tp TEXT NOT NULL,
    profit TEXT NOT NULL,
    commission TEXT NOT NULL,
    swap TEXT NOT NULL,
    taxes TEXT NOT NULL,
    net_profit TEXT NOT NULL,
    PRIMARY KEY (account_id,ticket,revision),
    FOREIGN KEY (source_run_id,account_id) REFERENCES history_runs(id,account_id)
) STRICT;
CREATE INDEX history_funds_page ON history_order_revisions(account_id,unit_revision,native_currency,event_time_wall DESC,ticket DESC)
    WHERE command IN (6,7);
CREATE TABLE history_order_heads (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    PRIMARY KEY (account_id,ticket),
    FOREIGN KEY (account_id,ticket,revision) REFERENCES history_order_revisions(account_id,ticket,revision)
) STRICT;
CREATE TRIGGER history_source_immutable BEFORE UPDATE ON history_order_revisions
BEGIN SELECT RAISE(ABORT,'append a history revision'); END;

CREATE TABLE cash_flow_classifications (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    based_trade_revision INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN
        ('deposit','withdrawal','rebate','fee','profit_share_income','profit_share_expense',
         'transfer_in','transfer_out','adjustment','unclassified')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 500),
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (account_id,ticket,revision),
    FOREIGN KEY (account_id,ticket,based_trade_revision) REFERENCES history_order_revisions(account_id,ticket,revision)
) STRICT;
CREATE TRIGGER cash_flow_classification_immutable BEFORE UPDATE ON cash_flow_classifications
BEGIN SELECT RAISE(ABORT,'append a classification revision'); END;
