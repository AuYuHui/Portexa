package app_test

import (
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestMarketRestCalendarDSTAndGuards(t *testing.T) {
	for _, tc := range []struct {
		at    string
		hours time.Duration
	}{{"2026-03-07T12:00:00Z", 45}, {"2026-11-01T12:00:00Z", 47}, {"2027-01-16T12:00:00Z", 46}} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		from, to, closed := marketrest.Window(at)
		if !closed || to.Sub(from) != tc.hours*time.Hour {
			t.Fatal("DST window", from, to, closed)
		}
		if _, _, yes := marketrest.Window(to); yes {
			t.Fatal("opening included in closure")
		}
		if marketrest.Reduced(from.Add(time.Minute)) || marketrest.Reduced(to.Add(-time.Minute)) {
			t.Fatal("lost opening/closing guard")
		}
	}
	for _, stamp := range []string{"2027-01-16T15:59:00Z", "2027-01-16T16:01:00Z"} {
		at, _ := time.Parse(time.RFC3339, stamp)
		if marketrest.Reduced(at) {
			t.Fatal("lost Beijing day guard", stamp)
		}
	}
	if !marketrest.SupportedSymbol("XAUUSD.c") || !marketrest.SupportedSymbol("EURUSD") || marketrest.SupportedSymbol("BTCUSD") || marketrest.SupportedSymbol("UNKNOWN") {
		t.Fatal("unsafe symbol applicability")
	}
}

func restFixture(t *testing.T) (testutil.Fixture, storage.Account, *time.Time, time.Time) {
	t.Helper()
	start := time.Date(time.Now().UTC().Year()+1, 1, 1, 6, 0, 0, 0, time.UTC)
	start = start.AddDate(0, 0, (int(time.Saturday)-int(start.Weekday())+7)%7)
	now := start.Add(-time.Hour)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Market-Rest")
	if _, err := f.Accounts.ConfirmServerTime(t.Context(), server.ID, accounts.ServerTimeInput{TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-rest-calendar"}); err != nil {
		t.Fatal(err)
	}
	a := f.Account(t, server, endpoint, 9871, "USD")
	return f, a, &now, start
}

func restSnapshot(t *testing.T, f testutil.Fixture, a storage.Account, at time.Time, symbol string, floating float64) storage.Snapshot {
	t.Helper()
	if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: *a.Currency, Balance: 1000, Equity: 1000 + floating},
		Trades: []mt4.Trade{{Ticket: 78301, Command: 0, Symbol: symbol, VolumeHundredths: 10, Profit: floating, OpenPrice: 1, ClosePrice: 1}}}
	if symbol == "" {
		result.Trades = nil
	}
	snapshot, positions, err := syncer.Convert(job, result, at.UnixMilli()-500, at.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishScheduled(t.Context(), job, snapshot, positions); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Read.First(&snapshot, "id=?", snapshot.ID).Error; err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestMarketRestSchedulingAndAutomaticExit(t *testing.T) {
	for _, scenario := range []string{"stable", "changed", "crypto", "cash_only", "exception", "off", "weekday", "late", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			f, a, now, start := restFixture(t)
			if scenario == "weekday" {
				start = start.Add(48 * time.Hour)
			}
			if scenario == "off" {
				off := marketrest.Off
				if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 60, MarketRestTemplate: &off}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "exception" {
				ids := []string{a.ID}
				if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 60, MarketRestExcludedAccountIDs: &ids}); err != nil {
					t.Fatal(err)
				}
			}
			symbol := "XAUUSD.c"
			if scenario == "crypto" {
				symbol = "BTCUSD"
			}
			floatingBase := float64(10)
			if scenario == "cash_only" {
				symbol = ""
				floatingBase = 0
			}
			*now = start
			restSnapshot(t, f, a, *now, symbol, floatingBase)
			*now = start.Add(time.Minute)
			second := restSnapshot(t, f, a, *now, symbol, floatingBase)
			want := 300
			if scenario == "off" || scenario == "exception" || scenario == "crypto" || scenario == "cash_only" || scenario == "weekday" {
				want = 60
			}
			if second.CollectionIntervalSeconds == nil || *second.CollectionIntervalSeconds != want {
				t.Fatal("wrong cadence", second.CollectionIntervalSeconds, want)
			}
			if want == 60 {
				return
			}
			if !second.RestVerified {
				t.Fatal("rest did not record immutable proof")
			}
			if scenario == "failed" {
				*now = now.Add(time.Minute)
				if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
					t.Fatal(err)
				}
				job, err := f.Accounts.Claim(t.Context(), a.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.Accounts.Fail(t.Context(), job, accounts.Failure{Code: "synthetic_failure"}, now.UnixMilli()); err != nil {
					t.Fatal(err)
				}
			}
			*now = start.Add(6 * time.Minute)
			if scenario == "late" {
				*now = start.Add(451 * time.Second)
			}
			floating := float64(10)
			if scenario == "changed" {
				floating = 11
			}
			third := restSnapshot(t, f, a, *now, symbol, floating)
			if scenario != "stable" {
				want = 60
			}
			if *third.CollectionIntervalSeconds != want {
				t.Fatal("did not exit rest", scenario, third)
			}
			if scenario == "stable" {
				*now = now.Add(4 * time.Minute)
				view, err := f.Accounts.Detail(t.Context(), a.ID, "")
				if err != nil || view.Account.Stale || !view.Account.MarketRest || view.Account.EffectiveCollectionIntervalSeconds != 300 {
					t.Fatal("planned rest marked stale", view.Account, err)
				}
				*now = now.Add(4 * time.Minute)
				view, err = f.Accounts.Detail(t.Context(), a.ID, "")
				if err != nil || !view.Account.Stale {
					t.Fatal("missing rest snapshots never stale", err)
				}
			}
		})
	}
}

