package app_test

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestAnalyticsLegacySnapshotsUseObservedTimesWithoutInventingCadence(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	for i := 0; i <= 4; i++ {
		*now = start.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 1000, float64(i)*10)
	}
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&storage.Snapshot{}).Where("account_id=?", a.ID).Update("collection_interval_seconds", nil).Error
	}); err != nil {
		t.Fatal(err)
	}
	*now = start.Add(5 * time.Minute)
	analyticsHistory(t, f, a, *now, nil)
	req := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(4 * time.Minute).UnixMilli()}
	view, err := f.Accounts.Analytics(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "40")
	assertMetric(t, view.Result.Metrics.ReturnPct, "4")
	if view.Result.Quality.ValidPoints != 5 || view.Result.Quality.UnknownCadence != 5 {
		t.Fatal("legacy samples were discarded or cadence was hidden", view.Result.Quality)
	}
	var count int64
	if err := f.Store.Read.Model(&storage.Snapshot{}).Where("account_id=? AND collection_interval_seconds IS NOT NULL", a.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("analysis rewrote source cadence", count, err)
	}
	// A real missing interval must still stop the complete return path, while
	// reliable endpoints can retain the independently reconciled amount.
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Where("account_id=? AND observed_to_ms>=? AND observed_to_ms<=?", a.ID,
			start.Add(time.Minute).UnixMilli(), start.Add(3*time.Minute).UnixMilli()).Delete(&storage.Snapshot{}).Error
	}); err != nil {
		t.Fatal(err)
	}
	view, err = f.Accounts.Analytics(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "40")
	if view.Result.Metrics.ReturnPct.Value != nil || view.Result.Quality.ValidPoints >= 5 {
		t.Fatal("real legacy gap was filled", view.Result)
	}
}

func calibratedAnalyticsFixture(t *testing.T) (testutil.Fixture, storage.Account, *time.Time, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Analytics-Clock")
	a := f.Account(t, server, endpoint, 9851, "USC")
	learnClock(t, f, a, &now, 71001, 120)
	start := now.Add(time.Minute)
	for i := 0; i <= 3; i++ {
		now = start.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, now, 100000, float64(i)*100)
	}
	now = start.Add(4 * time.Minute)
	analyticsHistory(t, f, a, now, nil)
	return f, a, &now, start
}

func TestAnalyticsObservedClockUsesBoundedEvidenceAndIgnoresUnrelatedOldOrders(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	// This old record is selected by the broad SQL candidate window, but has
	// no possible place inside the fully calibrated requested interval.
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 72001, Command: 6,
		OpenTime: uint32(start.Add(-time.Hour).Unix() + 2*3600), Profit: 5, Comment: "Unknown"}})
	req := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(3 * time.Minute).UnixMilli()}
	view, err := f.Accounts.Analytics(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "3")
	assertMetric(t, view.Result.Metrics.ReturnPct, "0.3")
	if !view.Result.Quality.HistoryComplete || view.Result.Quality.UnresolvedTimes != 0 || view.Result.Quality.ValidIntervals != 3 {
		t.Fatal("calibrated history still blocked", view.Result.Quality)
	}
	// Expiring today's calibration must not invalidate a historical interval
	// that is still supported by immutable accepted observations.
	*now = now.Add(25 * time.Hour)
	if err := f.Accounts.RefreshServerTimes(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err = f.Accounts.Analytics(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.ReturnPct, "0.3")
	// It must also never extrapolate the measured offset beyond its evidence.
	late := now.Truncate(time.Minute)
	for i := 0; i <= 2; i++ {
		*now = late.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 100000, 0)
	}
	view, err = f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: late.UnixMilli(), ToMS: now.UnixMilli()})
	if err != nil || view.Result.Quality.HistoryComplete || view.Result.Metrics.ReturnPct.Value != nil {
		t.Fatal("expired observation became an unlimited calendar", view.Result.Quality, err)
	}
}

func TestAnalyticsObservedClockStillPlacesAndReconcilesCurrentTrades(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	trade := mt4.Trade{Ticket: 72002, Command: 0, Symbol: "SYNTHUSD", OpenTime: uint32(start.Unix()),
		CloseTime: uint32(start.Add(90*time.Second).Unix() + 2*3600), Profit: 100}
	analyticsHistory(t, f, a, *now, []mt4.Trade{trade})
	req := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(3 * time.Minute).UnixMilli()}
	view, err := f.Accounts.Analytics(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.Realized, "1")
	if view.Result.Metrics.NetProfit.Value != nil || view.Result.Quality.Reconciliation != "failed" {
		t.Fatal("accepting a measured clock bypassed financial reconciliation", view.Result)
	}
}
