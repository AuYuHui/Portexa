package accounts

import "gorm.io/gorm"

// Older versions used the single-frame limit for an entire deal reply.
// Resume only the jobs stopped by that limit. Setup failures and windows
// blocked by the newer reply reader retain their existing state.
func resumeMT5HistoryAfterReplyLimit(tx *gorm.DB) error {
	err := tx.Exec(`UPDATE accounts AS a
		SET daily_sync_paused=0, daily_failure_count=0, next_daily_sync_at_ms=0
		WHERE a.sync_enabled=1 AND a.daily_sync_paused=1 AND a.removed_at_ms IS NULL
		AND EXISTS (
			SELECT 1 FROM servers s WHERE s.id=a.server_id AND s.protocol='mt5' AND s.retired_at_ms IS NULL
		)
		AND 1=(
			SELECT CASE WHEN state='failed' AND error_code='resource_limit' AND completion_reason='deals' THEN 1 ELSE 0 END
			FROM daily_reports d WHERE d.account_id=a.id AND d.config_version=a.config_version
			ORDER BY started_at_ms DESC,id DESC LIMIT 1
		)`).Error
	if err != nil {
		return err
	}
	return tx.Exec(`UPDATE history_coverage AS c
		SET state='pending', next_attempt_at_ms=0
		WHERE c.state='blocked'
		AND EXISTS (
			SELECT 1 FROM accounts a JOIN servers s ON s.id=a.server_id
			WHERE a.id=c.account_id AND a.sync_enabled=1 AND a.history_sync_paused=0 AND a.removed_at_ms IS NULL
			AND a.unit_revision=c.unit_revision AND s.active_time_ruleset_id=c.time_ruleset_id
			AND s.protocol='mt5' AND s.retired_at_ms IS NULL
		)
		AND 1=(
			SELECT CASE WHEN state='failed' AND error_code='resource_limit' AND completion_reason='history' THEN 1 ELSE 0 END
			FROM history_runs r WHERE r.account_id=c.account_id AND r.coverage_id=c.id
			ORDER BY started_at_ms DESC,id DESC LIMIT 1
		)`).Error
}
