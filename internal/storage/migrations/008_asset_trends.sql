ALTER TABLE account_snapshots ADD COLUMN collection_interval_seconds INTEGER
    CHECK (collection_interval_seconds IN (30, 60, 120, 300));

-- The next UTC minute is the first portfolio point eligible to use a sample.
-- This index selects the last actual sample per minute without reading every
-- 30-second row or casting any monetary TEXT to a binary floating-point value.
CREATE INDEX snapshots_account_minute
    ON account_snapshots(account_id, (observed_to_ms + 59999) / 60000, observed_to_ms DESC, id DESC);

-- Unit changes have an effective time. A prior confirmed snapshot must not be
-- carried across a later change while waiting for the first new snapshot.
CREATE TABLE account_unit_changes (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    unit_revision INTEGER NOT NULL CHECK (unit_revision > 0),
    effective_at_ms INTEGER NOT NULL,
    PRIMARY KEY (account_id, unit_revision)
) STRICT;
CREATE INDEX account_unit_changes_time
    ON account_unit_changes(account_id, effective_at_ms, unit_revision);

-- Earlier revisions retain their snapshot evidence; unknown historical change
-- times are not fabricated. The current revision is known from this baseline.
INSERT INTO account_unit_changes (account_id, unit_revision, effective_at_ms)
    SELECT id, unit_revision,
        CASE WHEN unit_revision = 1 THEN created_at_ms
             ELSE COALESCE(unit_verified_at_ms, CAST(unixepoch('subsec') * 1000 AS INTEGER)) END
    FROM accounts;
