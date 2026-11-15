package accounts

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func resolveRestWindow(tx *gorm.DB, account storage.Account, policy storage.MarketRestPolicy, symbols []string, observable map[string]bool, at time.Time) (storage.MarketRestWindow, bool, error) {
	templateFrom, templateTo, _ := marketrest.Window(at)
	from, to := templateFrom, templateTo
	ids := []string{}
	effective := policy.EffectiveAtMS
	seen := map[string]bool{}
	for _, symbol := range symbols {
		symbol = normalizeRestSymbol(symbol)
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		a, b := templateFrom, templateTo
		if policy.LearningEnabled && observable[symbol] {
			var group storage.MarketLearningGroup
			err := tx.First(&group, "id=?", learningGroupID(account, symbol, policy.LearningEpochMS)).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return storage.MarketRestWindow{}, false, err
			}
			if err == nil && group.ActiveModelID != "" {
				var model storage.MarketLearningModel
				if err = tx.First(&model, "id=?", group.ActiveModelID).Error; err != nil {
					return storage.MarketRestWindow{}, false, err
				}
				var event storage.MarketLearningEvent
				if err = tx.Where("model_id=?", model.ID).Order("id DESC").First(&event).Error; err != nil {
					return storage.MarketRestWindow{}, false, err
				}
				if event.BlockedUntilMS > at.UnixMilli() {
					return storage.MarketRestWindow{}, false, nil
				}
				if event.State == "adopted" && event.EffectiveAtMS <= at.UnixMilli() && model.ExpiresAtMS > at.UnixMilli() {
					a, b = marketrest.LearnedWindow(at, model.CloseMinute, model.OpenMinute)
					ids = append(ids, model.ID)
					effective = max(effective, event.EffectiveAtMS)
				}
			}
		}
		if len(seen) == 1 {
			from, to = a, b
		} else {
			if a.After(from) {
				from = a
			}
			if b.Before(to) {
				to = b
			}
		}
	}
	if at.Before(from) || !at.Before(to) || !from.Before(to) {
		return storage.MarketRestWindow{}, false, nil
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(ids)
	kind := "template"
	if len(ids) > 0 {
		kind = "learned"
	}
	window := storage.MarketRestWindow{AccountID: account.ID, PolicyRevision: policy.Revision, Kind: kind, FromMS: from.UnixMilli(), UntilMS: to.UnixMilli(), EffectiveAtMS: effective, ModelIDsJSON: string(raw)}
	window.ID = restDigest([]any{window.AccountID, window.PolicyRevision, window.Kind, window.FromMS, window.UntilMS, window.EffectiveAtMS, ids})
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&window).Error; err != nil {
		return window, false, err
	}
	return window, true, nil
}
func snapshotRestBounds(tx *gorm.DB, snapshot storage.Snapshot, at time.Time) (time.Time, time.Time, bool, error) {
	if snapshot.RestWindowID == "" {
		a, b, closed := marketrest.Window(at)
		return a, b, closed, nil
	}
	var w storage.MarketRestWindow
	if err := tx.First(&w, "id=? AND account_id=? AND policy_revision=?", snapshot.RestWindowID, snapshot.AccountID, snapshot.RestPolicyRevision).Error; err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	return time.UnixMilli(w.FromMS), time.UnixMilli(w.UntilMS), at.UnixMilli() >= max(w.FromMS, w.EffectiveAtMS) && at.UnixMilli() < w.UntilMS, nil
}

// A missing/unreadable current window shortens refresh instead of delaying it.
func restRefreshGuard(tx *gorm.DB, account storage.Account, at time.Time) int64 {
	if account.CurrentSnapshotID == nil {
		return at.Add(time.Minute).UnixMilli()
	}
	var latest storage.Snapshot
	if tx.First(&latest, "id=?", *account.CurrentSnapshotID).Error != nil {
		return at.Add(time.Minute).UnixMilli()
	}
	_, to, closed, err := snapshotRestBounds(tx, latest, at)
	if err != nil || !closed {
		return at.Add(time.Minute).UnixMilli()
	}
	return marketrest.NextWindowGuard(at, to).UnixMilli()
}
