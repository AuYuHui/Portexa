package accounts

import (
	"context"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/mt4"
)

// ClockJob contains an investor password; never serialize or log it.
type ClockJob struct {
	Account      storage.Account
	Run          storage.ClockRun
	Config       mt4.Config
	Observations []storage.ClockObservation
}

// ClosedClockTrade is the minimal history data needed for calibration.
type ClosedClockTrade struct {
	Ticket, Command     int32
	Symbol              string
	OpenTime, CloseTime uint32
}

func pendingClock(tx *gorm.DB, account storage.Account, now int64) *gorm.DB {
	return tx.Where("account_id = ? AND config_version = ? AND endpoint_id = ? AND state = 'pending' AND window_to_ms > ? AND window_to_ms <= ?",
		account.ID, account.ConfigVersion, account.PinnedEndpointID, now-clockPendingTTL.Milliseconds(), now)
}

func (s *Service) ClockDue(ctx context.Context, now int64, limit int) ([]string, error) {
	var ids []string
	err := s.Store.Read.WithContext(ctx).Table("accounts AS a").
		Where("a.sync_enabled = 1 AND a.clock_sync_paused = 0 AND a.removed_at_ms IS NULL AND a.next_clock_sync_at_ms <= ? AND a.next_sync_at_ms >= ?",
			now, now+DailyAdmissionLead.Milliseconds()).
		Where(`EXISTS (SELECT 1 FROM trade_clock_observations AS o WHERE o.account_id = a.id
			AND o.config_version = a.config_version AND o.endpoint_id = a.pinned_endpoint_id
			AND o.state = 'pending' AND o.window_to_ms > ? AND o.window_to_ms <= ?)`, now-clockPendingTTL.Milliseconds(), now).
		Order("a.next_clock_sync_at_ms, a.id").Limit(limit).Pluck("a.id", &ids).Error
	return ids, err
}

func (s *Service) ClaimClock(ctx context.Context, id string) (ClockJob, error) {
	var job ClockJob
	err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		if err := tx.First(&job.Account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		a, now := job.Account, s.Now()
		if !a.SyncEnabled || a.ClockSyncPaused || a.NextClockSyncAtMS > now.UnixMilli() || a.NextSyncAtMS < now.Add(DailyAdmissionLead).UnixMilli() {
			return ErrPaused
		}
		busy, err := accountFetching(tx, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrPaused
		}
		if err := pendingClock(tx, a, now.UnixMilli()).Order("window_to_ms, id").Limit(clockBatchLimit).Find(&job.Observations).Error; err != nil {
			return err
		}
		if len(job.Observations) == 0 {
			return ErrPaused
		}
		from, to := job.Observations[0].WindowFromMS, job.Observations[0].WindowToMS
		for _, observation := range job.Observations {
			from, to = min(from, observation.WindowFromMS), max(to, observation.WindowToMS)
		}
		// Raw wall seconds must cover every supported UTC offset. This small
		// event-driven bootstrap does not assume a broker, local zone or day.
		start, end := from/1000-12*3600-2, (to+999)/1000+14*3600+3
		if start < 0 || end > 2147483647 || start >= end {
			return ErrServerTime
		}
		job.Config, err = s.fetchConfig(tx, a)
		if err != nil {
			return err
		}
		job.Run = storage.ClockRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID,
			EndpointID: a.PinnedEndpointID, ConfigVersion: a.ConfigVersion, WindowStart: uint32(start), WindowEnd: uint32(end),
			State: "running", StartedAtMS: now.UnixMilli()}
		return tx.Create(&job.Run).Error
	})
	return job, err
}

func (s *Service) PublishClock(ctx context.Context, job ClockJob, trades []ClosedClockTrade, finished int64) error {
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		var current storage.Account
		if err := tx.First(&current, "id = ?", job.Account.ID).Error; err != nil {
			return err
		}
		if !current.SyncEnabled || current.RemovedAtMS != nil || current.ConfigVersion != job.Account.ConfigVersion ||
			current.PinnedEndpointID != job.Run.EndpointID || finished < job.Run.StartedAtMS {
			return storage.ErrConflict
		}
		if _, err := s.validEndpoint(tx, job.Run.EndpointID, job.Run.ServerID); err != nil {
			return err
		}
		var run storage.ClockRun
		if err := tx.First(&run, "id = ? AND state = 'running'", job.Run.ID).Error; err != nil {
			return err
		}
		byTicket := make(map[int32]ClosedClockTrade, len(trades))
		for _, trade := range trades {
			byTicket[trade.Ticket] = trade
		}
		for _, observation := range job.Observations {
			trade, found := byTicket[observation.Ticket]
			updates := map[string]any{"checked_at_ms": finished}
			if found {
				offset, unique := inferClockOffset(observation, trade.CloseTime)
				updates["state"] = "rejected"
				if unique && observation.Command == trade.Command && observation.Symbol == trade.Symbol && observation.OpenTimeWall == trade.OpenTime &&
					finished >= observation.WindowToMS && finished-observation.WindowToMS < clockPendingTTL.Milliseconds() {
					updates["state"], updates["close_time_wall"], updates["offset_minutes"] = "accepted", trade.CloseTime, offset
				}
			} // History propagation can lag: an absent record stays pending.
			if err := tx.Model(&storage.ClockObservation{}).Where("id = ? AND state = 'pending' AND config_version = ?", observation.ID, current.ConfigVersion).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&run).Updates(map[string]any{"state": "succeeded", "finished_at_ms": finished, "completion_reason": "complete"}).Error; err != nil {
			return err
		}
		if err := tx.Model(&current).Updates(map[string]any{"next_clock_sync_at_ms": finished + int64((30*time.Second)/time.Millisecond), "clock_failure_count": 0, "clock_sync_paused": false}).Error; err != nil {
			return err
		}
		var server storage.Server
		if err := tx.First(&server, "id = ?", current.ServerID).Error; err != nil {
			return err
		}
		return refreshClockCalibration(tx, &server, s.Now())
	})
}

func (s *Service) FailClock(ctx context.Context, job ClockJob, failure Failure) error {
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		var current storage.Account
		if err := tx.First(&current, "id = ?", job.Account.ID).Error; err != nil {
			return err
		}
		valid := current.ConfigVersion == job.Account.ConfigVersion && current.PinnedEndpointID == job.Run.EndpointID
		state := "failed"
		if failure.Canceled || !valid {
			state = "canceled"
		}
		if err := tx.Model(&storage.ClockRun{}).Where("id = ? AND state = 'running'", job.Run.ID).Updates(map[string]any{
			"state": state, "finished_at_ms": s.Now().UnixMilli(), "error_code": failure.Code, "completion_reason": failure.Operation,
		}).Error; err != nil || !valid {
			return err
		}
		updates := map[string]any{"next_clock_sync_at_ms": s.Now().Add(5 * time.Second).UnixMilli()}
		if state == "failed" {
			updates["next_clock_sync_at_ms"] = s.Now().Add(RetryDelay(current.ClockFailureCount)).UnixMilli()
			updates["clock_failure_count"] = current.ClockFailureCount + 1
			updates["clock_sync_paused"] = failure.Permanent
		}
		return tx.Model(&current).Updates(updates).Error
	})
}
