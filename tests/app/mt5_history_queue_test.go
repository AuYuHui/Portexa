package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func TestMT5HistorySkipsFutureWindowsAndTracksContinuousCoverage(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 11, 15, 53, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{Protocol: "mt5", BrokerName: "Synthetic Broker",
		ServerName: "Synthetic-History", IPAddress: "203.0.113.9", Port: 2085, TimeZone: "Etc/GMT-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.Review(ctx, endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic verification", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		Mode: "manual", TimeZone: "Etc/GMT-2", EffectiveDate: "2026-11-10",
	}); err != nil {
		t.Fatal(err)
	}
	account := f.Account(t, server, endpoint, 7111, "USD")
	snapshotJob, err := f.Accounts.Claim(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	stamp := now.UnixMilli()
	snapshot := storage.Snapshot{ID: storage.NewID(), AccountID: account.ID, SyncRunID: snapshotJob.Run.ID,
		ObservedFromMS: stamp, ObservedToMS: stamp, NativeCurrency: "USD", UnitRevision: account.UnitRevision, USDScale: account.USDScale}
	if err = f.Accounts.Publish(ctx, snapshotJob, snapshot, nil, now.Add(2*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	root, err := f.Accounts.ClaimHistory(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if root.HistoryEndWall() != uint32(now.Add(2*time.Hour).Unix()) || root.Coverage.EndWall <= root.HistoryEndWall() {
		t.Fatalf("MT5 query extended beyond the current server time: %+v, requested end %d", root.Coverage, root.HistoryEndWall())
	}
	if err = f.Accounts.FailHistory(ctx, root, accounts.Failure{Code: "resource_limit", Operation: "history"}, true); err != nil {
		t.Fatal(err)
	}
	var children []storage.HistoryCoverage
	if err = f.Store.Read.Where("parent_id=?", root.Coverage.ID).Order("start_wall").Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	mid := root.Coverage.StartWall + (root.HistoryEndWall()-root.Coverage.StartWall)/2
	if len(children) != 2 || children[1].StartWall != mid || children[1].EndWall != root.Coverage.EndWall {
		t.Fatalf("split was driven by the unqueried future range: %+v", children)
	}
	// Reproduce a previously split day with an overdue leaf in the future.
	day := root.Day
	window := func(start, end uint32) storage.HistoryCoverage {
		return storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision,
			TimeRulesetID: day.TimeRulesetID, ParentID: &root.Coverage.ID, StartWall: start, EndWall: end, State: "pending"}
	}
	older := window(day.Start, day.Start+16*3600+1)
	latest := window(older.EndWall-1, day.Start+18*3600+1)
	future := window(latest.EndWall-1, day.End)
	future.CheckedAtMS = &stamp // Pre-upgrade check that extended into the future.
	if err = f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := tx.Where("parent_id=?", root.Coverage.ID).Delete(&storage.HistoryCoverage{}).Error; err != nil {
			return err
		}
		return tx.Create(&[]storage.HistoryCoverage{older, latest, future}).Error
	}); err != nil {
		t.Fatal(err)
	}
	claim := func(expect storage.HistoryCoverage, refresh time.Duration) accounts.HistoryJob {
		t.Helper()
		now = now.Add(time.Second)
		job, err := f.Accounts.ClaimHistory(ctx, account.ID)
		if err != nil || job.Coverage.ID != expect.ID {
			t.Fatalf("claimed %q instead of %q: %v", job.Coverage.ID, expect.ID, err)
		}
		end := min(expect.EndWall, uint32(now.Add(2*time.Hour).Unix()))
		if job.HistoryEndWall() != end {
			t.Fatalf("requested end %d instead of %d", job.HistoryEndWall(), end)
		}
		if err := f.Accounts.PublishHistory(ctx, job, nil, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		var saved storage.HistoryCoverage
		if err := f.Store.Read.First(&saved, "id=?", expect.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.CheckedThroughWall == nil || *saved.CheckedThroughWall != end || saved.NextAttemptAtMS != now.Add(refresh).UnixMilli() {
			t.Fatalf("wrong coverage or recheck schedule: %+v", saved)
		}
		return job
	}
	recent := claim(latest, 5*time.Minute)
	if view := fundingView(t, f, account.ID); view.CoverageComplete || view.PendingWindows != 2 {
		t.Fatalf("missing earlier coverage or future window handled incorrectly: %+v", view)
	}
	claim(older, time.Hour)
	var backfill storage.HistoryCoverage
	if err = f.Store.Read.First(&backfill, "account_id=? AND start_wall=0", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	claim(backfill, 24*time.Hour)
	if view := fundingView(t, f, account.ID); !view.CoverageComplete || view.Stale || view.CoveredThroughWall != recent.HistoryEndWall() {
		t.Fatalf("recent continuous history should be fresh through the actual query end: %+v", view)
	}
	now = now.Add(time.Second)
	if _, err = f.Accounts.ClaimHistory(ctx, account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatalf("future leaf became eligible: %v", err)
	}
	now = now.Add(6 * time.Minute)
	if view := fundingView(t, f, account.ID); !view.CoverageComplete || view.Stale {
		t.Fatalf("history within the freshness threshold is still valid: %+v", view)
	}
	now = now.Add(5 * time.Minute)
	if view := fundingView(t, f, account.ID); view.CoverageComplete || !view.Stale || view.CoveredThroughWall != recent.HistoryEndWall() {
		t.Fatalf("elapsed time should reveal the uncovered tail without moving the coverage timestamp: %+v", view)
	}
}
