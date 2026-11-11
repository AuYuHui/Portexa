package app_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestBackfillSharesHistoryPoolAndYieldsWithoutSplitting(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	// Exercise backfill specifically, rather than the higher-priority daily task.
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&a).Updates(map[string]any{"daily_sync_paused": true, "next_sync_at_ms": now.Add(time.Hour).UnixMilli()}).Error
	}); err != nil {
		t.Fatal(err)
	}
	var historyCalls, snapshotCalls, active, maximum atomic.Int32
	entered := make(chan struct{}, 1)
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		current := active.Add(1)
		if current > maximum.Load() {
			maximum.Store(current)
		}
		defer active.Add(-1)
		if options.History == nil {
			snapshotCalls.Add(1)
			return historyResultForCurrency("USC"), nil
		}
		historyCalls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return mt4.Result{}, ctx.Err()
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{Workers: 1, Interval: time.Hour})
	startEngine(t, e)
	eventually(t, func() bool { return historyCalls.Load() == 1 })
	<-entered
	if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	e.Wake()
	eventually(t, func() bool { return snapshotCalls.Load() == 1 })
	eventually(t, func() bool {
		var count int64
		f.Store.Read.Model(&storage.HistoryRun{}).Where("account_id=? AND state='canceled' AND error_code='snapshot_deadline'", a.ID).Count(&count)
		return count == 1
	})
	var split int64
	if err := f.Store.Read.Model(&storage.HistoryCoverage{}).Where("account_id=? AND state='split'", a.ID).Count(&split).Error; err != nil || split != 0 || maximum.Load() != 1 {
		t.Fatal("backfill did not yield exclusively", split, maximum.Load(), err)
	}
}

func historyResultForCurrency(currency string) mt4.Result {
	return mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: currency, Balance: 100, Equity: 100}}
}

func TestHistoryOnlySplitsLimitsInTheHistoryPhase(t *testing.T) {
	for _, phase := range []string{"history", "tables"} {
		t.Run(phase, func(t *testing.T) {
			now := time.Now()
			f, a := dailyFixture(t, &now, "UTC")
			if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error { return tx.Model(&a).Update("daily_sync_paused", true).Error }); err != nil {
				t.Fatal(err)
			}
			fetch := func(ctx context.Context, _ mt4.Config, _ mt4.Options) (mt4.Result, error) {
				return mt4.Result{}, &mt4.Error{Code: mt4.ResourceLimit, Operation: phase}
			}
			e := syncer.New(f.Accounts, fetch, syncer.Options{})
			startEngine(t, e)
			eventually(t, func() bool {
				var account storage.Account
				var split int64
				f.Store.Read.First(&account, "id=?", a.ID)
				f.Store.Read.Model(&storage.HistoryCoverage{}).Where("account_id=? AND state='split'", a.ID).Count(&split)
				if phase == "history" {
					return split == 1 && !account.HistorySyncPaused
				}
				return account.HistorySyncPaused && split == 0
			})
		})
	}
}
