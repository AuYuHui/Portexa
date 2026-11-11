package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func clockTrade(ticket int32) mt4.Trade {
	return mt4.Trade{Ticket: ticket, Command: ticket % 2, Symbol: "SYNTHUSD", Digits: 2,
		OpenTime: 1794300000, VolumeHundredths: 4, OpenPrice: 100, ClosePrice: 101}
}

func clockSnapshot(t *testing.T, f testutil.Fixture, account storage.Account, now time.Time, trades []mt4.Trade) {
	t.Helper()
	ctx := context.Background()
	if err := f.Accounts.RequestSync(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := testutil.Result("USC")
	result.Trades = trades
	snapshot, positions, err := syncer.Convert(job, result, now.Add(-time.Second).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.Publish(ctx, job, snapshot, positions, now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func closedClock(trade mt4.Trade, now time.Time, offset int) mt4.Trade {
	trade.CloseTime = uint32(now.Add(-30*time.Second).Unix() + int64(offset*60))
	return trade
}

func clockHistory(job accounts.ClockJob, trades ...mt4.Trade) mt4.Result {
	result := testutil.Result("USC")
	result.HistoryRead, result.HistoryComplete = true, true
	result.HistoryCovered = []mt4.HistoryRange{{Start: job.Run.WindowStart, End: job.Run.WindowEnd}}
	result.History = trades
	return result
}

func publishClock(t *testing.T, f testutil.Fixture, account storage.Account, now time.Time, trades ...mt4.Trade) accounts.ClockJob {
	t.Helper()
	job, err := f.Accounts.ClaimClock(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := syncer.ConvertClock(job, clockHistory(job, trades...))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishClock(context.Background(), job, closed, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return job
}

func clockView(t *testing.T, f testutil.Fixture) accounts.ServerView {
	t.Helper()
	views, err := f.Accounts.Servers(context.Background())
	if err != nil || len(views) != 1 {
		t.Fatal("server view", err)
	}
	return views[0]
}

func learnClock(t *testing.T, f testutil.Fixture, account storage.Account, now *time.Time, first int32, offset int) {
	t.Helper()
	a, b, c := clockTrade(first), clockTrade(first+1), clockTrade(first+2)
	clockSnapshot(t, f, account, *now, []mt4.Trade{a, b, c})
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, account, *now, []mt4.Trade{c})
	publishClock(t, f, account, *now, closedClock(a, *now, offset), closedClock(b, *now, offset))
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, account, *now, nil)
	publishClock(t, f, account, *now, closedClock(c, *now, offset))
}

func TestTradeClockBootstrapsUnknownServerAndUsesServerDay(t *testing.T) {
	now := time.Date(2026, 11, 10, 19, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Clock")
	account := f.Account(t, server, endpoint, 7901, "USC")
	a, b, c := clockTrade(101), clockTrade(102), clockTrade(103)
	clockSnapshot(t, f, account, now, []mt4.Trade{a, b, c})
	if _, err := f.Accounts.ClaimClock(context.Background(), account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("initial open orders created false events", err)
	}
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, []mt4.Trade{c})
	job := publishClock(t, f, account, now, closedClock(a, now, 345), closedClock(b, now, 345), closedClock(clockTrade(999), now, 345))
	view := clockView(t, f)
	if view.TimeZoneVerified || view.AutomaticTimeZone.EvidenceCount != 2 || view.AutomaticTimeZone.EventWindowCount != 1 {
		t.Fatal("too few observations were adopted", view)
	}
	if job.Run.WindowEnd-job.Run.WindowStart < 26*3600 {
		t.Fatal("bootstrap omitted possible server offsets")
	}
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	publishClock(t, f, account, now, closedClock(c, now, 345))
	view = clockView(t, f)
	if view.TimeMode != "calibrated" || view.TimeZone != "UTC+5:45" || view.AutomaticTimeZone.Source != "events" ||
		view.AutomaticTimeZone.EvidenceCount != 3 || view.AutomaticTimeZone.EventWindowCount != 2 || view.AutomaticTimeZone.SourceURL != "" {
		t.Fatal("generic offset was not learned", view)
	}
	ctx := context.Background()
	daily, err := f.Accounts.ClaimDaily(ctx, account.ID)
	if err != nil || daily.Day.Date != "2026-11-11" || daily.Day.TimeZone != "UTC+5:45" {
		t.Fatal("learned clock was not used for the server day", daily.Day, err)
	}
	if err = f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if clockView(t, f).TimeRulesetID != view.TimeRulesetID {
		t.Fatal("restart duplicated or discarded measured clock")
	}
	if _, err = f.Accounts.ClaimClock(ctx, account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("historical replay created new clock evidence", err)
	}
	// Expiry is enforced by reads even before the periodic refresh runs.
	now = now.Add(24*time.Hour + time.Second)
	detail, err := f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || detail.Account.Daily.State != "time_zone_unconfirmed" || clockView(t, f).TimeZoneVerified {
		t.Fatal("stale measured offset remained authoritative", err)
	}
	if err := f.Accounts.RefreshServerTimes(ctx); err != nil {
		t.Fatal(err)
	}
	if clockView(t, f).TimeRulesetID != "" {
		t.Fatal("unknown server silently acquired a fallback")
	}
}

func TestTradeClockNeedsMoreThanOneObservationWindow(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-OneWindow")
	account := f.Account(t, server, endpoint, 7902, "USC")
	trades := []mt4.Trade{clockTrade(201), clockTrade(202), clockTrade(203)}
	clockSnapshot(t, f, account, now, trades)
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	publishClock(t, f, account, now, closedClock(trades[0], now, -300), closedClock(trades[1], now, -300), closedClock(trades[2], now, -300))
	view := clockView(t, f)
	if view.AutomaticTimeZone.EvidenceCount != 3 || view.AutomaticTimeZone.EventWindowCount != 1 || view.TimeZoneVerified {
		t.Fatal("a batch of simultaneous closes was treated as independent rounds", view)
	}
}

func TestTradeClockConflictResetsConfidenceAndInvalidatesOldDaily(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-ChangedClock")
	account := f.Account(t, server, endpoint, 7903, "USC")
	learnClock(t, f, account, &now, 301, 120)
	before := clockView(t, f)
	ctx := context.Background()
	daily, err := f.Accounts.ClaimDaily(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := syncer.ConvertDaily(daily, dailyResult(daily), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishDaily(ctx, daily, report, now.Add(5*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	a, b, c := clockTrade(401), clockTrade(402), clockTrade(403)
	clockSnapshot(t, f, account, now, []mt4.Trade{a, b, c})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, []mt4.Trade{b, c})
	publishClock(t, f, account, now, closedClock(a, now, 180))
	view := clockView(t, f)
	if view.TimeZoneVerified || view.AutomaticTimeZone.EvidenceCount != 1 || view.AutomaticTimeZone.Status != "conflict" {
		t.Fatal("conflicting event retained the old clock or confidence", view)
	}
	detail, err := f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || detail.Account.Daily.Report != nil {
		t.Fatal("disputed server day reused old daily money", err)
	}
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	publishClock(t, f, account, now, closedClock(b, now, 180), closedClock(c, now, 180))
	view = clockView(t, f)
	if view.TimeZone != "UTC+3" || view.TimeMode != "calibrated" || view.TimeRulesetID == before.TimeRulesetID || view.AutomaticTimeZone.EvidenceCount != 3 {
		t.Fatal("replacement offset was not independently confirmed", view)
	}
	detail, err = f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || detail.Account.Daily.Report != nil {
		t.Fatal("replacement calendar reused a prior ruleset report", err)
	}
}

func TestTradeClockOfficialFallbackManualPriorityAndDST(t *testing.T) {
	now := time.Date(2026, 3, 8, 6, 50, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
	account := f.Account(t, server, endpoint, 7904, "USC")
	learnClock(t, f, account, &now, 501, 120)
	view := clockView(t, f)
	if view.TimeMode != "calibrated" {
		t.Fatal("official initialization prevented event calibration", view)
	}
	// US spring transition invalidates a pre-transition fixed measurement.
	now = time.Date(2026, 3, 8, 7, 1, 0, 0, time.UTC)
	if err := f.Accounts.RefreshServerTimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	view = clockView(t, f)
	if view.TimeMode != "automatic" || view.TimeZone != "UTC+3" {
		t.Fatal("old measurement overrode the DST transition", view)
	}
	if _, err := f.Accounts.ConfirmServerTime(context.Background(), server.ID, accounts.ServerTimeInput{
		PreviousRulesetID: view.TimeRulesetID, Mode: "manual", TimeZone: "UTC",
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	learnClock(t, f, account, &now, 601, 240)
	view = clockView(t, f)
	if view.TimeMode != "manual" || view.TimeZone != "UTC" || view.AutomaticTimeZone.TimeZone != "UTC+4" {
		t.Fatal("automatic calibration overwrote explicit manual correction", view)
	}
	if _, err := f.Accounts.ConfirmServerTime(context.Background(), server.ID, accounts.ServerTimeInput{
		PreviousRulesetID: view.TimeRulesetID, Mode: "automatic",
	}); err != nil {
		t.Fatal(err)
	}
	view = clockView(t, f)
	if view.TimeMode != "calibrated" || view.TimeZone != "UTC+4" {
		t.Fatal("restore automatic ignored calibrated events", view)
	}
	now = now.Add(24*time.Hour + time.Second)
	if err := f.Accounts.RefreshServerTimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	view = clockView(t, f)
	if view.TimeMode != "automatic" || view.TimeZone != "UTC+3" {
		t.Fatal("expired observation did not fall back to official policy", view)
	}
}

func TestTradeClockRenewalKeepsRulesetAndRejectsDSTStraddlingWindow(t *testing.T) {
	now := time.Date(2026, 3, 8, 6, 40, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
	account := f.Account(t, server, endpoint, 7905, "USC")
	learnClock(t, f, account, &now, 701, 120)
	before := clockView(t, f)
	now = now.Add(time.Minute)
	trade := clockTrade(704)
	clockSnapshot(t, f, account, now, []mt4.Trade{trade})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	publishClock(t, f, account, now, closedClock(trade, now, 120))
	after := clockView(t, f)
	if after.TimeRulesetID != before.TimeRulesetID || after.AutomaticTimeZone.EvidenceCount != 4 ||
		*after.AutomaticTimeZone.ExpiresAtMS <= *before.AutomaticTimeZone.ExpiresAtMS {
		t.Fatal("fresh matching event did not renew without ruleset churn", after)
	}
	// Closes just before a known switch must not be interpreted as evidence of
	// the current offset simply because the next snapshot finished after it.
	now = time.Date(2026, 3, 8, 6, 59, 40, 0, time.UTC)
	trade = clockTrade(705)
	clockSnapshot(t, f, account, now, []mt4.Trade{trade})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	publishClock(t, f, account, now, closedClock(trade, now, 120))
	after = clockView(t, f)
	if after.TimeMode != "automatic" || after.TimeZone != "UTC+3" || after.AutomaticTimeZone.EvidenceCount != 0 {
		t.Fatal("a DST-straddling interval preserved the pre-switch offset", after)
	}
}
