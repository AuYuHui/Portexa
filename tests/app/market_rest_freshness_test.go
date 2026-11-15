package app_test

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestMarketRestBackgroundFreshnessFollowsPlannedInterval(t *testing.T) {
	f, a, now, start := restFixture(t)
	*now = start
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	*now = start.Add(time.Minute)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	*now = now.Add(time.Second)
	analyticsHistory(t, f, a, *now, nil)
	if err := f.Accounts.RequestDaily(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimDaily(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: "USD", Balance: 1000, Equity: 1010}, HistoryRead: true, HistoryComplete: true,
		HistoryCovered: []mt4.HistoryRange{{Start: job.Day.Start, End: job.Day.End}}}
	report, err := syncer.ConvertDaily(job, result, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishDaily(t.Context(), job, report, now.Add(5*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var current storage.Account
	if err = f.Store.Read.First(&current, "id=?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.NextDailySyncAtMS != now.Add(15*time.Minute).UnixMilli() {
		t.Fatal("daily report did not throttle", current.NextDailySyncAtMS)
	}
	for _, offset := range []time.Duration{6 * time.Minute, 11 * time.Minute} {
		*now = start.Add(offset)
		restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	}
	*now = start.Add(12 * time.Minute)
	view, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Account.Stale || view.Account.Daily.Stale || view.Account.Funding.Stale {
		t.Fatal("planned fifteen-minute refresh marked stale", view.Account.Stale, view.Account.Daily.Stale, view.Account.Funding.Stale)
	}
	req := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: now.UnixMilli()}
	analysis, err := f.Accounts.Analytics(t.Context(), req)
	if err != nil || analysis.Stale {
		t.Fatal("planned analysis cutoff marked stale", analysis.Stale, err)
	}
	if analysis.Result.ToMS >= now.UnixMilli() {
		t.Fatal("freshness must not fabricate a newer ledger cutoff")
	}
	*now = start.Add(16 * time.Minute)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	*now = start.Add(18 * time.Minute)
	view, err = f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil || !view.Account.Daily.Stale || !view.Account.Funding.Stale {
		t.Fatal("missed background refresh never expires", err)
	}
	req.ToMS = now.UnixMilli()
	analysis, err = f.Accounts.Analytics(t.Context(), req)
	if err != nil || !analysis.Stale {
		t.Fatal("missed analysis cutoff never expires", analysis.Stale, err)
	}
}

func TestMarketRestNewHistoryExitsButPreservesSnapshotBackoff(t *testing.T) {
	for _, backoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "backoff"}[backoff], func(t *testing.T) {
			f, a, now, start := restFixture(t)
			*now = start
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			*now = start.Add(time.Minute)
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
			*now = now.Add(time.Second)
			future := now.Add(time.Hour).UnixMilli()
			if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
				updates := map[string]any{"next_sync_at_ms": future}
				if backoff {
					updates["failure_count"] = 2
				}
				return tx.Model(&a).Updates(updates).Error
			}); err != nil {
				t.Fatal(err)
			}
			analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 78303, Command: 0, Symbol: "XAUUSD", OpenTime: uint32(start.Unix()), CloseTime: uint32(start.Add(30 * time.Second).Unix())}})
			var current storage.Account
			if err := f.Store.Read.First(&current, "id=?", a.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.RestBlockedUntilMS <= now.UnixMilli() {
				t.Fatal("new trade failed to exit rest")
			}
			if backoff && current.NextSyncAtMS != future {
				t.Fatal("history erased snapshot backoff")
			}
			if !backoff && current.NextSyncAtMS > now.UnixMilli() {
				t.Fatal("new trade did not request fresh snapshot")
			}
		})
	}
}
