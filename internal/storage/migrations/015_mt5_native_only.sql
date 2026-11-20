-- Require a new verified endpoint before sending credentials over native TCP.
UPDATE accounts SET sync_enabled = 0, config_version = config_version + 1
WHERE removed_at_ms IS NULL AND pinned_endpoint_id IN (
    SELECT id FROM server_endpoints WHERE web_hostname <> ''
);

UPDATE server_endpoints SET state = 'retired'
WHERE web_hostname <> '' AND state <> 'retired';

DROP TRIGGER endpoint_web_hostname_is_immutable;
ALTER TABLE server_endpoints DROP COLUMN web_hostname;
