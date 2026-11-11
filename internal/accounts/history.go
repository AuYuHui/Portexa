package accounts

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/storage"
	"portexa/mt4"
)

const HistoryPublishLimit = 5000

// HistoryJob contains a credential capability and must not be logged/serialized.
type HistoryJob struct {
	Account  storage.Account
	Coverage storage.HistoryCoverage
	Run      storage.HistoryRun
	Config   mt4.Config
	Day      ServerDay
}

func (s *Service) HistoryDue(ctx context.Context, now int64, limit int) ([]string, error) {
	var ids []string
	err := s.Store.Read.WithContext(ctx).Table("accounts AS a").
		Joins("JOIN servers AS s ON s.id=a.server_id AND s.retired_at_ms IS NULL").
		Where("a.sync_enabled=1 AND a.history_sync_paused=0 AND a.removed_at_ms IS NULL AND a.current_snapshot_id IS NOT NULL").
		Where("s.active_time_ruleset_id IS NOT NULL AND a.next_history_sync_at_ms<=? AND a.next_sync_at_ms>=?", now, now+DailyAdmissionLead.Milliseconds()).
		Order("a.next_history_sync_at_ms,a.id").Limit(limit).Pluck("a.id", &ids).Error
	return ids, err
}

func historyScope(tx *gorm.DB, account storage.Account, day ServerDay) *gorm.DB {
	return tx.Model(&storage.HistoryCoverage{}).Where("account_id=? AND unit_revision=? AND time_ruleset_id=?", account.ID, account.UnitRevision, day.TimeRulesetID)
}

func prepareHistory(tx *gorm.DB, account storage.Account, day ServerDay) error {
	var count int64
	if err := historyScope(tx, account, day).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 && day.Start > 0 {
		backfill := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision,
			TimeRulesetID: day.TimeRulesetID, StartWall: 0, EndWall: day.Start + 1, State: "pending"}
		if err := tx.Create(&backfill).Error; err != nil {
			return err
		}
	}
	if count > 0 {
		var end uint32
		if err := historyScope(tx, account, day).Select("COALESCE(MAX(end_wall),0)").Scan(&end).Error; err != nil {
			return err
		}
		if end > 0 && end < day.Start {
			gap := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision,
				TimeRulesetID: day.TimeRulesetID, StartWall: end - 1, EndWall: day.Start + 1, State: "pending"}
			if err := tx.Create(&gap).Error; err != nil {
				return err
			}
		}
	}
	// One fixed recent window per server day; no unbounded moving windows.
	recent := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision,
		TimeRulesetID: day.TimeRulesetID, StartWall: day.Start, EndWall: day.End, State: "pending"}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&recent).Error
}

func (s *Service) RequestHistory(ctx context.Context, id string) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if !account.SyncEnabled {
			return ErrPaused
		}
		if _, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID); err != nil {
			return err
		}
		var server storage.Server
		if err := tx.First(&server, "id=?", account.ServerID).Error; err != nil {
			return err
		}
		day, err := serverDay(tx, server, s.Now())
		if err != nil {
			return err
		}
		if err := prepareHistory(tx, account, day); err != nil {
			return err
		}
		if err := historyScope(tx, account, day).Where("state!='split' AND state!='running'").Updates(map[string]any{
			"state": "pending", "next_attempt_at_ms": 0, "failure_count": 0,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&account).Updates(map[string]any{"next_history_sync_at_ms": 0, "history_sync_paused": false}).Error
	})
}

func (s *Service) ClaimHistory(ctx context.Context, id string) (HistoryJob, error) {
	var job HistoryJob
	claimed := false
	err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		if err := tx.First(&job.Account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		a, now := job.Account, s.Now()
		if !a.SyncEnabled || a.HistorySyncPaused || a.CurrentSnapshotID == nil || a.NextHistorySyncAtMS > now.UnixMilli() || a.NextSyncAtMS < now.Add(DailyAdmissionLead).UnixMilli() {
			return ErrPaused
		}
		busy, err := accountFetching(tx, id)
		if err != nil || busy {
			if err != nil {
				return err
			}
			return ErrPaused
		}
		var server storage.Server
		if err = tx.First(&server, "id=? AND retired_at_ms IS NULL", a.ServerID).Error; err != nil {
			return err
		}
		job.Day, err = serverDay(tx, server, now)
		if err != nil {
			return err
		}
		job.Config, err = s.fetchConfig(tx, a)
		if err != nil {
			return err
		}
		var snapshot storage.Snapshot
		if err = tx.First(&snapshot, "id=? AND account_id=?", *a.CurrentSnapshotID, id).Error; err != nil {
			return err
		}
		if snapshot.UnitRevision != a.UnitRevision {
			return ErrPaused
		}
		if err = prepareHistory(tx, a, job.Day); err != nil {
			return err
		}
		found := historyScope(tx, a, job.Day).Where("state NOT IN ('split','blocked','running') AND next_attempt_at_ms<=?", now.UnixMilli()).
			Order(clause.OrderBy{Expression: clause.Expr{SQL: "CASE WHEN start_wall>=? THEN 0 WHEN checked_at_ms IS NULL THEN 1 ELSE 2 END, next_attempt_at_ms,start_wall,id", Vars: []any{job.Day.Start}}}).
			Limit(1).Find(&job.Coverage)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			next := now.Add(time.Minute)
			if job.Day.NextDayUTC.Before(next) {
				next = job.Day.NextDayUTC
			}
			return tx.Model(&a).Update("next_history_sync_at_ms", next.UnixMilli()).Error
		}
		job.Run = storage.HistoryRun{ID: storage.NewID(), AccountID: id, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID,
			CoverageID: job.Coverage.ID, ConfigVersion: a.ConfigVersion, UnitRevision: a.UnitRevision,
			NativeCurrency: snapshot.NativeCurrency, State: "running", StartedAtMS: now.UnixMilli()}
		if err = tx.Create(&job.Run).Error; err != nil {
			return err
		}
		claimed = true
		return tx.Model(&job.Coverage).Update("state", "running").Error
	})
	if err == nil && !claimed {
		err = ErrPaused
	}
	return job, err
}

