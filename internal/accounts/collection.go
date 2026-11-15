package accounts

import (
	"context"
	"hash/fnv"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

type CollectionInput struct {
	MarketRestLearningEnabled    *bool     `json:"market_rest_learning_enabled"`
	SnapshotIntervalSeconds      int       `json:"snapshot_interval_seconds"`
	MarketRestTemplate           *string   `json:"market_rest_template"`
	MarketRestExcludedAccountIDs *[]string `json:"market_rest_excluded_account_ids"`
}

func readCollectionSettings(tx *gorm.DB) (storage.CollectionSettings, error) {
	var settings storage.CollectionSettings
	if err := tx.First(&settings, "id = 1").Error; err != nil {
		return settings, err
	}
	policy, err := readRestPolicy(tx, settings.MarketRestPolicyRevision)
	if err != nil {
		return settings, err
	}
	settings.MarketRestTemplate = policy.Template
	settings.MarketRestLearningEnabled = policy.LearningEnabled
	settings.MarketRestLearningEpochMS = policy.LearningEpochMS
	settings.MarketRestExcludedAccountIDs, err = restExclusions(policy)
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
		if err != nil {
			return err
		}
		now := s.Now()
		restChanged, err := updateRestPolicy(tx, &settings, input, now.UnixMilli())
		if err != nil {
			return err
		}
		if !restChanged && settings.SnapshotIntervalSeconds == input.SnapshotIntervalSeconds {
			return nil
		}
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
			next := NextSnapshotAt(account.ID, now, interval).UnixMilli()
			if restChanged {
				next = min(account.NextSyncAtMS, now.Add(DailyAdmissionLead+2*time.Second).UnixMilli())
			}
			updates := map[string]any{"next_sync_at_ms": next}
			if restChanged {
				if account.DailyFailureCount == 0 && !account.DailySyncPaused {
					updates["next_daily_sync_at_ms"] = min(account.NextDailySyncAtMS, next)
				}
				if !account.HistorySyncPaused {
					updates["next_history_sync_at_ms"] = min(account.NextHistorySyncAtMS, next)
				}
			}
			if err := tx.Model(&account).Updates(updates).Error; err != nil {
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