func TestMarketRestEstimatesRequireTwoSnapshotsAndCompleteLedger(t *testing.T) {
	for _, scenario := range []string{"stable", "no_history", "changed", "zero_trade"} {
		t.Run(scenario, func(t *testing.T) {
			f, a, now, start := restFixture(t)
			*now = start
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			*now = start.Add(time.Minute)
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			*now = start.Add(6 * time.Minute)
			floating := float64(10)
			if scenario == "changed" {
				floating = 11
			}
			restSnapshot(t, f, a, *now, "XAUUSD.c", floating)
			*now = now.Add(time.Second)
			if scenario != "no_history" {
				var trades []mt4.Trade
				if scenario == "zero_trade" {
					trades = []mt4.Trade{{Ticket: 78302, Command: 0, Symbol: "XAUUSD", OpenTime: uint32(start.Unix()), CloseTime: uint32(start.Add(3 * time.Minute).Unix())}}
				}
				analyticsHistory(t, f, a, *now, trades)
			}
			for _, account := range []string{a.ID, ""} {
				result, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: account, Unit: "USD", FromMS: start.Add(time.Minute).UnixMilli(), ToMS: start.Add(6 * time.Minute).UnixMilli()})
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "stable" {
					assertMetric(t, result.Result.Metrics.NetProfit, "0")
					assertMetric(t, result.Result.Metrics.ReturnPct, "0")
					if result.Result.Quality.RestCarriedPoints != 3 {
						t.Fatal("did not certify the missing minutes", result.Result.Quality)
					}
				} else if result.Result.Quality.RestCarriedPoints != 0 {
					t.Fatal("invented a flat interval", scenario, result.Result.Quality)
				}
			}
			trend, err := f.Accounts.PortfolioTrend(t.Context(), accounts.TrendRequest{Unit: "USD", FromMS: start.Add(time.Minute).UnixMilli(), ToMS: start.Add(6 * time.Minute).UnixMilli()})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "stable" {
				if trend.RestCarriedPoints != 3 || trend.GapPoints != 0 {
					t.Fatal("trend disagrees with performance", trend.RestCarriedPoints, trend.GapPoints)
				}
			} else if trend.RestCarriedPoints != 0 {
				t.Fatal("trend invented flat values", scenario)
			}
		})
	}
}
