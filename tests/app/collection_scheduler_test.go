package app_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestRunningSnapshotUsesLatestCollectionSettings(t *testing.T) {
	f := testutil.New(t, false)
	server, endpoint := f.Server(t, "Synthetic-Collection-Inflight")
	a := f.Account(t, server, endpoint, 8910, "USD")
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fetch := func(ctx context.Context, _ mt4.Config, options mt4.Options) (mt4.Result, error) {
		if options.History != nil {
			t.Error("unexpected history fetch for an unconfirmed server clock")
		}
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return testutil.Result("USD"), nil
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		}
	}
	e := syncer.New(f.Accounts, fetch, syncer.Options{})
	startEngine(t, e)
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("snapshot did not start")
	}
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	close(release)
	eventually(t, func() bool {
		var current storage.Account
		return f.Store.Read.First(&current, "id = ?", a.ID).Error == nil && current.CurrentSnapshotID != nil
	})
	detail, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	want := accounts.NextSnapshotAt(a.ID, time.UnixMilli(detail.Account.Snapshot.ObservedToMS), 300*time.Second).UnixMilli()
	if detail.Account.NextSyncAtMS != want || calls.Load() != 1 {
		t.Fatal("in-flight snapshot restored the old cycle", detail.Account.NextSyncAtMS, want, calls.Load())
	}
}

func TestThirtySecondSnapshotsLeaveRoomForDailyAndBackfill(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	started := now
	// A slow snapshot used to leave at most 10 seconds of a 30-second phase,
	// which could never meet the existing 25-second history admission rule.
	now = now.Add(20 * time.Second)
	snapshot, positions, err := syncer.Convert(job, testutil.Result("USC"), started.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.PublishScheduled(t.Context(), job, snapshot, positions); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	due, err := f.Accounts.DailyDue(t.Context(), now.UnixMilli(), 10)
	if err != nil || len(due) != 1 || due[0] != a.ID {
		t.Fatal("30-second snapshots starve daily reports", due, err)
	}
	daily, err := f.Accounts.ClaimDaily(t.Context(), a.ID)
	if err != nil {
		t.Fatal("daily cannot enter the reserved window", err)
	}
	if time.UnixMilli(daily.Account.NextSyncAtMS).Add(-accounts.DailySnapshotGuard).Sub(now) < 20*time.Second {
		t.Fatal("daily task received less than its full fetch budget")
	}
	if err := f.Accounts.FailDaily(t.Context(), daily, accounts.Failure{Code: "synthetic_cancel", Canceled: true}); err != nil {
		t.Fatal(err)
	}
	due, err = f.Accounts.HistoryDue(t.Context(), now.UnixMilli(), 10)
	if err != nil || len(due) != 1 || due[0] != a.ID {
		t.Fatal("30-second snapshots starve backfill", due, err)
	}
	if _, err := f.Accounts.ClaimHistory(t.Context(), a.ID); err != nil {
		t.Fatal("backfill cannot enter the reserved window", err)
	}
}

func TestLongCollectionCycleAdjustsSnapshotStaleness(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	detail, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil || detail.Account.Stale {
		t.Fatal("snapshot expires before its next configured collection", err)
	}
	overview, err := f.Accounts.Overview(t.Context())
	if err != nil || overview.StaleCount != 0 {
		t.Fatal("overview uses a different stale threshold", err)
	}
	now = now.Add(12 * time.Minute)
	detail, err = f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil || !detail.Account.Stale {
		t.Fatal("snapshot never becomes stale", err)
	}
}
