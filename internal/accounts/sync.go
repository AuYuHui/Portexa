package accounts

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/storage"
	"portexa/internal/vault"
	"portexa/mt4"
)

// Job is an internal capability for one immutable account configuration.
// It must never be serialized or logged: Config contains the investor password.
type Job struct {
	Account storage.Account
	Run     storage.SyncRun
	Config  mt4.Config
}

func (s *Service) Due(ctx context.Context, now int64, limit int) ([]string, error) {
	var ids []string
	err := s.Store.Read.WithContext(ctx).Model(&storage.Account{}).
		Where("sync_enabled = 1 AND removed_at_ms IS NULL AND next_sync_at_ms <= ?", now).
		Order("next_sync_at_ms, id").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

func (s *Service) RequestSync(ctx context.Context, id string) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if !account.SyncEnabled {
			return ErrPaused
		}
		if _, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID); err != nil {
			return err
		}
		return tx.Model(&account).Update("next_sync_at_ms", s.Now().UnixMilli()).Error
	})
}

func (s *Service) Recover(ctx context.Context) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := recoverHistory(tx, s.Now().UnixMilli(), "process_restart"); err != nil {
			return err
		}
		if err := tx.Model(&storage.SyncRun{}).Where("state IN ('running', 'queued')").Updates(map[string]any{
			"state": "interrupted", "finished_at_ms": s.Now().UnixMilli(), "completion_reason": "process_restart",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.DailyReport{}).Where("state = 'running'").Updates(map[string]any{
			"state": "interrupted", "finished_at_ms": s.Now().UnixMilli(), "completion_reason": "process_restart",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.ClockRun{}).Where("state = 'running'").Updates(map[string]any{
			"state": "interrupted", "finished_at_ms": s.Now().UnixMilli(), "completion_reason": "process_restart",
		}).Error; err != nil {
			return err
		}
		var servers []storage.Server
		if err := tx.Where("retired_at_ms IS NULL").Find(&servers).Error; err != nil {
			return err
		}
		for i := range servers {
			if err := refreshClockCalibration(tx, &servers[i], s.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) Claim(ctx context.Context, id string) (Job, error) {
	var job Job
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := tx.First(&job.Account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		account := job.Account
		if !account.SyncEnabled || account.NextSyncAtMS > s.Now().UnixMilli() {
			return ErrPaused
		}
		busy, err := accountFetching(tx, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrPaused
		}
		job.Config, err = s.fetchConfig(tx, account)
		if err != nil {
			return err
		}
		now := s.Now().UnixMilli()
		job.Run = storage.SyncRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID,
			EndpointID: account.PinnedEndpointID, CredentialVersion: account.CredentialVersion, ConfigVersion: account.ConfigVersion,
			Kind: "snapshot", State: "running", DueAtMS: account.NextSyncAtMS, NotBeforeMS: now, StartedAtMS: &now, Attempt: account.FailureCount + 1}
		return tx.Create(&job.Run).Error
	})
	return job, err
}

// DisableBrokenGate prevents a permanently invalid configuration from spinning.
func (s *Service) DisableBrokenGate(ctx context.Context, id string, version int64) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ? AND config_version = ?", id, version).Error; err != nil {
			return err
		}
		if !account.SyncEnabled {
			return nil
		}
		now := s.Now().UnixMilli()
		code := "credential_unavailable"
		if _, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID); err != nil {
			code = "endpoint_untrusted"
		}
		run := storage.SyncRun{ID: storage.NewID(), AccountID: id, ServerID: account.ServerID, EndpointID: account.PinnedEndpointID,
			CredentialVersion: account.CredentialVersion, ConfigVersion: account.ConfigVersion, Kind: "snapshot", State: "failed",
			DueAtMS: account.NextSyncAtMS, NotBeforeMS: now, StartedAtMS: &now, FinishedAtMS: &now, Attempt: 1, ErrorCode: &code}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		return tx.Model(&account).Update("sync_enabled", false).Error
	})
}

func (s *Service) Publish(ctx context.Context, job Job, snapshot storage.Snapshot, positions []storage.Position, next int64) error {
	observations, err := s.clockDisappearances(ctx, job, snapshot, positions)
	if err != nil {
		return err
	}
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var current storage.Account
		if err := tx.First(&current, "id = ?", job.Account.ID).Error; err != nil {
			return err
		}
		if !current.SyncEnabled || current.ConfigVersion != job.Account.ConfigVersion {
			return storage.ErrConflict
		}
		if _, err := s.validEndpoint(tx, job.Run.EndpointID, job.Run.ServerID); err != nil {
			return err
		}
		var run storage.SyncRun
		if err := tx.First(&run, "id = ? AND state = 'running'", job.Run.ID).Error; err != nil {
			return err
		}
		if err := tx.Create(&snapshot).Error; err != nil {
			return err
		}
		if len(observations) > 0 && current.CurrentSnapshotID != nil && job.Account.CurrentSnapshotID != nil && *current.CurrentSnapshotID == *job.Account.CurrentSnapshotID {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&observations, 100).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("account_id = ?", current.ID).Delete(&storage.Position{}).Error; err != nil {
			return err
		}
		if len(positions) > 0 {
			if err := tx.CreateInBatches(&positions, 100).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"current_snapshot_id": snapshot.ID, "next_sync_at_ms": next, "failure_count": 0}
		if current.IncludedAtMS == nil && snapshot.USDScale != nil {
			updates["included_at_ms"] = snapshot.ObservedToMS
			updates["initial_statistical_equity"] = snapshot.Equity.Sub(snapshot.Credit)
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(&run).Updates(map[string]any{"state": "succeeded", "finished_at_ms": snapshot.ObservedToMS, "completion_reason": "complete"}).Error
	})
}

type Failure struct {
	Code      string
	Operation string
	Status    *int
	Permanent bool
	Canceled  bool
}

func (s *Service) Fail(ctx context.Context, job Job, failure Failure, next int64) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var current storage.Account
		if err := tx.First(&current, "id = ?", job.Account.ID).Error; err != nil {
			return err
		}
		state := "failed"
		if failure.Canceled || current.ConfigVersion != job.Account.ConfigVersion {
			state = "canceled"
		}
		if err := tx.Model(&storage.SyncRun{}).Where("id = ? AND state = 'running'", job.Run.ID).Updates(map[string]any{
			"state": state, "finished_at_ms": s.Now().UnixMilli(), "error_code": failure.Code,
			"error_operation": failure.Operation, "raw_status": failure.Status,
		}).Error; err != nil {
			return err
		}
		if current.ConfigVersion != job.Account.ConfigVersion {
			return nil
		}
		updates := map[string]any{"next_sync_at_ms": next}
		if failure.Permanent {
			updates["sync_enabled"] = false
		}
		if state == "failed" {
			updates["failure_count"] = current.FailureCount + 1
		}
		return tx.Model(&current).Updates(updates).Error
	})
}

func IsBrokenGate(err error) bool {
	return errors.Is(err, vault.ErrUnavailable) || errors.Is(err, vault.ErrDecrypt) || errors.Is(err, ErrEndpoint) || errors.Is(err, ErrTarget)
}

func RetryDelay(failures int64) time.Duration {
	switch failures {
	case 0:
		return time.Minute
	case 1:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}
