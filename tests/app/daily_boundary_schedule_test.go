package app_test

import (
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func TestDailyBoundaryScheduleEntersGuardAndKeepsHistoryBudget(t *testing.T) {
	zone := time.FixedZone("Beijing", 8*60*60)
	midnight := time.Date(2026, 1, 15, 0, 0, 0, 0, zone)
	for _, interval := range []time.Duration{30, 60, 120, 300} {
		interval *= time.Second
		start := midnight.Add(-2 * time.Minute)
		before := start.Add(-time.Second)
		next := accounts.NextSnapshotAt("synthetic-boundary", before, interval)
		if !next.Equal(start) {
			t.Fatalf("%v cycle sleeps through boundary guard: %v", interval, next)
		}
		at := midnight.Add(-32 * time.Second)
		if next := accounts.NextSnapshotAt("synthetic-boundary", at, interval); !next.Equal(midnight.Add(-3 * time.Second)) {
			t.Fatal("boundary phase missed the final seconds of the day", next)
		}
		for at := start; at.Before(midnight.Add(90 * time.Second)); at = at.Add(13 * time.Second) {
			next = accounts.NextSnapshotAt("synthetic-boundary", at, interval)
			gap := next.Sub(at)
			if gap < accounts.DailyAdmissionLead+2*time.Second || gap > 30*time.Second {
				t.Fatalf("boundary schedule loses cadence or history budget: at=%v gap=%v", at, gap)
			}
		}
	}
}

func TestDailyBoundaryPublicationRecordsCadenceAndRestoresSetting(t *testing.T) {
	zone := time.FixedZone("Beijing", 8*60*60)
	now := time.Date(2026, 1, 15, 23, 59, 28, 0, zone)
	f, a := dailyFixture(t, &now, "UTC")
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	for _, seconds := range []int{30, 300} {
		if seconds == 300 {
			now = now.Add(3 * time.Minute)
		}
		if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
			t.Fatal(err)
		}
		job, err := f.Accounts.Claim(t.Context(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, positions, err := syncer.Convert(job, testutil.Result("USC"), now.Add(-2*time.Second).UnixMilli(), now.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Accounts.PublishScheduled(t.Context(), job, snapshot, positions); err != nil {
			t.Fatal(err)
		}
		var saved storage.Snapshot
		if err := f.Store.Read.First(&saved, "id=?", snapshot.ID).Error; err != nil {
			t.Fatal(err)
		}
		view, err := f.Accounts.Detail(t.Context(), a.ID, "")
		if err != nil || saved.CollectionIntervalSeconds == nil || *saved.CollectionIntervalSeconds != seconds || view.Account.EffectiveCollectionIntervalSeconds != seconds {
			t.Fatal("boundary cadence disagrees with persisted sample or account view", seconds, saved.CollectionIntervalSeconds, view.Account.EffectiveCollectionIntervalSeconds, err)
		}
		want := accounts.NextSnapshotAt(a.ID, now, time.Duration(seconds)*time.Second).UnixMilli()
		if view.Account.NextSyncAtMS != want {
			t.Fatal("publication ignored boundary schedule", view.Account.NextSyncAtMS, want)
		}
	}
	settings, err := f.Accounts.CollectionSettings(t.Context())
	if err != nil || settings.SnapshotIntervalSeconds != 300 {
		t.Fatal("temporary boundary cadence overwrote configured interval", settings, err)
	}
}
