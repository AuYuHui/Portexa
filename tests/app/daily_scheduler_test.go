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
)

func TestDailyHistoryPoolIsSeparateAndYieldsToSnapshot(t *testing.T) {
	now := time.Now()
	f, first := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	ids := map[uint32]string{first.Login: first.ID}
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id = ?", first.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id = ?", first.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	for login := uint32(7102); login <= 7104; login++ {
		a := f.Account(t, server, endpoint, login, "USC")
		ids[login] = a.ID
		job, err := f.Accounts.Claim(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, positions, err := syncer.Convert(job, dailyResult(accounts.DailyJob{}), now.UnixMilli(), now.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Accounts.Publish(ctx, job, snapshot, positions, now.Add(time.Hour).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	active := map[uint32]int{}
	historyActive, historyMaximum, accountMaximum := 0, 0, 0
	var historyCalls, snapshotCalls atomic.Int32
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
		if options.History == nil {
			snapshotCalls.Add(1)
			return dailyResult(accounts.DailyJob{}), nil
		}
		historyCalls.Add(1)
		entered <- config.Login
		select {
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		case <-release:
			r := dailyResult(accounts.DailyJob{})
			r.HistoryCovered = []mt4.HistoryRange{*options.History}
			return r, nil
		}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{Workers: 1, Interval: time.Hour})
	startEngine(t, e)
	eventually(t, func() bool { return historyCalls.Load() == 2 })
	login := <-entered
	if err := f.Accounts.RequestSync(ctx, ids[login]); err != nil {
		t.Fatal(err)
	}
	e.Wake()
	eventually(t, func() bool { return snapshotCalls.Load() == 1 })
	eventually(t, func() bool {
		var count int64
		f.Store.Read.Model(&storage.DailyReport{}).Where("account_id = ? AND state = 'canceled' AND error_code = 'snapshot_deadline'", ids[login]).Count(&count)
		return count == 1
	})
	close(release)
	eventually(t, func() bool {
		var count int64
		f.Store.Read.Model(&storage.DailyReport{}).Where("state = 'succeeded'").Count(&count)
		return count >= 3
	})
	mu.Lock()
	defer mu.Unlock()
	if historyMaximum != 2 || accountMaximum != 1 {
		t.Fatal("fetch pools or account exclusion failed", historyMaximum, accountMaximum)
	}
}

func TestDailyAndSnapshotClaimsAreDurablyExclusive(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	daily, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.RequestSync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.Claim(ctx, a.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("snapshot overlapped history", err)
	}
	if err = f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Create(&storage.SyncRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID,
			CredentialVersion: 1, ConfigVersion: 1, Kind: "snapshot", State: "running", Attempt: 1}).Error
	}); err == nil {
		t.Fatal("database accepted an overlapping snapshot")
	}
	if err = f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.Claim(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Model(&storage.Account{}).Where("id = ?", a.ID).Update("next_sync_at_ms", now.Add(time.Hour).UnixMilli()).Error
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.ClaimDaily(ctx, a.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("history overlapped snapshot", err)
	}
	if err = f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		copy := daily.Run
		copy.ID = storage.NewID()
		return tx.Create(&copy).Error
	}); err == nil {
		t.Fatal("database accepted overlapping history")
	}
}

func TestDailyPermanentFailureDoesNotPauseSnapshots(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		if options.History == nil {
			t.Error("fixture should only fetch history")
		}
		return mt4.Result{}, &mt4.Error{Code: mt4.ServerRejected, Operation: "history", Status: 7}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{})
	startEngine(t, e)
	eventually(t, func() bool {
		var account storage.Account
		f.Store.Read.First(&account, "id = ?", a.ID)
		return account.DailySyncPaused && account.SyncEnabled && account.FailureCount == 0
	})
	detail, err := f.Accounts.Detail(context.Background(), a.ID, "")
	if err != nil || detail.Account.Daily.RawStatus == nil || *detail.Account.Daily.RawStatus != 7 {
		t.Fatal("history rejection lost its server status", err)
	}
}
