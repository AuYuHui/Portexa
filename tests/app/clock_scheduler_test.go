package app_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestTradeClockSharesHistoryPoolAndYieldsToSnapshot(t *testing.T) {
	now := time.Now().Add(-3 * time.Minute)
	f, first, _ := pendingClockFixture(t, &now)
	ctx := context.Background()
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id = ?", first.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id = ?", first.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	second := f.Account(t, server, endpoint, 7952, "USC")
	clockSnapshot(t, f, second, now, []mt4.Trade{clockTrade(811)})
	now = now.Add(time.Minute)
	clockSnapshot(t, f, second, now, nil)
	// An unrelated daily report competes for the same two history slots.
	dailyServer, dailyEndpoint := f.Server(t, "Synthetic-SharedHistory")
	if _, err := f.Accounts.ConfirmServerTime(ctx, dailyServer.ID, accounts.ServerTimeInput{Mode: "manual", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	dailyAccount := f.Account(t, dailyServer, dailyEndpoint, 7953, "USC")
	clockSnapshot(t, f, dailyAccount, now, nil)
	f.Accounts.Now = time.Now
	ids := map[uint32]string{first.Login: first.ID, second.Login: second.ID}
	var mu sync.Mutex
	active := map[uint32]int{}
	historyActive, historyMaximum, accountMaximum := 0, 0, 0
	var clockCalls, snapshotCalls atomic.Int32
	entered := make(chan uint32, 4)
	release := make(chan struct{})
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		mu.Lock()
		active[config.Login]++
		accountMaximum = max(accountMaximum, active[config.Login])
		if options.History != nil {
			historyActive++
			historyMaximum = max(historyMaximum, historyActive)
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active[config.Login]--
			if options.History != nil {
				historyActive--
			}
			mu.Unlock()
		}()
		result := testutil.Result("USC")
		result.Trades = nil
		if options.History == nil {
			snapshotCalls.Add(1)
			return result, nil
		}
		if config.Login != dailyAccount.Login {
			clockCalls.Add(1)
			entered <- config.Login
		}
		select {
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		case <-release:
			result.HistoryRead, result.HistoryComplete = true, true
			result.HistoryCovered = []mt4.HistoryRange{*options.History}
			return result, nil
		}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{Workers: 1, Interval: time.Hour})
	startEngine(t, e)
	eventually(t, func() bool { return clockCalls.Load() == 2 })
	login := <-entered
	if err := f.Accounts.RequestSync(ctx, ids[login]); err != nil {
		t.Fatal(err)
	}
	e.Wake()
	eventually(t, func() bool { return snapshotCalls.Load() == 1 })
	eventually(t, func() bool {
		var count int64
		f.Store.Read.Model(&storage.ClockRun{}).Where("account_id = ? AND state = 'canceled' AND error_code = 'snapshot_deadline'", ids[login]).Count(&count)
		return count == 1
	})
	close(release)
	eventually(t, func() bool {
		var count int64
		f.Store.Read.Model(&storage.DailyReport{}).Where("account_id = ? AND state = 'succeeded'", dailyAccount.ID).Count(&count)
		return count == 1
	})
	mu.Lock()
	defer mu.Unlock()
	if historyMaximum != 2 || accountMaximum != 1 {
		t.Fatal("clock calibration broke shared history limits", historyMaximum, accountMaximum)
	}
}

func TestTradeClockClaimsAreDurablyExclusiveAndRecoverable(t *testing.T) {
	now := time.Now()
	f, account, _ := pendingClockFixture(t, &now)
	ctx := context.Background()
	if _, err := f.Accounts.ConfirmServerTime(ctx, account.ServerID, accounts.ServerTimeInput{Mode: "manual", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	clock, err := f.Accounts.ClaimClock(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.ClaimDaily(ctx, account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("daily claim overlapped calibration", err)
	}
	if err := f.Accounts.RequestSync(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.Claim(ctx, account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("snapshot claim overlapped calibration", err)
	}
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Create(&storage.SyncRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID,
			EndpointID: account.PinnedEndpointID, CredentialVersion: 1, ConfigVersion: 1, Kind: "snapshot", State: "running"}).Error
	}); err == nil {
		t.Fatal("SQL allowed a snapshot during calibration")
	}
	if err := f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	var run storage.ClockRun
	if err := f.Store.Read.First(&run, "id = ?", clock.Run.ID).Error; err != nil || run.State != "interrupted" {
		t.Fatal("restart retained a calibration lock", err)
	}
	if _, err := f.Accounts.Claim(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		copy := clock.Run
		copy.ID = storage.NewID()
		return tx.Create(&copy).Error
	}); err == nil {
		t.Fatal("SQL allowed calibration during snapshot")
	}
}

func TestTradeClockFailureDoesNotPauseSnapshotOrDaily(t *testing.T) {
	now := time.Now().Add(-2 * time.Minute)
	f, account, _ := pendingClockFixture(t, &now)
	f.Accounts.Now = time.Now
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		if options.History == nil {
			t.Error("fixture should only fetch calibration history")
		}
		return mt4.Result{}, &mt4.Error{Code: mt4.ResourceLimit, Operation: "history"}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{})
	startEngine(t, e)
	eventually(t, func() bool {
		var current storage.Account
		f.Store.Read.First(&current, "id = ?", account.ID)
		return current.ClockSyncPaused && current.SyncEnabled && !current.DailySyncPaused && current.FailureCount == 0 && current.DailyFailureCount == 0
	})
}
