-- Clear values inferred by the previous MT5 reader and reschedule coverage.
-- Keep the source runs, deal revisions and classification records for audit.
UPDATE account_snapshots SET leverage=0, margin=NULL, margin_available=0
WHERE account_id IN (SELECT a.id FROM accounts a JOIN servers s ON s.id=a.server_id WHERE s.protocol='mt5');

UPDATE history_coverage
SET state=CASE WHEN state='split' THEN state ELSE 'pending' END,
    checked_at_ms=NULL, next_attempt_at_ms=0, failure_count=0
WHERE account_id IN (SELECT a.id FROM accounts a JOIN servers s ON s.id=a.server_id WHERE s.protocol='mt5');

UPDATE accounts
SET next_daily_sync_at_ms=0, next_history_sync_at_ms=0
WHERE removed_at_ms IS NULL AND server_id IN (SELECT id FROM servers WHERE protocol='mt5');
