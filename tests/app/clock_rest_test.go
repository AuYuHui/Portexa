package app_test

import (
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func learnRestClock(t *testing.T, f testutil.Fixture, a storage.Account, now *time.Time, first int32, offset int, symbol string) {
	t.Helper()
	x, y, z := clockTrade(first), clockTrade(first+1), clockTrade(first+2)
	for _, trade := range []*mt4.Trade{&x, &y, &z} {
		trade.Symbol = symbol
		trade.OpenTime = uint32(now.Add(-time.Hour).Unix())
	}
	clockSnapshot(t, f, a, *now, []mt4.Trade{x, y, z})
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, a, *now, []mt4.Trade{z})
	publishClock(t, f, a, *now, closedClock(x, *now, offset), closedClock(y, *now, offset))
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, a, *now, nil)
	publishClock(t, f, a, *now, closedClock(z, *now, offset))
}

func TestTradeClockRestPausesAgingAndResumesAfterOpening(t *testing.T) {
	for _, scenario := range []struct {
		name, symbol string
		start        time.Time
		extra        time.Duration
	}{
		{"winter", "XAUUSD.c", time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC), 46 * time.Hour},
		{"summer", "EURUSD", time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC), 46 * time.Hour},
		{"spring transition", "EURUSD", time.Date(2026, 3, 6, 11, 0, 0, 0, time.UTC), 45 * time.Hour},
		{"fall transition", "EURUSD", time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC), 47 * time.Hour},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			now := scenario.start
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := f.Server(t, "Synthetic-Clock-Rest")
			a := f.Account(t, server, endpoint, 7920, "USC")
			learnRestClock(t, f, a, &now, 80001, 120, scenario.symbol)
			initial := clockView(t, f)
			wantExpiry := now.Add(24*time.Hour + scenario.extra).UnixMilli()
			if !initial.TimeZoneVerified || initial.AutomaticTimeZone.ExpiresAtMS == nil || *initial.AutomaticTimeZone.ExpiresAtMS != wantExpiry {
				t.Fatal("clock expiry did not exclude the actual weekend duration", initial.AutomaticTimeZone, wantExpiry)
			}
			now = scenario.start.Add(60 * time.Hour)
			if err := f.Accounts.RefreshServerTimes(t.Context()); err != nil {
				t.Fatal(err)
			}
			rested := clockView(t, f)
			if !rested.TimeZoneVerified || rested.TimeZone != "UTC+2" || rested.TimeRulesetID != initial.TimeRulesetID {
				t.Fatal("rest erased a confirmed offset or created a new rule", rested)
			}
			// No new events: active-time expiry still applies after reopening.
			now = time.UnixMilli(wantExpiry).Add(time.Second)
			if err := f.Accounts.RefreshServerTimes(t.Context()); err != nil {
				t.Fatal(err)
			}
			if clockView(t, f).TimeZoneVerified {
				t.Fatal("weekend pause made the observation permanently valid")
			}
		})
	}
}

func TestTradeClockRestDoesNotReviveExpiredOrUnknownSymbols(t *testing.T) {
	for _, scenario := range []struct {
		name, symbol string
		start        time.Time
	}{
		{"expired before rest", "EURUSD", time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)},
		{"crypto", "BTCUSD", time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)},
		{"unknown", "SYNTHUSD", time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			now := scenario.start
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := f.Server(t, "Synthetic-Clock-No-Rest")
			a := f.Account(t, server, endpoint, 7921, "USC")
			learnRestClock(t, f, a, &now, 80101, 120, scenario.symbol)
			now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
			if err := f.Accounts.RefreshServerTimes(t.Context()); err != nil {
				t.Fatal(err)
			}
			if clockView(t, f).TimeZoneVerified {
				t.Fatal("rest revived expired or ineligible evidence")
			}
		})
	}
}

func TestTradeClockRestStillStopsAtKnownDSTChange(t *testing.T) {
	now := time.Date(2026, 3, 6, 11, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
	a := f.Account(t, server, endpoint, 7922, "USC")
	learnRestClock(t, f, a, &now, 80201, 120, "EURUSD")
	want := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
	view := clockView(t, f)
	if view.AutomaticTimeZone.ExpiresAtMS == nil || *view.AutomaticTimeZone.ExpiresAtMS != want.UnixMilli() {
		t.Fatal("rest carried a fixed offset beyond the known DST switch", view.AutomaticTimeZone)
	}
	now = want.Add(time.Minute)
	if err := f.Accounts.RefreshServerTimes(t.Context()); err != nil {
		t.Fatal(err)
	}
	if view := clockView(t, f); view.TimeMode != "automatic" || view.TimeZone != "UTC+3" {
		t.Fatal("post-switch fallback did not replace the old offset", view)
	}
}

func TestTradeClockRestRechecksNewTradesAfterOpening(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		offset int
	}{{"matching", 120}, {"conflicting", 180}} {
		t.Run(scenario.name, func(t *testing.T) {
			now := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := f.Server(t, "Synthetic-Clock-Reopen")
			a := f.Account(t, server, endpoint, 7923, "USC")
			learnRestClock(t, f, a, &now, 80301, 120, "EURUSD")
			before := clockView(t, f)
			now = time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
			trade := clockTrade(80304)
			trade.Symbol = "EURUSD"
			clockSnapshot(t, f, a, now, []mt4.Trade{trade})
			now = now.Add(time.Minute)
			clockSnapshot(t, f, a, now, nil)
			publishClock(t, f, a, now, closedClock(trade, now, scenario.offset))
			after := clockView(t, f)
			if scenario.offset == 120 {
				if !after.TimeZoneVerified || after.TimeRulesetID != before.TimeRulesetID || *after.AutomaticTimeZone.ExpiresAtMS <= *before.AutomaticTimeZone.ExpiresAtMS {
					t.Fatal("matching reopen event did not renew the original rule", after)
				}
			} else if after.TimeZoneVerified || after.AutomaticTimeZone.Status != "conflict" {
				t.Fatal("conflicting reopen event retained the old offset", after)
			}
		})
	}
}

