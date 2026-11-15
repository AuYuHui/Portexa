ALTER TABLE market_rest_policies ADD COLUMN learning_enabled INTEGER NOT NULL DEFAULT 0 CHECK(learning_enabled IN (0,1));
ALTER TABLE market_rest_policies ADD COLUMN learning_epoch_ms INTEGER NOT NULL DEFAULT 0;
INSERT INTO market_rest_policies(revision,template,effective_at_ms,excluded_accounts_json,learning_enabled,learning_epoch_ms)
SELECT (SELECT MAX(revision)+1 FROM market_rest_policies),p.template,CAST(unixepoch('subsec')*1000 AS INTEGER),
    p.excluded_accounts_json,1,CAST(unixepoch('subsec')*1000 AS INTEGER)
FROM market_rest_policies p JOIN collection_settings c ON c.market_rest_policy_revision=p.revision;
UPDATE collection_settings SET market_rest_policy_revision=(SELECT MAX(revision) FROM market_rest_policies);

CREATE TABLE market_learning_groups (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    endpoint_id TEXT NOT NULL REFERENCES server_endpoints(id),
    symbol TEXT NOT NULL,
    epoch_ms INTEGER NOT NULL,
    last_observed_ms INTEGER NOT NULL,
    next_review_ms INTEGER NOT NULL DEFAULT 0,
    reviewed_at_ms INTEGER NOT NULL DEFAULT 0,
    valid_weekends INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'learning',
    reason TEXT NOT NULL DEFAULT 'collecting',
    candidate_model_id TEXT NOT NULL DEFAULT '',
    active_model_id TEXT NOT NULL DEFAULT '',
    UNIQUE(server_id,endpoint_id,symbol,epoch_ms)
) STRICT;
CREATE INDEX market_learning_review ON market_learning_groups(next_review_ms,id);

CREATE TABLE market_activity_observations (
    id INTEGER PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES market_learning_groups(id),
    account_id TEXT NOT NULL REFERENCES accounts(id),
    snapshot_id TEXT NOT NULL REFERENCES account_snapshots(id),
    previous_snapshot_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL REFERENCES sync_runs(id),
    config_version INTEGER NOT NULL,
    unit_revision INTEGER NOT NULL,
    observed_from_ms INTEGER NOT NULL,
    observed_to_ms INTEGER NOT NULL,
    previous_to_ms INTEGER NOT NULL,
    max_gap_ms INTEGER NOT NULL,
    healthy INTEGER NOT NULL CHECK(healthy IN (0,1)),
    comparable INTEGER NOT NULL CHECK(comparable IN (0,1)),
    quote_changed INTEGER NOT NULL CHECK(quote_changed IN (0,1)),
    position_changed INTEGER NOT NULL CHECK(position_changed IN (0,1)),
    account_changed INTEGER NOT NULL CHECK(account_changed IN (0,1)),
    quote_price TEXT NOT NULL,
    quote_anchor TEXT NOT NULL,
    UNIQUE(group_id,snapshot_id)
) STRICT;
CREATE INDEX market_activity_group_time ON market_activity_observations(group_id,observed_to_ms,id);
CREATE TRIGGER market_activity_no_update BEFORE UPDATE ON market_activity_observations
BEGIN SELECT RAISE(ABORT,'market activity evidence is immutable'); END;

CREATE TRIGGER market_activity_no_delete BEFORE DELETE ON market_activity_observations
BEGIN SELECT RAISE(ABORT,'market activity evidence is immutable'); END;

CREATE TABLE market_learning_models (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES market_learning_groups(id),
    algorithm TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    trained_through_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    close_minute INTEGER NOT NULL,
    open_minute INTEGER NOT NULL,
    close_min_minute INTEGER NOT NULL,
    close_max_minute INTEGER NOT NULL,
    open_min_minute INTEGER NOT NULL,
    open_max_minute INTEGER NOT NULL,
    training_keys_json TEXT NOT NULL CHECK(json_valid(training_keys_json)),
    evidence_sha256 TEXT NOT NULL
) STRICT;
CREATE TRIGGER market_model_no_update BEFORE UPDATE ON market_learning_models
BEGIN SELECT RAISE(ABORT,'market learning model is immutable'); END;
CREATE TRIGGER market_model_no_delete BEFORE DELETE ON market_learning_models
BEGIN SELECT RAISE(ABORT,'market learning model is immutable'); END;

CREATE TABLE market_learning_events (
    id INTEGER PRIMARY KEY,
    model_id TEXT NOT NULL REFERENCES market_learning_models(id),
    state TEXT NOT NULL CHECK(state IN ('candidate','adopted','suspended','rejected')),
    effective_at_ms INTEGER NOT NULL,
    blocked_until_ms INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL,
    source_run_id TEXT REFERENCES sync_runs(id),
    validation_key TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX market_learning_model_events ON market_learning_events(model_id,id DESC);
CREATE TRIGGER market_learning_event_no_update BEFORE UPDATE ON market_learning_events
BEGIN SELECT RAISE(ABORT,'market learning event is immutable'); END;
CREATE TRIGGER market_learning_event_no_delete BEFORE DELETE ON market_learning_events
BEGIN SELECT RAISE(ABORT,'market learning event is immutable'); END;
CREATE TRIGGER analytics_learning_event AFTER INSERT ON market_learning_events
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;

CREATE TABLE market_rest_windows (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    policy_revision INTEGER NOT NULL REFERENCES market_rest_policies(revision),
    kind TEXT NOT NULL CHECK(kind IN ('template','learned')),
    from_ms INTEGER NOT NULL,
    until_ms INTEGER NOT NULL CHECK(until_ms>from_ms),
    effective_at_ms INTEGER NOT NULL,
    model_ids_json TEXT NOT NULL CHECK(json_valid(model_ids_json))
) STRICT;
CREATE TRIGGER market_window_no_update BEFORE UPDATE ON market_rest_windows
BEGIN SELECT RAISE(ABORT,'market rest window is immutable'); END;
CREATE TRIGGER market_window_no_delete BEFORE DELETE ON market_rest_windows
BEGIN SELECT RAISE(ABORT,'market rest window is immutable'); END;
CREATE TRIGGER analytics_learning_window AFTER INSERT ON market_rest_windows
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
ALTER TABLE account_snapshots ADD COLUMN rest_window_id TEXT NOT NULL DEFAULT '' CHECK(length(rest_window_id) IN (0,64));
