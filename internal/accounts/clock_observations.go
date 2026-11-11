package accounts

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/storage"
)

const ClockRequiredEvents = 3
const clockEvidenceTTL = 24 * time.Hour
const clockPendingTTL = 15 * time.Minute
const clockWindowLimit = 3 * time.Minute
const clockBatchLimit = 128

// Read outside the high-priority writer. The pointer/configuration are checked
// again at publication, so a concurrent settings change cannot seed evidence.
func (s *Service) clockDisappearances(ctx context.Context, job Job, snapshot storage.Snapshot, positions []storage.Position) ([]storage.ClockObservation, error) {
	if job.Account.CurrentSnapshotID == nil {
		return nil, nil
	}
	db := s.Store.Read.WithContext(ctx)
	var previous storage.Snapshot
	if err := db.First(&previous, "id = ? AND account_id = ?", *job.Account.CurrentSnapshotID, job.Account.ID).Error; err != nil {
		return nil, err
	}
	// Account read can happen anywhere in a fetch. Use its start for the lower
	// bound, not its finish; a trade may close while the fetch is still running.
	if previous.ObservedToMS > snapshot.ObservedFromMS || previous.ObservedFromMS > snapshot.ObservedToMS ||
		snapshot.ObservedToMS-previous.ObservedFromMS > clockWindowLimit.Milliseconds() {
		return nil, nil
	}
	var run storage.SyncRun
	if err := db.First(&run, "id = ?", previous.SyncRunID).Error; err != nil {
		return nil, err
	}
	if run.ConfigVersion != job.Account.ConfigVersion || run.EndpointID != job.Run.EndpointID || run.State != "succeeded" {
		return nil, nil
	}
	var old []storage.Position
	if err := db.Select("ticket", "command", "symbol", "open_time_wall").
		Where("account_id = ? AND snapshot_id = ? AND command IN (0, 1)", job.Account.ID, previous.ID).Order("ticket").Find(&old).Error; err != nil {
		return nil, err
	}
	present := make(map[int32]bool, len(positions))
	for _, position := range positions {
		present[position.Ticket] = true
	}
	observations := make([]storage.ClockObservation, 0)
	for _, position := range old {
		if present[position.Ticket] || position.OpenTimeWall == 0 || position.Symbol == "" {
			continue
		}
		observations = append(observations, storage.ClockObservation{ID: storage.NewID(),
			AccountID: job.Account.ID, ServerID: job.Account.ServerID, EndpointID: job.Run.EndpointID,
			ConfigVersion: job.Account.ConfigVersion, BeforeSnapshotID: previous.ID, AfterSnapshotID: snapshot.ID,
			Ticket: position.Ticket, Command: position.Command, Symbol: position.Symbol, OpenTimeWall: position.OpenTimeWall,
			WindowFromMS: previous.ObservedFromMS, WindowToMS: snapshot.ObservedToMS, State: "pending"})
		if len(observations) == clockBatchLimit {
			break
		}
	}
	return observations, nil
}

// A unique quarter-hour UTC offset must place the raw close time inside the
// independently observed interval. Two seconds tolerate timestamp resolution;
// a long interval, an old order or an ambiguous offset supplies no evidence.
func inferClockOffset(observation storage.ClockObservation, closeWall uint32) (int, bool) {
	if closeWall == 0 || observation.WindowFromMS > observation.WindowToMS ||
		observation.WindowToMS-observation.WindowFromMS > clockWindowLimit.Milliseconds() {
		return 0, false
	}
	matched, offset := 0, 0
	for minutes := -12 * 60; minutes <= 14*60; minutes += 15 {
		instant := int64(closeWall)*1000 - int64(minutes)*60000
		if instant >= observation.WindowFromMS-2000 && instant <= observation.WindowToMS+2000 {
			matched++
			offset = minutes
		}
	}
	return offset, matched == 1
}