func historyConfiguration(tx *gorm.DB, job HistoryJob) (storage.Account, bool, error) {
	var current storage.Account
	if err := tx.First(&current, "id=?", job.Account.ID).Error; err != nil {
		return current, false, err
	}
	var server storage.Server
	if err := tx.First(&server, "id=?", current.ServerID).Error; err != nil {
		return current, false, err
	}
	valid := current.ConfigVersion == job.Account.ConfigVersion && current.UnitRevision == job.Account.UnitRevision &&
		server.ActiveTimeRulesetID != nil && *server.ActiveTimeRulesetID == job.Day.TimeRulesetID
	return current, valid, nil
}

func recoverHistory(tx *gorm.DB, now int64, reason string) error {
	if err := tx.Model(&storage.HistoryRun{}).Where("state='running'").Updates(map[string]any{
		"state": "interrupted", "finished_at_ms": gorm.Expr("MAX(?,started_at_ms)", now), "completion_reason": reason,
	}).Error; err != nil {
		return err
	}
	if err := storage.DiscardUnpublishedHistory(tx); err != nil {
		return err
	}
	return tx.Model(&storage.HistoryCoverage{}).Where("state='running'").Updates(map[string]any{"state": "pending", "next_attempt_at_ms": 0}).Error
}

// History failures are independent of the live snapshot's retry budget.
func (s *Service) FailHistory(ctx context.Context, job HistoryJob, failure Failure, split bool) error {
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		current, valid, err := historyConfiguration(tx, job)
		if err != nil {
			return err
		}
		state := "failed"
		if failure.Canceled || !valid {
			state, split = "canceled", false
		}
		finished := tx.Model(&storage.HistoryRun{}).Where("id=? AND state='running'", job.Run.ID).Updates(map[string]any{
			"state": state, "finished_at_ms": gorm.Expr("MAX(?,started_at_ms)", s.Now().UnixMilli()), "error_code": failure.Code,
			"raw_status": failure.Status, "completion_reason": failure.Operation,
		})
		if finished.Error != nil {
			return finished.Error
		}
		if finished.RowsAffected == 0 {
			return nil
		}
		if err := storage.DiscardUnpublishedHistory(tx); err != nil {
			return err
		}
		next := s.Now().Add(RetryDelay(job.Coverage.FailureCount)).UnixMilli()
		updates := map[string]any{"state": "failed", "next_attempt_at_ms": next, "failure_count": job.Coverage.FailureCount + 1}
		if state == "canceled" {
			next = s.Now().Add(5 * time.Second).UnixMilli()
			updates = map[string]any{"state": "pending", "next_attempt_at_ms": next}
		}
		if split {
			if job.Coverage.EndWall-job.Coverage.StartWall <= 2 {
				updates["state"] = "blocked"
			} else {
				mid := job.Coverage.StartWall + (job.Coverage.EndWall-job.Coverage.StartWall)/2
				for _, span := range [][2]uint32{{job.Coverage.StartWall, mid + 1}, {mid, job.Coverage.EndWall}} {
					child := storage.HistoryCoverage{ID: storage.NewID(), AccountID: job.Account.ID, UnitRevision: job.Account.UnitRevision,
						TimeRulesetID: job.Day.TimeRulesetID, ParentID: &job.Coverage.ID, StartWall: span[0], EndWall: span[1], State: "pending"}
					if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&child).Error; err != nil {
						return err
					}
				}
				updates["state"] = "split"
			}
			next = s.Now().Add(time.Second).UnixMilli()
		}
		if err = tx.Model(&job.Coverage).Updates(updates).Error; err != nil {
			return err
		}
		if !valid {
			return nil
		}
		return tx.Model(&current).Updates(map[string]any{"next_history_sync_at_ms": next, "history_sync_paused": failure.Permanent && !split}).Error
	})
}
