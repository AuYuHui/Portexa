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

func TestAnalyticsObservedClockRequiresQuorumAndCurrentProvenance(t *testing.T) {
	for _, scenario := range []string{"missing evidence", "insufficient quorum", "configuration changed", "endpoint revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f, a, _, start := calibratedAnalyticsFixture(t)
			if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
				switch scenario {
				case "missing evidence":
					return tx.Where("server_id=?", a.ServerID).Delete(&storage.ClockObservation{}).Error
				case "insufficient quorum":
					return tx.Where("account_id=? AND ticket=?", a.ID, 71001).Delete(&storage.ClockObservation{}).Error
				case "configuration changed":
					return tx.Model(&storage.Account{}).Where("id=?", a.ID).Update("config_version", a.ConfigVersion+1).Error
				default:
					return tx.Model(&storage.Endpoint{}).Where("id=?", a.PinnedEndpointID).Update("state", "retired").Error
				}
			}); err != nil {
				t.Fatal(err)
			}
			view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(3 * time.Minute).UnixMilli()})
			if err != nil || view.Result.Quality.HistoryComplete || view.Result.Metrics.ReturnPct.Value != nil {
				t.Fatal("unsupported clock evidence was trusted", view.Result.Quality, err)
			}
		})
	}
}

func TestAnalyticsObservedClockConflictDoesNotRewriteEarlierValidInterval(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	trade := clockTrade(73001)
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, a, *now, []mt4.Trade{trade})
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, a, *now, nil)
	publishClock(t, f, a, *now, closedClock(trade, *now, 180))
	before, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(3 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, before.Result.Metrics.ReturnPct, "0.3")
	late := now.Add(time.Minute)
	for i := 0; i <= 2; i++ {
		*now = late.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 100000, 0)
	}
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: late.UnixMilli(), ToMS: now.UnixMilli()})
	if err != nil || view.Result.Quality.HistoryComplete || view.Result.Metrics.ReturnPct.Value != nil {
		t.Fatal("a conflicting measurement retained the old calendar", view.Result.Quality, err)
	}
}

func TestAnalyticsObservedClockExpiresAtKnownDSTTransition(t *testing.T) {
	now := time.Date(2026, 3, 8, 6, 50, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
	a := f.Account(t, server, endpoint, 9852, "USC")
	learnClock(t, f, a, &now, 74001, 120)
	start := now.Add(time.Minute)
	for i := 0; i <= 2; i++ {
		now = start.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, now, 100000, float64(i)*100)
	}
	now = now.Add(time.Minute)
	analyticsHistory(t, f, a, now, nil)
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(2 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.ReturnPct, "0.2")
	// No periodic refresh or new event has run yet. The historical reader
	// itself must refuse the pre-transition fixed offset.
	start = time.Date(2026, 3, 8, 7, 1, 0, 0, time.UTC)
	for i := 0; i <= 2; i++ {
		now = start.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, now, 100000, 0)
	}
	view, err = f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: now.UnixMilli()})
	if err != nil || view.Result.Quality.HistoryComplete || view.Result.Metrics.ReturnPct.Value != nil {
		t.Fatal("measurement crossed a known DST boundary", view.Result.Quality, err)
	}
}

func TestAnalyticsObservedClockAlsoLocatesCashFlowMarkers(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	stamp := start.Add(90 * time.Second)
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 75001, Command: 6, OpenTime: uint32(stamp.Unix() + 2*3600), Profit: 500, Comment: "Deposit"}})
	view, err := f.Accounts.AccountTrend(t.Context(), a.ID, trendRange(start, start.Add(3*time.Minute), "USD"))
	if err != nil || view.UnplacedEvents != 0 || len(view.Events) != 1 || view.Events[0].TimeMS != stamp.UnixMilli() || view.Events[0].Amount.String() != "5" {
		t.Fatal(view.Events, err)
	}
}

func TestAnalyticsObservedClockTransferUsesSameBoundedCalendar(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	*now = start
	b := f.Account(t, storage.Server{ID: a.ServerID}, storage.Endpoint{ID: a.PinnedEndpointID}, 9853, "USC")
	analyticsSnapshot(t, f, b, start, 100000, 0)
	*now = start.Add(4 * time.Minute)
	outAt, inAt := start.Add(90*time.Second), start.Add(150*time.Second)
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 76001, Command: 6, OpenTime: uint32(outAt.Unix() + 2*3600), Profit: -500, Comment: "Unknown"}})
	analyticsHistory(t, f, b, *now, []mt4.Trade{{Ticket: 76002, Command: 6, OpenTime: uint32(inAt.Unix() + 2*3600), Profit: 500, Comment: "Unknown"}})
	classify(t, f, a.ID, 76001, 1, 0, "transfer_out")
	classify(t, f, b.ID, 76002, 1, 0, "transfer_in")
	match, err := f.Accounts.MatchAnalyticsTransfer(t.Context(), accounts.TransferInput{OutAccountID: a.ID, OutTicket: 76001, OutRevision: 1, InAccountID: b.ID, InTicket: 76002, InRevision: 1, Reason: "合成转账到账依据"})
	if err != nil || match.OutMS != outAt.UnixMilli() || match.InMS != inAt.UnixMilli() || match.AmountUSD.String() != "5" {
		t.Fatal("transfer used a different clock from analytics", match, err)
	}
}

func TestAnalyticsObservedClockRenewalKeepsExpiredGap(t *testing.T) {
	f, a, now, start := calibratedAnalyticsFixture(t)
	*now = now.Add(25 * time.Hour)
	learnClock(t, f, a, now, 77001, 120)
	renewed := now.Add(time.Minute)
	for i := 0; i <= 2; i++ {
		*now = renewed.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 100000, float64(i)*100)
	}
	*now = now.Add(time.Minute)
	analyticsHistory(t, f, a, *now, nil)
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: renewed.UnixMilli(), ToMS: renewed.Add(2 * time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.ReturnPct, "0.2")
	gapStart := start.Add(24*time.Hour + 30*time.Minute)
	view, err = f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: gapStart.UnixMilli(), ToMS: renewed.Add(2 * time.Minute).UnixMilli()})
	if err != nil || view.Result.Quality.HistoryComplete || view.Result.Metrics.ReturnPct.Value != nil {
		t.Fatal("renewal retroactively filled an expired interval", view.Result.Quality, err)
	}
}