func refreshClockCalibration(tx *gorm.DB, server *storage.Server, now time.Time) error {
	var observations []storage.ClockObservation
	if err := tx.Table("trade_clock_observations AS o").Select("o.*").
		Joins("JOIN accounts AS a ON a.id = o.account_id AND a.server_id = o.server_id AND a.config_version = o.config_version AND a.pinned_endpoint_id = o.endpoint_id").
		Joins("JOIN server_endpoints AS e ON e.id = o.endpoint_id AND e.state = 'reviewed'").
		Where("o.server_id = ? AND o.state = 'accepted' AND a.removed_at_ms IS NULL AND o.window_to_ms > ? AND o.window_to_ms <= ?",
			server.ID, now.Add(-clockEvidenceTTL).UnixMilli(), now.UnixMilli()).
		Order("o.window_to_ms DESC, o.id DESC").Limit(2048).Find(&observations).Error; err != nil {
		return err
	}
	state := storage.ClockCalibration{ServerID: server.ID, Status: "learning"}
	tickets, windows := map[int32]bool{}, map[string]bool{}
	for _, observation := range observations {
		// Published DST policy expires earlier observations when its offset
		// changes. A measured current offset is not an annual DST rule.
		if !sameOfficialClockPeriod(*server, time.UnixMilli(observation.WindowFromMS), time.UnixMilli(observation.WindowToMS)) ||
			!sameOfficialClockPeriod(*server, time.UnixMilli(observation.WindowToMS), now) {
			continue
		}
		if state.OffsetMinutes == nil {
			offset := *observation.OffsetMinutes
			state.OffsetMinutes = &offset
			stamp := observation.WindowToMS
			expires := stamp + clockEvidenceTTL.Milliseconds()
			state.ObservedAtMS, state.ExpiresAtMS = &stamp, &expires
		} else if *state.OffsetMinutes != *observation.OffsetMinutes {
			break // Only the most recent consistent sequence counts.
		}
		tickets[observation.Ticket] = true
		windows[observation.AccountID+":"+observation.AfterSnapshotID] = true
	}
	state.EvidenceCount, state.EventWindowCount = len(tickets), len(windows)
	if state.EvidenceCount >= ClockRequiredEvents && state.EventWindowCount >= 2 {
		state.Status = "calibrated"
	} else if state.OffsetMinutes != nil {
		var previous storage.ClockCalibration
		err := tx.First(&previous, "server_id = ?", server.ID).Error
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		if previous.OffsetMinutes != nil && (previous.Status == "conflict" || (previous.Status == "calibrated" && *previous.OffsetMinutes != *state.OffsetMinutes)) {
			state.Status = "conflict"
		}
		if entry, available := automaticTimeRule(*server, now); available {
			zone, _, err := serverLocation(entry, now)
			if err != nil {
				return err
			}
			_, seconds := now.In(zone).Zone()
			if seconds/60 != *state.OffsetMinutes {
				state.Status = "conflict"
			}
		}
	}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&state).Error; err != nil {
		return err
	}
	return ensureAutomaticTime(tx, server, now)
}

func sameOfficialClockPeriod(server storage.Server, observed, now time.Time) bool {
	entry, available := automaticTimeRule(server, now)
	if !available {
		return true
	}
	before, _, err := serverLocation(entry, observed)
	if err != nil {
		return false
	}
	after, _, err := serverLocation(entry, now)
	if err != nil {
		return false
	}
	_, a := observed.In(before).Zone()
	_, b := now.In(after).Zone()
	return a == b
}

// RefreshServerTimes also expires measurements while there are no trades.
func (s *Service) RefreshServerTimes(ctx context.Context) error {
	var servers []storage.Server
	if err := s.Store.Read.WithContext(ctx).Table("servers AS s").Select("s.*").
		Joins("JOIN server_clock_calibrations AS c ON c.server_id = s.id").
		Where("s.retired_at_ms IS NULL AND c.observed_at_ms IS NOT NULL").Find(&servers).Error; err != nil {
		return err
	}
	for _, server := range servers {
		if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			var current storage.Server
			if err := tx.First(&current, "id = ?", server.ID).Error; err != nil {
				return err
			}
			return refreshClockCalibration(tx, &current, s.Now())
		}); err != nil {
			return err
		}
	}
	return nil
}
