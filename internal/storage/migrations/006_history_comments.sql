ALTER TABLE history_order_revisions ADD COLUMN comment TEXT NOT NULL DEFAULT '';
ALTER TABLE history_order_revisions ADD COLUMN automatic_category TEXT NOT NULL DEFAULT '';
ALTER TABLE history_order_revisions ADD COLUMN automatic_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE history_order_revisions ADD COLUMN automatic_rule_version TEXT NOT NULL DEFAULT '';
CREATE INDEX history_order_run ON history_order_revisions(source_run_id);