func TestAnalyticsRestClockPreservesMondayBeforeRecalibration(t *testing.T) {
	now := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Clock-Monday")
	a := f.Account(t, server, endpoint, 7924, "USC")
	learnRestClock(t, f, a, &now, 80401, 120, "XAUUSD.c")
	start := time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC) // Monday in Beijing.
	now = start
	analyticsSnapshot(t, f, a, now, 100000, 0)
	now = time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	learnRestClock(t, f, a, &now, 80411, 120, "XAUUSD.c")
	end := start.Add(24 * time.Hour)
	now = end
	analyticsSnapshot(t, f, a, now, 100000, 0)
	now = now.Add(time.Second)
	analyticsHistory(t, f, a, now, nil)
	now = now.Add(time.Second)
	backfill, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, backfill, nil, now)
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: end.UnixMilli()})
	if err != nil || !view.Result.Quality.HistoryComplete || view.Result.Quality.UnresolvedTimes != 0 {
		t.Fatal("Monday lost Friday's evidence before reopen confirmation", view.Result.Quality, err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "0")
	if len(view.Result.Days) != 1 || view.Result.Days[0].Trades == nil || *view.Result.Days[0].Trades != 0 {
		t.Fatal("complete Monday calendar remained unverified", view.Result.Days)
	}
}

func TestAnalyticsReopenConflictRevokesOnlyUnconfirmedRestCarry(t *testing.T) {
	for _, scenario := range []struct {
		name string
		at   time.Time
	}{
		{"active quorum", time.Date(2026, 11, 16, 7, 1, 0, 0, time.UTC)},
		{"quorum just expired", time.Date(2026, 11, 16, 9, 1, 30, 0, time.UTC)},
	} {
		t.Run(scenario.name, func(t *testing.T) { checkRestClockConflict(t, scenario.at) })
	}
}

func checkRestClockConflict(t *testing.T, conflictAt time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Clock-Rest-Conflict")
	a := f.Account(t, server, endpoint, 7925, "USC")
	learnRestClock(t, f, a, &now, 80501, 120, "EURUSD")
	friday := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	saturday := friday.Add(24 * time.Hour)
	monday := time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{friday, friday.Add(time.Minute), saturday, saturday.Add(time.Minute), monday, end} {
		now = at
		analyticsSnapshot(t, f, a, now, 100000, 0)
	}
	now = end.Add(time.Second)
	analyticsHistory(t, f, a, now, nil)
	now = now.Add(time.Second)
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, nil, now)
	request := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: monday.UnixMilli(), ToMS: end.UnixMilli()}
	before, err := f.Accounts.Analytics(t.Context(), request)
	if err != nil || !before.Result.Quality.HistoryComplete {
		t.Fatal("fixture did not establish the carried weekend clock", before.Result.Quality, err)
	}
	assertMetric(t, before.Result.Metrics.NetProfit, "0")
	now = conflictAt.Add(-time.Minute)
	trade := clockTrade(80511)
	trade.Symbol = "EURUSD"
	clockSnapshot(t, f, a, now, []mt4.Trade{trade})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, a, now, nil)
	publishClock(t, f, a, now, closedClock(trade, now, 180))
	for _, span := range [][2]time.Time{{monday, end}, {saturday, saturday.Add(time.Minute)}} {
		request.FromMS, request.ToMS = span[0].UnixMilli(), span[1].UnixMilli()
		after, err := f.Accounts.Analytics(t.Context(), request)
		if err != nil || after.Result.Quality.HistoryComplete || after.Result.Metrics.NetProfit.Value != nil {
			t.Fatal("reopen conflict left the unconfirmed rest carry trusted", after.Result.Quality, err)
		}
	}
	request.FromMS, request.ToMS = friday.UnixMilli(), friday.Add(time.Minute).UnixMilli()
	earlier, err := f.Accounts.Analytics(t.Context(), request)
	if err != nil || !earlier.Result.Quality.HistoryComplete {
		t.Fatal("reopen conflict erased the confirmed pre-rest interval", earlier.Result.Quality, err)
	}
	assertMetric(t, earlier.Result.Metrics.NetProfit, "0")
}
