package app_test

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func TestMarketRestDayBoundaryAndOpeningRestoreNormalCadence(t *testing.T) {
	for _, label := range []string{"midnight", "opening"} {
		t.Run(label, func(t *testing.T) {
			f, a, now, start := restFixture(t)
			if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 300}); err != nil {
				t.Fatal(err)
			}
			target := time.Date(start.Year(), start.Month(), start.Day(), 15, 58, 0, 0, time.UTC)
			if label == "opening" {
				_, end, _ := marketrest.Window(start)
				target = end.Add(-5 * time.Minute)
			}
			*now = target.Add(-2 * time.Minute)
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			*now = target.Add(-time.Minute)
			previous := restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			if *previous.CollectionIntervalSeconds != 300 {
				t.Fatal("failed to enter rest before guard")
			}
			var account storage.Account
			if err := f.Store.Read.First(&account, "id=?", a.ID).Error; err != nil {
				t.Fatal(err)
			}
			if account.NextSyncAtMS > target.UnixMilli() {
				t.Fatal("slept through guard", account.NextSyncAtMS, target)
			}
			*now = target
			point := restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			want := 60
			if label == "midnight" {
				want = 30
			}
			if *point.CollectionIntervalSeconds != want {
				t.Fatal("guard did not restore regular sampling")
			}
			view, err := f.Accounts.Detail(t.Context(), a.ID, "")
			if err != nil || view.Account.EffectiveCollectionIntervalSeconds != want || view.Account.MarketRest {
				t.Fatal("guard reporting differs from actual cadence", view.Account.EffectiveCollectionIntervalSeconds, view.Account.MarketRest, err)
			}
		})
	}
}

func TestMarketRestPolicyChangesDoNotEraseHistoricalProof(t *testing.T) {
	f, a, now, start := restFixture(t)
	for _, offset := range []time.Duration{0, time.Minute, 6 * time.Minute} {
		*now = start.Add(offset)
		restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	}
	*now = now.Add(time.Second)
	analyticsHistory(t, f, a, *now, nil)
	off := marketrest.Off
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 60, MarketRestTemplate: &off}); err != nil {
		t.Fatal(err)
	}
	view, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil || view.Account.MarketRest {
		t.Fatal("disabled policy still live", err)
	}
	result, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.Add(time.Minute).UnixMilli(), ToMS: start.Add(6 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, result.Result.Metrics.NetProfit, "0")
	if result.Result.Quality.RestCarriedPoints != 3 {
		t.Fatal("new policy rewrote history", result.Result.Quality)
	}
	short, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(4 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if short.Result.ToMS != start.Add(4*time.Minute).UnixMilli() || short.Result.Quality.RestCarriedPoints == 0 {
		t.Fatal("verified historical end was truncated", short.Result.ToMS, short.Result.Quality)
	}
}

func TestMarketRestMixedAccountSamplingKeepsPortfolioComplete(t *testing.T) {
	f, a, now, start := restFixture(t)
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id=?", a.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	b := f.Account(t, server, endpoint, 9873, "USD")
	for _, offset := range []time.Duration{0, time.Minute, 6 * time.Minute} {
		*now = start.Add(offset)
		restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
		*now = now.Add(20 * time.Second)
		restSnapshot(t, f, b, *now, "EURUSD", 10)
	}
	*now = now.Add(time.Second)
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		return tx.Model(&storage.Account{}).Where("id IN ?", []string{a.ID, b.ID}).Update("next_sync_at_ms", now.Add(time.Hour).UnixMilli()).Error
	}); err != nil {
		t.Fatal(err)
	}
	analyticsHistory(t, f, a, *now, nil)
	analyticsHistory(t, f, b, *now, nil)
	from, to := start.Add(2*time.Minute).UnixMilli(), start.Add(6*time.Minute).UnixMilli()
	report, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{Unit: "USD", FromMS: from, ToMS: to})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, report.Result.Metrics.NetProfit, "0")
	assertMetric(t, report.Result.Metrics.ReturnPct, "0")
	if report.Result.Quality.ValidPoints != report.Result.Quality.ExpectedPoints {
		t.Fatal("mixed rest sampling created false gaps", report.Result.Quality)
	}
	series, err := f.Accounts.PortfolioTrend(t.Context(), accounts.TrendRequest{Unit: "USD", FromMS: from, ToMS: to})
	if err != nil {
		t.Fatal(err)
	}
	if series.GapPoints != 0 || series.RestCarriedPoints == 0 {
		t.Fatal("mixed trend disagrees", series.GapPoints, series.RestCarriedPoints)
	}
}
