package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestRestLearningAdoptionSchedulingFinancialProofAndConflict(t *testing.T) {
	f, a, now, start := restFixture(t)
	*now = start
	seed := restSnapshot(t, f, a, start, "XAUUSD.c", 10)
	var group storage.MarketLearningGroup
	if err := f.Store.Read.First(&group).Error; err != nil {
		t.Fatal(err)
	}
	rows, reviewAt := learningRows(t)
	// Synthetic evidence exercises persistence and the full review/adoption path.
	var sourceRun storage.SyncRun
	if err := f.Store.Read.First(&sourceRun, "id=?", seed.SyncRunID).Error; err != nil {
		t.Fatal(err)
	}
	runs := []storage.SyncRun{}
	snaps := []storage.Snapshot{}
	observations := []storage.MarketActivityObservation{}
	for _, r := range rows {
		id := fmt.Sprintf("%032d", 100000+r.ID)
		previous := ""
		if r.Previous != "" {
			previous = fmt.Sprintf("%032d", 100000+r.ID-1)
		}
		run := sourceRun
		run.ID = storage.NewID()
		runs = append(runs, run)
		snap := seed
		snap.SyncRunID = run.ID
		snap.ID = id
		snap.RestWindowID = ""
		snap.RestVerified = false
		snap.RestPolicyRevision = 0
		snap.ObservedFromMS = r.From
		snap.ObservedToMS = r.To
		snaps = append(snaps, snap)
		observations = append(observations, storage.MarketActivityObservation{GroupID: group.ID, AccountID: a.ID, SnapshotID: id, PreviousSnapshotID: previous, SourceRunID: run.ID, ConfigVersion: a.ConfigVersion, UnitRevision: a.UnitRevision, ObservedFromMS: r.From, ObservedToMS: r.To, PreviousToMS: r.PreviousTo, MaxGapMS: r.MaxGap, Healthy: r.Healthy, Comparable: r.Comparable, QuoteChanged: r.Changed, QuotePrice: "1", QuoteAnchor: "synthetic"})
	}

	for offset := 0; offset < len(runs); offset += 100 {
		end := min(offset+100, len(runs))
		if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
			if err := tx.Create(runs[offset:end]).Error; err != nil {
				return err
			}
			if err := tx.Create(snaps[offset:end]).Error; err != nil {
				return err
			}
			return tx.Create(observations[offset:end]).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		return tx.Model(&group).Update("last_observed_ms", rows[len(rows)-1].To).Error
	}); err != nil {
		t.Fatal(err)
	}
	*now = reviewAt.AddDate(0, 0, -7)
	if err := f.Accounts.ReviewRestLearning(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&group, "id=?", group.ID).Error; err != nil {
		t.Fatal(err)
	}
	if group.State != "candidate" || group.ValidWeekends != 3 || group.ActiveModelID != "" {
		t.Fatal("adopted without independent fourth weekend", group.State, group.ValidWeekends)
	}
	*now = reviewAt
	if err := f.Accounts.ReviewRestLearning(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&group, "id=?", group.ID).Error; err != nil {
		t.Fatal(err)
	}
	if group.State != "adopted" || group.ValidWeekends != 4 || group.ActiveModelID == "" {
		t.Fatal("no adoption", group.State, group.Reason, group.ValidWeekends)
	}
	var model storage.MarketLearningModel
	if err := f.Store.Read.First(&model, "id=?", group.ActiveModelID).Error; err != nil {
		t.Fatal(err)
	}
	// This is outside the built-in Sunday 16:00 boundary and inside the learned
	// window. A new pair must establish actual stability before lowering cadence.
	ny, _ := time.LoadLocation("America/New_York")
	at := time.Date(2027, 3, 14, 16, 15, 0, 0, ny)
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		return tx.Model(&a).Updates(map[string]any{"current_snapshot_id": nil, "rest_blocked_until_ms": 0}).Error
	}); err != nil {
		t.Fatal(err)
	}
	var stable storage.Snapshot
	for _, offset := range []time.Duration{0, time.Minute, 6 * time.Minute} {
		*now = at.Add(offset)
		stable = restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	}
	if !stable.RestVerified || stable.RestWindowID == "" || *stable.CollectionIntervalSeconds != 300 {
		t.Fatal("learned window not scheduled", stable.RestVerified, stable.CollectionIntervalSeconds)
	}
	var window storage.MarketRestWindow
	if err := f.Store.Read.First(&window, "id=?", stable.RestWindowID).Error; err != nil || window.Kind != "learned" {
		t.Fatal("no immutable learned window", err)
	}
	view, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil || !view.Account.MarketRest {
		t.Fatal("view ignores learned extension", err)
	}
	// Prediction by itself does not supply ledger/time coverage.
	input := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: at.Add(time.Minute).UnixMilli(), ToMS: now.UnixMilli()}
	report, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Result.Metrics.NetProfit.Value != nil {
		t.Fatal("prediction invented profit")
	}
	*now = now.Add(time.Second)
	analyticsHistory(t, f, a, *now, nil)
	report, err = f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, report.Result.Metrics.NetProfit, "0")
	if report.Result.Quality.RestCarriedPoints == 0 {
		t.Fatal("learned bounds not used in valuation")
	}
	// A second symbol without a learned model restricts the shared account to
	// the template intersection, even if its first symbol has a wider model.
	var server storage.Server
	var endpoint storage.Endpoint
	f.Store.Read.First(&server, "id=?", a.ServerID)
	f.Store.Read.First(&endpoint, "id=?", a.PinnedEndpointID)
	b := f.Account(t, server, endpoint, 9879, "USD")
	*now = now.Add(30 * time.Second)
	if err = f.Accounts.RequestSync(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	mixedJob, err := f.Accounts.Claim(t.Context(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	mixedResult := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: "USD", Balance: 1000, Equity: 1020}, Trades: []mt4.Trade{{Ticket: 88301, Command: 0, Symbol: "XAUUSD.c", VolumeHundredths: 10, Profit: 10, OpenPrice: 1, ClosePrice: 1}, {Ticket: 88302, Command: 0, Symbol: "EURUSD", VolumeHundredths: 10, Profit: 10, OpenPrice: 1, ClosePrice: 1}}}
	mixed, holdings, err := syncer.Convert(mixedJob, mixedResult, now.Add(-time.Second).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishScheduled(t.Context(), mixedJob, mixed, holdings); err != nil {
		t.Fatal(err)
	}
	f.Store.Read.First(&mixed, "id=?", mixed.ID)
	if mixed.RestWindowID != "" || mixed.RestVerified {
		t.Fatal("missing symbol model widened template")
	}
	c := f.Account(t, server, endpoint, 9880, "USD")
	restSnapshot(t, f, c, *now, "XAUUSD.c", 10)
	*now = now.Add(time.Minute)
	shared := restSnapshot(t, f, c, *now, "XAUUSD.c", 10)
	if !shared.RestVerified {
		t.Fatal("shared server/symbol model not used")
	}
	// An equity/fee change exits reduced sampling but is not a quote tick.
	*now = now.Add(time.Minute)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 11)
	var activity storage.MarketActivityObservation
	if err = f.Store.Read.Order("id DESC").First(&activity).Error; err != nil || activity.QuoteChanged || !activity.AccountChanged {
		t.Fatal("funds learned as quotes", err)
	}
	var last storage.MarketLearningEvent
	f.Store.Read.Where("model_id=?", model.ID).Order("id DESC").First(&last)
	if last.State != "adopted" {
		t.Fatal("funds suspended quote model")
	}
	// A true quote conflict suspends the shared model in the publishing tx.
	*now = now.Add(time.Minute)
	if err = f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: "USD", Balance: 1000, Equity: 1011}, Trades: []mt4.Trade{{Ticket: 78301, Command: 0, Symbol: "XAUUSD.c", VolumeHundredths: 10, Profit: 11, OpenPrice: 1, ClosePrice: 2}}}
	snap, positions, err := syncer.Convert(job, result, now.Add(-time.Second).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishScheduled(t.Context(), job, snap, positions); err != nil {
		t.Fatal(err)
	}
	last = storage.MarketLearningEvent{}
	f.Store.Read.Where("model_id=?", model.ID).Order("id DESC").First(&last)
	if last.State != "suspended" || last.Reason != "quote_conflict" {
		t.Fatal("quote conflict not suspended", last.State, last.Reason)
	}
	other, err := f.Accounts.Detail(t.Context(), c.ID, "")
	if err != nil || other.Account.MarketRest {
		t.Fatal("shared conflict not immediately visible", err)
	}
	report, err = f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, report.Result.Metrics.NetProfit, "0")
	// Neither a model nor the historical window may be edited in place.
	for _, item := range []any{&model, &window} {
		err = f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error { return tx.Model(item).Update("id", strings.Repeat("f", 64)).Error })
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatal("mutable model/window", err)
		}
	}
}
