package accounts

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/mt4"
)

const DailyAdmissionLead = 25 * time.Second
const DailySnapshotGuard = 5 * time.Second

// DailyJob contains an investor password and must never be serialized or logged.
type DailyJob struct {
	Account        storage.Account
	Run            storage.DailyReport
	Config         mt4.Config
	Day            ServerDay
	NativeCurrency string
}

type DailyView struct {
	Date      string               `json:"date"`
	TimeZone  string               `json:"time_zone"`
	State     string               `json:"state"`
	ErrorCode *string              `json:"error_code"`
	RawStatus *int                 `json:"raw_status"`
	Stale     bool                 `json:"stale"`
	Report    *storage.DailyReport `json:"report"`
}

func (s *Service) DailyDue(ctx context.Context, now int64, limit int) ([]string, error) {
	var ids []string
	err := s.Store.Read.WithContext(ctx).Table("accounts AS a").
		Joins("JOIN servers AS s ON s.id = a.server_id AND s.retired_at_ms IS NULL").
		Where("a.sync_enabled = 1 AND a.daily_sync_paused = 0 AND a.removed_at_ms IS NULL AND a.current_snapshot_id IS NOT NULL").
		Where("s.active_time_ruleset_id IS NOT NULL AND a.next_daily_sync_at_ms <= ? AND a.next_sync_at_ms >= ?", now, now+DailyAdmissionLead.Milliseconds()).
		Order("a.next_daily_sync_at_ms, a.id").Limit(limit).Pluck("a.id", &ids).Error
	return ids, err
}

func (s *Service) RequestDaily(ctx context.Context, id string) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if !account.SyncEnabled {
			return ErrPaused
		}
		var server storage.Server
		if err := tx.First(&server, "id = ?", account.ServerID).Error; err != nil {
			return err
		}
		if _, err := serverDay(tx, server, s.Now()); err != nil {
			return err
		}
		if _, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID); err != nil {
			return err
		}
		return tx.Model(&account).Updates(map[string]any{"next_daily_sync_at_ms": 0, "daily_sync_paused": false, "daily_failure_count": 0}).Error
	})
}

func accountFetching(tx *gorm.DB, id string) (bool, error) {
	var count int64
	if err := tx.Model(&storage.SyncRun{}).Where("account_id = ? AND state = 'running'", id).Count(&count).Error; err != nil || count > 0 {
		return count > 0, err
	}
	if err := tx.Model(&storage.DailyReport{}).Where("account_id = ? AND state = 'running'", id).Count(&count).Error; err != nil || count > 0 {
		return count > 0, err
	}
	err := tx.Model(&storage.ClockRun{}).Where("account_id = ? AND state = 'running'", id).Count(&count).Error
	if err != nil || count > 0 {
		return count > 0, err
	}
	err = tx.Model(&storage.HistoryRun{}).Where("account_id = ? AND state = 'running'", id).Count(&count).Error
	return count > 0, err
}

func (s *Service) ClaimDaily(ctx context.Context, id string) (DailyJob, error) {
	var job DailyJob
	err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		if err := tx.First(&job.Account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		a := job.Account
		now := s.Now()
		if !a.SyncEnabled || a.DailySyncPaused || a.CurrentSnapshotID == nil || a.NextDailySyncAtMS > now.UnixMilli() || a.NextSyncAtMS < now.Add(DailyAdmissionLead).UnixMilli() {
			return ErrPaused
		}
		busy, err := accountFetching(tx, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrPaused
		}
		var server storage.Server
		if err = tx.First(&server, "id = ? AND retired_at_ms IS NULL", a.ServerID).Error; err != nil {
			return err
		}
		job.Day, err = serverDay(tx, server, now)
		if err != nil {
			return err
		}
		var snapshot storage.Snapshot
		if err = tx.First(&snapshot, "id = ? AND account_id = ?", *a.CurrentSnapshotID, a.ID).Error; err != nil {
			return err
		}
		job.NativeCurrency = snapshot.NativeCurrency
		job.Config, err = s.fetchConfig(tx, a)
		if err != nil {
			return err
		}
		job.Run = storage.DailyReport{ID: storage.NewID(), AccountID: id, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID,
			ConfigVersion: a.ConfigVersion, UnitRevision: a.UnitRevision, TimeRulesetID: job.Day.TimeRulesetID,
			ReportDate: job.Day.Date, TimeZone: job.Day.TimeZone, WindowStart: job.Day.Start, WindowEnd: job.Day.End,
			State: "running", StartedAtMS: now.UnixMilli()}
		return tx.Create(&job.Run).Error
	})
	return job, err
}

