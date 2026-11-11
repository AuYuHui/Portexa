package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestTwentyAccountsRemainBoundedAndIsolated(t *testing.T) {
	f := testutil.New(t, false) // Sync runs independently of browser authentication.
	server, endpoint := f.Server(t, "Synthetic-Capacity")
	for i := uint32(0); i < 20; i++ {
		f.Account(t, server, endpoint, 4000+i, "USD")
	}
	var mu sync.Mutex
	active, maximum := 0, 0
	calls := map[uint32]int{}
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		mu.Lock()
		calls[config.Login]++
		active++
		maximum = max(maximum, active)
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		if config.Login == 4000 {
			return mt4.Result{}, &mt4.Error{Code: mt4.ServerRejected, Operation: "authentication", Status: 7}
		}
		if config.Login == 4001 {
			<-ctx.Done()
			return mt4.Result{}, ctx.Err()
		}
		select {
		case <-time.After(30 * time.Millisecond):
			return testutil.Result("USD"), nil
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		}
	}
	engine := syncer.New(f.Accounts, fetch, syncer.Options{Timeout: 300 * time.Millisecond})
	startEngine(t, engine)
	eventually(t, func() bool {
		var n int64
		f.Store.Read.Model(&storage.SyncRun{}).Where("state IN ('succeeded','failed')").Count(&n)
		return n == 20
	})
	var snapshots, paused int64
	f.Store.Read.Model(&storage.Snapshot{}).Count(&snapshots)
	f.Store.Read.Model(&storage.Account{}).Where("sync_enabled = 0").Count(&paused)
	mu.Lock()
	defer mu.Unlock()
	if maximum > 10 || maximum < 2 || snapshots != 18 || paused != 1 {
		t.Fatal("capacity/isolation failed", maximum, snapshots, paused)
	}
	for login, n := range calls {
		if n != 1 {
			t.Fatal("duplicate Fetch", login, n)
		}
	}
	if len(calls) != 20 {
		t.Fatal("starved account")
	}
}
