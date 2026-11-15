CREATE TABLE market_rest_policies (
    revision INTEGER PRIMARY KEY CHECK(revision>0),
    template TEXT NOT NULL CHECK(template IN ('off','fx_metals_weekend_v1')),
    effective_at_ms INTEGER NOT NULL,
    excluded_accounts_json TEXT NOT NULL CHECK(json_valid(excluded_accounts_json))
) STRICT;
INSERT INTO market_rest_policies VALUES
    (1,'fx_metals_weekend_v1',CAST(unixepoch('subsec')*1000 AS INTEGER),'[]');
CREATE TRIGGER market_rest_policy_no_update BEFORE UPDATE ON market_rest_policies
BEGIN SELECT RAISE(ABORT,'market rest policy is immutable'); END;
CREATE TRIGGER market_rest_policy_no_delete BEFORE DELETE ON market_rest_policies
BEGIN SELECT RAISE(ABORT,'market rest policy is immutable'); END;
CREATE TRIGGER analytics_rest_policy AFTER INSERT ON market_rest_policies
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;

CREATE TABLE collection_settings_v11 (
    id INTEGER PRIMARY KEY CHECK(id=1),
    snapshot_interval_seconds INTEGER NOT NULL DEFAULT 60
        CHECK(snapshot_interval_seconds IN (30,60,120,300)),
    updated_at_ms INTEGER NOT NULL DEFAULT 0,
    market_rest_policy_revision INTEGER NOT NULL DEFAULT 1 REFERENCES market_rest_policies(revision)
) STRICT;
INSERT INTO collection_settings_v11(id,snapshot_interval_seconds,updated_at_ms)
    SELECT id,snapshot_interval_seconds,updated_at_ms FROM collection_settings;
DROP TABLE collection_settings;
ALTER TABLE collection_settings_v11 RENAME TO collection_settings;
ALTER TABLE accounts ADD COLUMN rest_blocked_until_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE account_snapshots ADD COLUMN rest_policy_revision INTEGER NOT NULL DEFAULT 0 CHECK(rest_policy_revision>=0);
ALTER TABLE account_snapshots ADD COLUMN rest_verified INTEGER NOT NULL DEFAULT 0 CHECK(rest_verified IN (0,1));
ALTER TABLE account_snapshots ADD COLUMN state_fingerprint TEXT NOT NULL DEFAULT ''
    CHECK(length(state_fingerprint) IN (0,64));
