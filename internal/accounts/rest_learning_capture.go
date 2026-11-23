package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func restDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func learningGroupID(account storage.Account, symbol string, epoch int64) string {
	return restDigest([]any{account.ServerID, collectionEndpointID(account), symbol, epoch})
}
func normalizeRestSymbol(symbol string) string { return strings.ToUpper(strings.TrimSpace(symbol)) }
func quotePositions(items []storage.Position) map[string][]storage.Position {
	out := map[string][]storage.Position{}
	for _, p := range items {
		symbol := strings.ToUpper(strings.TrimSpace(p.Symbol))
		if marketrest.SupportedSymbol(symbol) && (p.Command == 0 || p.Command == 1) && p.VolumeHundredths > 0 {
			out[symbol] = append(out[symbol], p)
		}
	}
	for key := range out {
		sort.Slice(out[key], func(i, j int) bool { return out[key][i].Ticket < out[key][j].Ticket })
	}
	return out
}
func positionShape(items []storage.Position) string {
	shape := []string{}
	for _, p := range items {
		shape = append(shape, fmt.Sprintf("%d:%d:%d", p.Ticket, p.Command, p.VolumeHundredths))
	}
	return restDigest(shape)
}

// Capture precedes replacement of current positions in the publication tx.
// Balance/fees and position edits cannot masquerade as observed quote ticks.
func captureRestActivity(tx *gorm.DB, account storage.Account, snapshot storage.Snapshot, positions []storage.Position, settings storage.CollectionSettings) error {
	if !settings.MarketRestLearningEnabled || settings.MarketRestTemplate != marketrest.Template {
		return nil
	}
	policy, err := readRestPolicy(tx, settings.MarketRestPolicyRevision)
	if err != nil {
		return err
	}
	if !restPolicyIncludes(policy, account.ID, snapshot.ObservedFromMS) {
		return nil
	}
	var previous storage.Snapshot
	var old []storage.Position
	if account.CurrentSnapshotID != nil {
		if err = tx.First(&previous, "id=? AND account_id=?", *account.CurrentSnapshotID, account.ID).Error; err != nil {
			return err
		}
		if err = tx.Where("account_id=? AND snapshot_id=?", account.ID, previous.ID).Find(&old).Error; err != nil {
			return err
		}
	}
	before, after := quotePositions(old), quotePositions(positions)
	for key := range before {
		if _, ok := after[key]; !ok {
			after[key] = nil
		}
	}
	for symbol, current := range after {
		id := learningGroupID(account, symbol, policy.LearningEpochMS)
		group := storage.MarketLearningGroup{ID: id, ServerID: account.ServerID, EndpointID: collectionEndpointID(account), Symbol: symbol, EpochMS: policy.LearningEpochMS, LastObservedMS: snapshot.ObservedToMS, State: "learning", Reason: "collecting"}
		if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"last_observed_ms": snapshot.ObservedToMS})}).Create(&group).Error; err != nil {
			return err
		}
		gap := marketrest.Grace.Milliseconds()
		if previous.CollectionIntervalSeconds != nil {
			gap += int64(*previous.CollectionIntervalSeconds) * 1000
		}
		row := storage.MarketActivityObservation{GroupID: id, AccountID: account.ID, SnapshotID: snapshot.ID, PreviousSnapshotID: previous.ID, SourceRunID: snapshot.SyncRunID, ConfigVersion: account.ConfigVersion, UnitRevision: account.UnitRevision, ObservedFromMS: snapshot.ObservedFromMS, ObservedToMS: snapshot.ObservedToMS, PreviousToMS: previous.ObservedToMS, MaxGapMS: gap,
			PositionChanged: positionShape(before[symbol]) != positionShape(current), AccountChanged: previous.ID != "" && (!previous.Balance.Equal(snapshot.Balance) || !previous.Credit.Equal(snapshot.Credit) || !previous.Equity.Equal(snapshot.Equity) || !previous.FloatingNetProfit.Equal(snapshot.FloatingNetProfit))}
		var previousRun storage.SyncRun
		if previous.ID != "" {
			if err = tx.First(&previousRun, "id=?", previous.SyncRunID).Error; err != nil {
				return err
			}
		}
		reliable := previous.ID != "" && previousRun.State == "succeeded" && previousRun.EndpointID == collectionEndpointID(account) && previousRun.ServerID == account.ServerID && previousRun.ConfigVersion == account.ConfigVersion && previous.UnitRevision == snapshot.UnitRevision && previous.NativeCurrency == snapshot.NativeCurrency && previous.ObservedToMS >= policy.LearningEpochMS && snapshot.ObservedFromMS >= previous.ObservedToMS && snapshot.ObservedFromMS <= snapshot.ObservedToMS && snapshot.ObservedToMS-snapshot.ObservedFromMS <= marketrest.Grace.Milliseconds()
		row.Healthy = reliable && account.FailureCount == 0 && snapshot.ObservedToMS-previous.ObservedToMS <= gap
		for _, p := range current {
			for _, b := range before[symbol] {
				if p.Ticket == b.Ticket && p.Command == b.Command && p.VolumeHundredths == b.VolumeHundredths && p.CurrentPrice.IsPositive() && b.CurrentPrice.IsPositive() {
					row.Comparable = true
					row.QuoteChanged = row.QuoteChanged || !p.CurrentPrice.Equal(b.CurrentPrice)
					if row.QuoteAnchor == "" {
						row.QuoteAnchor = restDigest([]any{p.Ticket, p.Command, p.VolumeHundredths})
						row.QuotePrice = p.CurrentPrice.String()
					}
				}
			}
		}
		row.Comparable = row.Comparable && reliable
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		if row.Comparable && row.QuoteChanged {
			if err = suspendContradictedModel(tx, id, row); err != nil {
				return err
			}
		}
	}
	return nil
}

func suspendContradictedModel(tx *gorm.DB, id string, row storage.MarketActivityObservation) error {
	var group storage.MarketLearningGroup
	if err := tx.First(&group, "id=?", id).Error; err != nil {
		return err
	}
	if group.ActiveModelID == "" {
		return nil
	}
	var m storage.MarketLearningModel
	if err := tx.First(&m, "id=?", group.ActiveModelID).Error; err != nil {
		return err
	}
	var event storage.MarketLearningEvent
	if err := tx.Where("model_id=?", m.ID).Order("id DESC").First(&event).Error; err != nil {
		return err
	}
	if event.State != "adopted" || m.ExpiresAtMS <= row.ObservedToMS || event.EffectiveAtMS > row.ObservedToMS {
		return nil
	}
	from, to := marketrest.LearnedWindow(time.UnixMilli(row.ObservedToMS), m.CloseMinute, m.OpenMinute)
	if row.PreviousToMS >= to.UnixMilli() || row.ObservedToMS <= from.UnixMilli() {
		return nil
	}
	event = storage.MarketLearningEvent{ModelID: m.ID, State: "suspended", EffectiveAtMS: row.ObservedToMS, BlockedUntilMS: to.UnixMilli(), Reason: "quote_conflict", SourceRunID: &row.SourceRunID}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	return tx.Model(&group).Updates(map[string]any{"state": "suspended", "reason": "quote_conflict"}).Error
}