func dailyConfiguration(tx *gorm.DB, job DailyJob) (storage.Account, bool, error) {
	var current storage.Account
	if err := tx.First(&current, "id = ?", job.Account.ID).Error; err != nil {
		return current, false, err
	}
	var server storage.Server
	if err := tx.First(&server, "id = ?", current.ServerID).Error; err != nil {
		return current, false, err
	}
	valid := current.ConfigVersion == job.Account.ConfigVersion && current.UnitRevision == job.Account.UnitRevision &&
		server.ActiveTimeRulesetID != nil && *server.ActiveTimeRulesetID == job.Day.TimeRulesetID
	return current, valid, nil
}

func (s *Service) PublishDaily(ctx context.Context, job DailyJob, report storage.DailyReport, next int64) error {
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		current, valid, err := dailyConfiguration(tx, job)
		if err != nil {
			return err
		}
		if !valid || !current.SyncEnabled || report.ID != job.Run.ID {
			return storage.ErrConflict
		}
		if _, err = s.validEndpoint(tx, job.Run.EndpointID, job.Run.ServerID); err != nil {
			return err
		}
		result := tx.Model(&storage.DailyReport{}).Where("id = ? AND state = 'running'", job.Run.ID).Updates(map[string]any{
			"state": "succeeded", "finished_at_ms": report.FinishedAtMS, "native_currency": report.NativeCurrency,
			"net_profit": report.NetProfit, "balance_adjustment": report.BalanceAdjustment, "balance_change": report.BalanceChange,
			"credit_change": report.CreditChange, "closed_trades": report.ClosedTrades, "completion_reason": "complete",
			"winning_trades": report.WinningTrades, "losing_trades": report.LosingTrades, "breakeven_trades": report.BreakevenTrades,
			"gross_profit": report.GrossProfit, "gross_loss": report.GrossLoss,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return storage.ErrConflict
		}
		return tx.Model(&current).Updates(map[string]any{"next_daily_sync_at_ms": next, "daily_failure_count": 0, "daily_sync_paused": false}).Error
	})
}

func (s *Service) FailDaily(ctx context.Context, job DailyJob, failure Failure) error {
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		current, valid, err := dailyConfiguration(tx, job)
		if err != nil {
			return err
		}
		state := "failed"
		if failure.Canceled || !valid {
			state = "canceled"
		}
		if err = tx.Model(&storage.DailyReport{}).Where("id = ? AND state = 'running'", job.Run.ID).Updates(map[string]any{
			"state": state, "finished_at_ms": s.Now().UnixMilli(), "error_code": failure.Code, "completion_reason": failure.Operation,
			"raw_status": failure.Status,
		}).Error; err != nil || !valid {
			return err
		}
		next := s.Now().Add(5 * time.Second).UnixMilli()
		updates := map[string]any{"next_daily_sync_at_ms": next}
		if state == "failed" {
			updates["next_daily_sync_at_ms"] = s.Now().Add(RetryDelay(current.DailyFailureCount)).UnixMilli()
			updates["daily_failure_count"] = current.DailyFailureCount + 1
			updates["daily_sync_paused"] = failure.Permanent
		}
		return tx.Model(&current).Updates(updates).Error
	})
}

func readDailyView(tx *gorm.DB, account storage.Account, server storage.Server, now time.Time) (DailyView, error) {
	view := DailyView{State: "idle"}
	day, err := serverDay(tx, server, now)
	if errors.Is(err, ErrServerTime) {
		view.State = "time_zone_unconfirmed"
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Date, view.TimeZone = day.Date, day.TimeZone
	query := func() *gorm.DB {
		return tx.Where("account_id = ? AND report_date = ? AND time_ruleset_id = ? AND unit_revision = ?",
			account.ID, day.Date, day.TimeRulesetID, account.UnitRevision)
	}
	var latest storage.DailyReport
	if err = query().Where("config_version = ?", account.ConfigVersion).Order("started_at_ms DESC, id DESC").First(&latest).Error; err != nil && !errors.Is(err, storage.ErrNotFound) {
		return view, err
	}
	if err == nil {
		view.State, view.ErrorCode, view.RawStatus = latest.State, latest.ErrorCode, latest.RawStatus
	}
	var report storage.DailyReport
	reportQuery := query().Where("state = 'succeeded'")
	if account.CurrentSnapshotID != nil {
		reportQuery = reportQuery.Where("native_currency = (SELECT native_currency FROM account_snapshots WHERE id = ?)", *account.CurrentSnapshotID)
	}
	if err = reportQuery.Order("started_at_ms DESC, id DESC").First(&report).Error; err != nil && !errors.Is(err, storage.ErrNotFound) {
		return view, err
	}
	if err == nil {
		view.Report = &report
		view.Stale = report.FinishedAtMS == nil || now.UnixMilli()-*report.FinishedAtMS > (10*time.Minute).Milliseconds()
	}
	if !account.SyncEnabled || account.DailySyncPaused {
		view.State = "paused"
	}
	return view, nil
}
