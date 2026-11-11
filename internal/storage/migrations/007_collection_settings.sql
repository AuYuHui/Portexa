CREATE TABLE collection_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    snapshot_interval_seconds INTEGER NOT NULL DEFAULT 60
        CHECK (snapshot_interval_seconds IN (30, 60, 120, 300)),
    updated_at_ms INTEGER NOT NULL DEFAULT 0
) STRICT;

INSERT INTO collection_settings (id) VALUES (1);
