package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"gorm.io/gorm"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func snapshotFingerprint(snapshot storage.Snapshot, positions []storage.Position) string {
	items := slices.Clone(positions)
	slices.SortFunc(items, func(a, b storage.Position) int { return int(a.Ticket) - int(b.Ticket) })
	state := struct {
		Currency                                  string
		Revision                                  int64
		Balance, Credit, Equity, Floating, Margin string
		MarginAvailable                           bool
		Leverage                                  uint32
		Positions                                 []storage.Position
	}{Currency: snapshot.NativeCurrency, Revision: snapshot.UnitRevision, Balance: snapshot.Balance.String(), Credit: snapshot.Credit.String(),
		Equity: snapshot.Equity.String(), Floating: snapshot.FloatingNetProfit.String(), MarginAvailable: snapshot.MarginAvailable,
		Leverage: snapshot.Leverage, Positions: items}
	if snapshot.Margin != nil {
		state.Margin = snapshot.Margin.String()
	}
	raw, _ := json.Marshal(state)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// restSchedule verifies the template against consecutive, timely snapshots.
// A change after entry disables rest sampling for the rest of this window.
func restSchedule(tx *gorm.DB, account storage.Account, snapshot *storage.Snapshot, positions []storage.Position, settings storage.CollectionSettings) (time.Duration, int64, error) {
	interval := time.Duration(settings.SnapshotIntervalSeconds) * time.Second
	snapshot.StateFingerprint = snapshotFingerprint(*snapshot, positions)
	policy, err := readRestPolicy(tx, settings.MarketRestPolicyRevision)
	if err != nil {
		return interval, 0, err
	}
	at := time.UnixMilli(snapshot.ObservedToMS)
	possibleFrom, possibleTo := marketrest.LearnedWindow(at, 17*60, 2*1440+17*60)
	if at.Before(possibleFrom) || !at.Before(possibleTo) {
		return interval, 0, nil
	}
	if account.Currency == nil || account.UnitVerifiedAtMS == nil || *account.Currency != snapshot.NativeCurrency || account.UnitRevision != snapshot.UnitRevision || !restPolicyIncludes(policy, account.ID, snapshot.ObservedFromMS) || snapshot.ObservedFromMS > snapshot.ObservedToMS || snapshot.ObservedToMS-snapshot.ObservedFromMS > marketrest.Grace.Milliseconds() {
		return interval, 0, nil
	}
	if account.RestBlockedUntilMS > snapshot.ObservedToMS {
		return interval, 0, nil
	}
	for _, position := range positions {
		if !marketrest.SupportedSymbol(position.Symbol) {
			return interval, possibleTo.UnixMilli(), nil
		}
	}
	// Cash-only/new accounts provide no market evidence. Recent published
	// symbols can establish applicability; unknown/24h symbols opt out.
	var recent []string
	if err = currentHistory(tx, account.ID).Where("o.command IN (0,1) AND o.unit_revision=?", account.UnitRevision).
		Order("o.event_time_wall DESC,o.ticket DESC").Limit(100).Pluck("o.symbol", &recent).Error; err != nil {
		return interval, 0, err
	}
	if len(positions) == 0 && len(recent) == 0 {
		return interval, 0, nil
	}
	for _, symbol := range recent {
		if !marketrest.SupportedSymbol(symbol) {
			return interval, possibleTo.UnixMilli(), nil
		}
	}
	symbols := slices.Clone(recent)
	observable := map[string]bool{}
	for _, p := range positions {
		symbols = append(symbols, p.Symbol)
		if (p.Command == 0 || p.Command == 1) && p.VolumeHundredths > 0 && p.CurrentPrice.IsPositive() {
			observable[normalizeRestSymbol(p.Symbol)] = true
		}
	}
	window, closed, err := resolveRestWindow(tx, account, policy, symbols, observable, at)
	if err != nil {
		return interval, 0, err
	}
	if !closed {
		return interval, 0, nil
	}
	from, end := time.UnixMilli(window.FromMS), time.UnixMilli(window.UntilMS)
	snapshot.RestWindowID = window.ID
	snapshot.RestPolicyRevision = policy.Revision
	if account.CurrentSnapshotID == nil {
		return interval, 0, nil
	}
	var before storage.Snapshot
	if err = tx.First(&before, "id=? AND account_id=?", *account.CurrentSnapshotID, account.ID).Error; err != nil {
		return interval, 0, err
	}
	if before.StateFingerprint == "" || before.RestPolicyRevision != policy.Revision || before.RestWindowID != snapshot.RestWindowID {
		return interval, 0, nil
	}
	maxGap := marketrest.Grace.Milliseconds()
	if before.CollectionIntervalSeconds != nil {
		maxGap += int64(*before.CollectionIntervalSeconds) * 1000
	}
	changed := before.StateFingerprint != snapshot.StateFingerprint || account.FailureCount > 0 || snapshot.ObservedToMS-before.ObservedToMS > maxGap
	if changed {
		return interval, end.UnixMilli(), nil
	}
	if snapshot.ObservedToMS-before.ObservedToMS < 30_000 {
		return interval, 0, nil
	}
	snapshot.RestVerified = true
	if marketrest.ReducedWindow(at, from, end) {
		interval = max(interval, marketrest.Interval)
	} else {
		interval = min(interval, time.Minute)
	}
	return interval, 0, nil
}

func restNextSnapshot(tx *gorm.DB, id string, snapshot storage.Snapshot, interval time.Duration) (int64, error) {
	at := time.UnixMilli(snapshot.ObservedToMS)
	next := NextSnapshotAt(id, at, interval)
	from, to, closed, err := snapshotRestBounds(tx, snapshot, at)
	if err != nil {
		return 0, err
	}
	if snapshot.RestVerified && closed && marketrest.ReducedWindow(at, from, to) && interval >= marketrest.Interval {
		if guard := marketrest.NextWindowGuard(at, to); guard.After(at) && guard.Before(next) {
			next = guard
		}
	}
	return next.UnixMilli(), nil
}

func restBackgroundRefresh(tx *gorm.DB, account storage.Account, at time.Time) (time.Duration, error) {
	stable, err := restCurrentlyStable(tx, account, at)
	if err != nil {
		return 0, err
	}
	if stable {
		return marketrest.HistoryInterval, nil
	}
	return 0, nil
}

func restHistoryFreshness(tx *gorm.DB, account storage.Account, at time.Time) (time.Duration, error) {
	stable, err := restCurrentlyStable(tx, account, at)
	if err != nil {
		return 0, err
	}
	if stable {
		return marketrest.HistoryInterval + marketrest.Grace, nil
	}
	return 10 * time.Minute, nil
}

func restSnapshotTemplateActive(tx *gorm.DB, account storage.Account, snapshot storage.Snapshot, settings storage.CollectionSettings, at time.Time) (bool, error) {
	_, _, closed, err := snapshotRestBounds(tx, snapshot, at)
	if err != nil {
		return false, err
	}
	if !(closed && account.SyncEnabled && account.FailureCount == 0 && account.RestBlockedUntilMS <= at.UnixMilli() && snapshot.RestVerified && snapshot.RestPolicyRevision == settings.MarketRestPolicyRevision && settings.MarketRestTemplate == marketrest.Template && !slices.Contains(settings.MarketRestExcludedAccountIDs, account.ID) && account.Currency != nil && account.UnitVerifiedAtMS != nil && snapshot.UnitRevision == account.UnitRevision && snapshot.NativeCurrency == *account.Currency) {
		return false, nil
	}
	// A conflict discovered by another account takes effect immediately.
	if snapshot.RestWindowID != "" {
		var w storage.MarketRestWindow
		if err = tx.First(&w, "id=?", snapshot.RestWindowID).Error; err != nil {
			return false, err
		}
		var ids []string
		if err = json.Unmarshal([]byte(w.ModelIDsJSON), &ids); err != nil {
			return false, err
		}
		for _, id := range ids {
			var event storage.MarketLearningEvent
			if err = tx.Where("model_id=?", id).Order("id DESC").First(&event).Error; err != nil {
				return false, err
			}
			var model storage.MarketLearningModel
			if err = tx.First(&model, "id=?", id).Error; err != nil {
				return false, err
			}
			if event.State != "adopted" || model.ExpiresAtMS <= at.UnixMilli() {
				return false, nil
			}
		}
	}
	return true, nil
}
func restCurrentlyStable(tx *gorm.DB, account storage.Account, at time.Time) (bool, error) {
	if !account.SyncEnabled || account.FailureCount > 0 || account.CurrentSnapshotID == nil || account.RestBlockedUntilMS > at.UnixMilli() {
		return false, nil
	}
	settings, err := readCollectionSettings(tx)
	if err != nil {
		return false, err
	}
	var latest storage.Snapshot
	if err = tx.First(&latest, "id=? AND account_id=?", *account.CurrentSnapshotID, account.ID).Error; err != nil {
		return false, err
	}
	active, err := restSnapshotTemplateActive(tx, account, latest, settings, at)
	if err != nil {
		return false, err
	}
	from, to, _, err := snapshotRestBounds(tx, latest, at)
	if err != nil {
		return false, err
	}
	age := at.UnixMilli() - latest.ObservedToMS
	return active && marketrest.ReducedWindow(at, from, to) && age >= 0 && age <= (marketrest.Interval+marketrest.Grace).Milliseconds(), nil
}
