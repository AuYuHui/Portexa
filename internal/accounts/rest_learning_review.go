package accounts

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

// ReviewRestLearning is bounded, local-only work; collection never waits for
// training. Immutable evidence is loaded before acquiring the writer queue.
func (s *Service) ReviewRestLearning(ctx context.Context) error {
	now := s.Now()
	settings, err := s.CollectionSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.MarketRestLearningEnabled || settings.MarketRestTemplate != marketrest.Template {
		return nil
	}
	var groups []storage.MarketLearningGroup
	if err = s.Store.Read.WithContext(ctx).Where("epoch_ms=? AND next_review_ms<=?", settings.MarketRestLearningEpochMS, now.UnixMilli()).Order("next_review_ms,id").Limit(4).Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		if err = s.reviewRestGroup(ctx, group, now); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) reviewRestGroup(ctx context.Context, group storage.MarketLearningGroup, now time.Time) error {
	var stored []storage.MarketActivityObservation
	err := s.Store.Read.WithContext(ctx).Where("group_id=? AND observed_to_ms>=?", group.ID, now.AddDate(0, 0, -57).UnixMilli()).Order("observed_to_ms,id").Limit(200001).Find(&stored).Error
	if err != nil {
		return err
	}
	rows := make([]marketrest.Observation, 0, len(stored))
	for _, r := range stored {
		rows = append(rows, marketrest.Observation{ID: r.ID, Stream: marketrest.StreamKey(r.AccountID, r.ConfigVersion, r.UnitRevision), Snapshot: r.SnapshotID, Previous: r.PreviousSnapshotID, From: r.ObservedFromMS, To: r.ObservedToMS, PreviousTo: r.PreviousToMS, MaxGap: r.MaxGapMS, Healthy: r.Healthy, Comparable: r.Comparable, Changed: r.QuoteChanged})
	}
	weeks := []marketrest.Weekend{}
	if len(stored) <= 200000 {
		weeks = marketrest.CompleteWeekends(rows, now)
	}
	state, reason := "learning", "collecting"
	if len(stored) == 0 || !stored[len(stored)-1].Comparable {
		reason = "no_quote"
	}
	if len(stored) > 200000 {
		weeks = nil
		reason = "evidence_limit"
	}
	// A partial silent stretch is a candidate observation, never an adopted model.
	observationStart, observationEnd := marketrest.LearningBounds(now)
	if len(stored) > 0 && reason == "collecting" && !now.Before(observationStart) && !now.After(observationEnd) {
		end := stored[len(stored)-1]
		start := end.PreviousToMS
		for i := len(stored) - 1; i >= 0; i-- {
			r := stored[i]
			if r.AccountID != end.AccountID || r.ConfigVersion != end.ConfigVersion || r.UnitRevision != end.UnitRevision {
				continue
			}
			if !r.Healthy || !r.Comparable || r.QuoteChanged || r.ObservedToMS < start {
				break
			}
			start = r.PreviousToMS
		}
		if end.ObservedToMS-start >= 30*time.Minute.Milliseconds() {
			state, reason = "candidate", "silent_candidate"
		}
	}
	var model *storage.MarketLearningModel
	validation := ""
	passed := false
	if len(weeks) >= 3 {
		train := weeks
		if len(train) > 4 {
			train = train[len(train)-4:]
		}
		learned, ok := marketrest.Train(train)
		if ok {
			keys, _ := json.Marshal(learned.Keys)
			m := storage.MarketLearningModel{ID: restDigest([]string{group.ID, marketrest.LearningVersion, learned.Digest}), GroupID: group.ID, Algorithm: marketrest.LearningVersion, CreatedAtMS: now.UnixMilli(), TrainedThroughMS: learned.Through, ExpiresAtMS: learned.Expires, CloseMinute: learned.Close, OpenMinute: learned.Open, CloseMinMinute: learned.CloseMin, CloseMaxMinute: learned.CloseMax, OpenMinMinute: learned.OpenMin, OpenMaxMinute: learned.OpenMax, TrainingKeysJSON: string(keys), EvidenceSHA256: learned.Digest}
			model = &m
			state, reason = "candidate", "awaiting_holdout"
			if len(train) >= 4 {
				validation = train[3].Key
				passed = marketrest.Validate(learned, train[3], rows)
				if !passed {
					state, reason = "suspended", "validation_failed"
				}
			}
		} else {
			state, reason = "suspended", "boundary_drift"
		}
	}
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		var current storage.MarketLearningGroup
		if err := tx.First(&current, "id=?", group.ID).Error; err != nil {
			return err
		}
		// Quote conflicts are recorded atomically by the collector; never overwrite
		// a new observation with a review based on an older read snapshot.
		if current.LastObservedMS != group.LastObservedMS {
			return nil
		}
		updates := map[string]any{"reviewed_at_ms": now.UnixMilli(), "next_review_ms": now.Add(time.Hour).UnixMilli(), "valid_weekends": len(weeks), "state": state, "reason": reason}
		if model != nil {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model).Error; err != nil {
				return err
			}
			var events []storage.MarketLearningEvent
			if err := tx.Where("model_id=?", model.ID).Order("id DESC").Limit(1).Find(&events).Error; err != nil {
				return err
			}
			if len(events) == 0 {
				event := storage.MarketLearningEvent{ModelID: model.ID, State: "candidate", EffectiveAtMS: now.UnixMilli(), Reason: "awaiting_holdout"}
				if err := tx.Create(&event).Error; err != nil {
					return err
				}
				events = []storage.MarketLearningEvent{event}
			}
			updates["candidate_model_id"] = model.ID
			latest := events[0]
			if latest.State == "candidate" && validation != "" {
				event := storage.MarketLearningEvent{ModelID: model.ID, State: "rejected", EffectiveAtMS: now.UnixMilli(), Reason: "validation_failed", ValidationKey: validation}
				if passed && model.ExpiresAtMS > now.UnixMilli() {
					event.State, event.Reason = "adopted", "validated"
				}
				if err := tx.Create(&event).Error; err != nil {
					return err
				}
				latest = event
			}
			if latest.State == "adopted" && model.ExpiresAtMS > now.UnixMilli() {
				updates["active_model_id"] = model.ID
				updates["state"], updates["reason"] = "adopted", "validated"
			} else if latest.State == "suspended" || latest.State == "rejected" {
				updates["state"], updates["reason"] = "suspended", latest.Reason
			}
		}

		if current.ActiveModelID != "" {
			var active storage.MarketLearningModel
			if err := tx.First(&active, "id=?", current.ActiveModelID).Error; err != nil {
				return err
			}
			var last storage.MarketLearningEvent
			if err := tx.Where("model_id=?", active.ID).Order("id DESC").First(&last).Error; err != nil {
				return err
			}
			why := ""
			if active.ExpiresAtMS <= now.UnixMilli() {
				why = "stale"
			}
			if reason == "boundary_drift" || reason == "validation_failed" {
				why = reason
			}
			if last.State == "adopted" && why != "" {
				last = storage.MarketLearningEvent{ModelID: active.ID, State: "suspended", EffectiveAtMS: now.UnixMilli(), Reason: why}
				if err := tx.Create(&last).Error; err != nil {
					return err
				}
			}
			if _, adopted := updates["active_model_id"]; !adopted {
				if last.State == "adopted" {
					updates["state"], updates["reason"] = "adopted", "validated"
				} else {
					updates["state"], updates["reason"] = "suspended", last.Reason
				}
			}
		}
		return tx.Model(&current).Updates(updates).Error
	})
}
