package app_test

import (
	"context"
	"errors"
	"math"
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

func eventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func startEngine(t *testing.T, e *syncer.Engine) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("engine did not drain")
		}
	})
}

func TestSchedulerCoalescesAndLimitsConcurrency(t *testing.T) {
	f := testutil.New(t, false)
	server, endpoint := f.Server(t, "Synthetic-Scheduler")
	a := f.Account(t, server, endpoint, 2001, "USD")
	_ = f.Account(t, server, endpoint, 2002, "USD")
	var calls, active, max atomic.Int32
	release := make(chan struct{})
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		if options.History != nil || config.PasswordType != mt4.InvestorPassword {
			t.Error("unexpected SDK options")
		}
		calls.Add(1)
		n := active.Add(1)
		for old := max.Load(); n > old; old = max.Load() {
			if max.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(-1)
		select {
		case <-release:
			return testutil.Result("USD"), nil
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{Workers: 1, Interval: time.Hour})
	startEngine(t, e)
	eventually(t, func() bool { return calls.Load() == 1 })
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = f.Accounts.RequestSync(context.Background(), a.ID); e.Wake() }()
	}
	wg.Wait()
	if max.Load() != 1 || calls.Load() != 1 {
		t.Fatal("overlapping Fetch")
	}
	close(release)
	eventually(t, func() bool { var n int64; f.Store.Read.Model(&storage.Snapshot{}).Count(&n); return n == 2 })
	if max.Load() != 1 || calls.Load() != 2 {
		t.Fatal("coalescing failed", calls.Load(), max.Load())
	}
}

func TestConfigurationChangeCancelsPublicationAndGateNeverDials(t *testing.T) {
	f := testutil.New(t, false)
	server, endpoint := f.Server(t, "Synthetic-Cancel")
	a := f.Account(t, server, endpoint, 2003, "USD")
	var calls atomic.Int32
	entered := make(chan struct{})
	fetch := func(ctx context.Context, _ mt4.Config, _ mt4.Options) (mt4.Result, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return testutil.Result("USD"), nil
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{})
	startEngine(t, e)
	<-entered
	disabled := false
	if err := f.Accounts.UpdateAccount(context.Background(), a.ID, accounts.AccountPatch{SyncEnabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	e.Cancel(a.ID)
	eventually(t, func() bool {
		var n int64
		f.Store.Read.Model(&storage.SyncRun{}).Where("state = 'canceled'").Count(&n)
		return n == 1
	})
	var n int64
	f.Store.Read.Model(&storage.Snapshot{}).Count(&n)
	if n != 0 {
		t.Fatal("stale configuration published")
	}
	if err := f.Store.Write(context.Background(), storage.High, func(tx *gorm.DB) error {
		if err := tx.Model(&storage.Endpoint{}).Where("id = ?", endpoint.ID).Update("state", "retired").Error; err != nil {
			return err
		}
		return tx.Model(&storage.Account{}).Where("id = ?", a.ID).Updates(map[string]any{"sync_enabled": true, "next_sync_at_ms": 0}).Error
	}); err != nil {
		t.Fatal(err)
	}
	e.Wake()
	eventually(t, func() bool {
		var account storage.Account
		f.Store.Read.First(&account, "id = ?", a.ID)
		return !account.SyncEnabled
	})
	if calls.Load() != 1 {
		t.Fatal("untrusted gate dialed")
	}
}

func TestSnapshotAggregationAndIncompleteRejection(t *testing.T) {
	f := testutil.New(t, false)
	ctx := context.Background()
	server, endpoint := f.Server(t, "Synthetic-Money")
	for i, currency := range []string{"USD", "USC"} {
		a := f.Account(t, server, endpoint, uint32(3000+i), currency)
		job, err := f.Accounts.Claim(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UnixMilli()
		snapshot, positions, err := syncer.Convert(job, testutil.Result(currency), now-1000, now)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Accounts.Publish(ctx, job, snapshot, positions, now+60000); err != nil {
			t.Fatal(err)
		}
		partial := testutil.Result(currency)
		partial.AccountRead = false
		if _, _, err = syncer.Convert(job, partial, now, now); !errors.Is(err, syncer.ErrIncomplete) {
			t.Fatal("partial accepted")
		}
		partial = testutil.Result(currency)
		partial.Account.Equity = math.NaN()
		if _, _, err = syncer.Convert(job, partial, now, now); err == nil {
			t.Fatal("NaN accepted")
		}
	}
	view, err := f.Accounts.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.USDTotal.Equity.String() != "1072.7513" || view.USDTotal.FloatingNetProfit.String() != "12.2412" || !view.USDTotal.Complete {
		t.Fatal("wrong decimal total", view.USDTotal)
	}
	_ = f.Account(t, server, endpoint, 3009, "USD")
	view, err = f.Accounts.Overview(ctx)
	if err != nil || view.USDTotal.Complete || view.MissingCount != 1 || view.USDTotal.Equity.String() != "1072.7513" {
		t.Fatal("missing treated as zero", err)
	}
}

func TestPersistenceFailureStopsEngine(t *testing.T) {
	f := testutil.New(t, false)
	server, endpoint := f.Server(t, "Synthetic-Storage-Failure")
	f.Account(t, server, endpoint, 6001, "USD")
	entered, release := make(chan struct{}), make(chan struct{})
	fetch := func(ctx context.Context, _ mt4.Config, _ mt4.Options) (mt4.Result, error) {
		close(entered)
		<-release
		return testutil.Result("USD"), nil
	}
	engine := syncer.New(f.Accounts, fetch, syncer.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()
	<-entered
	if err := f.Store.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("persistence failure was hidden")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine remained stuck")
	}
}
