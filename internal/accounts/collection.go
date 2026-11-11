package accounts

import (
	"context"
	"hash/fnv"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

type CollectionInput struct {
	SnapshotIntervalSeconds int `json:"snapshot_interval_seconds"`
}

func readCollectionSettings(tx *gorm.DB) (storage.CollectionSettings, error) {
	var settings storage.CollectionSettings
	err := tx.First(&settings, "id = 1").Error
	return settings, err
}

func (s *Service) CollectionSettings(ctx context.Context) (storage.CollectionSettings, error) {
	return readCollectionSettings(s.Store.Read.WithContext(ctx))
}

func (s *Service) UpdateCollectionSettings(ctx context.Context, input CollectionInput) (storage.CollectionSettings, error) {
	switch input.SnapshotIntervalSeconds {
	case 30, 60, 120, 300:
	default:
		return storage.CollectionSettings{}, &InputError{"采集周期仅支持 30、60、120 或 300 秒"}
	}
	var settings storage.CollectionSettings
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var err error
		settings, err = readCollectionSettings(tx)
		if err != nil || settings.SnapshotIntervalSeconds == input.SnapshotIntervalSeconds {
			return err
		}
		now := s.Now()
		settings.SnapshotIntervalSeconds, settings.UpdatedAtMS = input.SnapshotIntervalSeconds, now.UnixMilli()
		if err := tx.Save(&settings).Error; err != nil {
			return err
		}
		// Preserve due/manual work, first snapshots, pauses and network backoff.
		// Running snapshots read this setting again in their publication transaction.
		var list []storage.Account
		if err := tx.Where("sync_enabled = 1 AND removed_at_ms IS NULL AND failure_count = 0 AND current_snapshot_id IS NOT NULL AND next_sync_at_ms > ?", now.UnixMilli()).Find(&list).Error; err != nil {
			return err
		}
		interval := time.Duration(settings.SnapshotIntervalSeconds) * time.Second
		for _, account := range list {
			if err := tx.Model(&account).Update("next_sync_at_ms", NextSnapshotAt(account.ID, now, interval).UnixMilli()).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return settings, err
}

// NextSnapshotAt spreads accounts across the configured cycle. A complete
// history request needs 20 seconds plus its 5-second snapshot guard; two extra
// seconds let the scheduler observe completion and admit it on the next scan.
// Slow snapshots may therefore shift their next due time beyond the phase.
func NextSnapshotAt(id string, finished time.Time, interval time.Duration) time.Time {
	if interval < time.Millisecond {
		interval = time.Minute
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	cycle := interval.Milliseconds()
	phase := int64(h.Sum32()) % cycle
	next := ((finished.UnixMilli()-phase)/cycle+1)*cycle + phase
	earliest := finished.Add(DailyAdmissionLead + 2*time.Second).UnixMilli()
	return time.UnixMilli(max(next, earliest))
}

func snapshotStaleAfter(settings storage.CollectionSettings) time.Duration {
	return max(3*time.Minute, 3*time.Duration(settings.SnapshotIntervalSeconds)*time.Second)
}
