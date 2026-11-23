ALTER TABLE accounts ADD COLUMN backup_endpoint_ids TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(backup_endpoint_ids) AND json_type(backup_endpoint_ids) = 'array'
           AND json_array_length(backup_endpoint_ids) <= 2);
ALTER TABLE accounts ADD COLUMN last_collection_endpoint_id TEXT NOT NULL DEFAULT '';

-- Clock provenance can change when a successful snapshot uses a backup.
CREATE TRIGGER analytics_clock_collection_endpoint AFTER UPDATE OF last_collection_endpoint_id ON accounts
WHEN COALESCE(NULLIF(NEW.last_collection_endpoint_id, ''), NEW.pinned_endpoint_id)
  IS NOT COALESCE(NULLIF(OLD.last_collection_endpoint_id, ''), OLD.pinned_endpoint_id)
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
