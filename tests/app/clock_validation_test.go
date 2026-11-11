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

func pendingClockFixture(t *testing.T, now *time.Time) (testutil.Fixture, storage.Account, mt4.Trade) {
	t.Helper()
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return *now }
	server, endpoint := f.Server(t, "Synthetic-PendingClock")
	account := f.Account(t, server, endpoint, 7950, "USC")
	trade := clockTrade(801)
	clockSnapshot(t, f, account, *now, []mt4.Trade{trade})
	*now = now.Add(time.Minute)
	clockSnapshot(t, f, account, *now, nil)
	return f, account, trade
}

func TestTradeClockRejectsPartialHistoryAndUnmatchedOldTrades(t *testing.T) {
	for _, test := range []struct {
		name            string
		edit            func(*mt4.Result)
		invalidResponse bool
	}{
		{"partial history", func(r *mt4.Result) { r.HistoryComplete = false }, true},
		{"unread history", func(r *mt4.Result) { r.HistoryRead = false }, true},
		{"unconfirmed session", func(r *mt4.Result) { r.SessionConfirmed = false }, true},
		{"coverage gap", func(r *mt4.Result) { r.HistoryCovered[0].Start++ }, true},
		{"duplicate ticket", func(r *mt4.Result) { r.History = append(r.History, r.History[0]) }, true},
		{"missing close time", func(r *mt4.Result) { r.History[0].CloseTime = 0 }, true},
		{"old trade", func(r *mt4.Result) { r.History[0].CloseTime -= 86400 }, false},
		{"different symbol", func(r *mt4.Result) { r.History[0].Symbol = "OTHERUSD" }, false},
		{"different open time", func(r *mt4.Result) { r.History[0].OpenTime++ }, false},
		{"different command", func(r *mt4.Result) { r.History[0].Command = 0 }, false},
		{"unobserved ticket", func(r *mt4.Result) { r.History[0].Ticket++ }, false},
		{"pending cancellation", func(r *mt4.Result) { r.History[0].Command = 2 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
			f, account, trade := pendingClockFixture(t, &now)
			job, err := f.Accounts.ClaimClock(context.Background(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			result := clockHistory(job, closedClock(trade, now, 120))
			test.edit(&result)
			closed, err := syncer.ConvertClock(job, result)
			if test.invalidResponse {
				if !errors.Is(err, syncer.ErrIncomplete) {
					t.Fatal("incomplete response was accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Accounts.PublishClock(context.Background(), job, closed, now.UnixMilli()); err != nil {
				t.Fatal(err)
			}
			view := clockView(t, f)
			if view.AutomaticTimeZone.EvidenceCount != 0 || view.TimeZoneVerified {
				t.Fatal("unqualified history became clock evidence", view)
			}
		})
	}
}

func TestTradeClockRejectsGapsPendingOrdersAndChangedConfigurations(t *testing.T) {
	for _, scenario := range []string{"long gap", "pending cancellation", "configuration change"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := f.Server(t, "Synthetic-NoGuess")
			account := f.Account(t, server, endpoint, 7951, "USC")
			trade := clockTrade(901)
			if scenario == "pending cancellation" {
				trade.Command = 3
			}
			clockSnapshot(t, f, account, now, []mt4.Trade{trade})
			now = now.Add(time.Minute)
			if scenario == "long gap" {
				now = now.Add(5 * time.Minute)
			}
			if scenario == "configuration change" {
				label := "更新后的合成账户"
				if err := f.Accounts.UpdateAccount(context.Background(), account.ID, accounts.AccountPatch{Label: &label}); err != nil {
					t.Fatal(err)
				}
			}
			clockSnapshot(t, f, account, now, nil)
			if _, err := f.Accounts.ClaimClock(context.Background(), account.ID); !errors.Is(err, accounts.ErrPaused) {
				t.Fatal("unbounded or incompatible change was calibrated", err)
			}
		})
	}
}

func TestTradeClockDoesNotRenewFromReplayedHistoryOrLateArrival(t *testing.T) {
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	f, account, trade := pendingClockFixture(t, &now)
	ctx := context.Background()
	job := publishClock(t, f, account, now, closedClock(trade, now, 120))
	first := clockView(t, f).AutomaticTimeZone
	if err := f.Accounts.PublishClock(ctx, job, nil, now.Add(time.Second).UnixMilli()); err == nil {
		t.Fatal("completed calibration run was published twice")
	}
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	if err := f.Accounts.RefreshServerTimes(ctx); err != nil {
		t.Fatal(err)
	}
	second := clockView(t, f).AutomaticTimeZone
	if first.EvidenceCount != 1 || second.EvidenceCount != 1 || *first.ExpiresAtMS != *second.ExpiresAtMS {
		t.Fatal("replay renewed clock confidence")
	}

	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, []mt4.Trade{clockTrade(902)})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, account, now, nil)
	late, err := f.Accounts.ClaimClock(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := syncer.ConvertClock(late, clockHistory(late, closedClock(clockTrade(902), now, 120)))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	if err = f.Accounts.PublishClock(ctx, late, closed, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if clockView(t, f).AutomaticTimeZone.EvidenceCount != 1 {
		t.Fatal("late historical response was treated as a new event")
	}
}

func TestTradeClockPublicationRechecksAccountAndKeepsSnapshotUnchanged(t *testing.T) {
	now := time.Now()
	f, account, trade := pendingClockFixture(t, &now)
	ctx := context.Background()
	job, err := f.Accounts.ClaimClock(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	label := "合成配置变更"
	if err := f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	closed, err := syncer.ConvertClock(job, clockHistory(job, closedClock(trade, now, 120)))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.PublishClock(ctx, job, closed, now.UnixMilli()); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("obsolete calibration configuration was published", err)
	}
	if err := f.Accounts.FailClock(ctx, job, accounts.Failure{Code: "configuration_changed", Canceled: true}); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || detail.Account.Snapshot == nil || detail.Account.Snapshot.Equity.String() != "1062.13" || len(detail.Positions) != 0 || !detail.Account.SyncEnabled {
		t.Fatal("calibration modified funds, positions or snapshot admission", err)
	}
}
