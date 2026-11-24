package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt5"
	"portexa/tests/testutil"
)

func mt5HistoryFixture(t *testing.T, now *time.Time, backupCount int) (testutil.Fixture, storage.Account) {
	t.Helper()
	ctx := t.Context()
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return *now }
	server, _, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{Protocol: "mt5", BrokerName: "Synthetic Broker",
		ServerName: "Synthetic-History", IPAddress: "203.0.113.9", Port: 2085, TimeZone: "Etc/GMT-2"})
	if err != nil {
		t.Fatal(err)
	}
	primary := reviewedCollectionPoint(t, f, server.ID, 2085)
	var backups []string
	for i := 0; i < backupCount; i++ {
		endpoint := reviewedCollectionPoint(t, f, server.ID, 2086+i)
		backups = append(backups, endpoint.ID)
	}
	if _, err = f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		Mode: "manual", TimeZone: "Etc/GMT-2", EffectiveDate: now.AddDate(0, 0, -1).Format(time.DateOnly),
	}); err != nil {
		t.Fatal(err)
	}
	account, err := f.Accounts.CreateAccount(ctx, accounts.AccountInput{ServerID: server.ID, EndpointID: primary.ID, BackupEndpointIDs: backups,
		Login: 7111, Label: "Synthetic History", Password: testutil.InvestorPassword, ConfirmedCurrency: "USD", ConfirmUnit: true, SyncEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
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
	return f, account
}

func TestMT5HistorySkipsFutureWindowsAndTracksContinuousCoverage(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 11, 15, 53, 0, 0, time.UTC)
	f, account := mt5HistoryFixture(t, &now, 0)
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
	stamp := now.UnixMilli()
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

func TestMT5HistoryTimeoutHandlingByPhase(t *testing.T) {
	for _, tc := range []struct {
		name          string
		timeout       time.Duration
		phases        []mt5.HistoryPhase
		waitForExpiry bool
		ports         []int
		runState      string
		coverageState string
	}{
		{name: "connect and handshake failover", phases: []mt5.HistoryPhase{mt5.HistoryConnect, mt5.HistoryHandshake},
			ports: []int{2085, 2086, 2087}, runState: "succeeded", coverageState: "complete"},
		{name: "deals split before switching", phases: []mt5.HistoryPhase{mt5.HistoryDeals},
			ports: []int{2085}, runState: "failed", coverageState: "split"},
		{name: "deals split at the task deadline", timeout: 500 * time.Millisecond, phases: []mt5.HistoryPhase{mt5.HistoryDeals}, waitForExpiry: true,
			ports: []int{2085}, runState: "failed", coverageState: "split"},
		{name: "not enough time for another setup", timeout: 3 * time.Second, phases: []mt5.HistoryPhase{mt5.HistoryHandshake},
			ports: []int{2085}, runState: "canceled", coverageState: "pending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			f, account := mt5HistoryFixture(t, &now, 2)
			if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
				return tx.Model(&account).Update("daily_sync_paused", true).Error
			}); err != nil {
				t.Fatal(err)
			}
			calls := make(chan int, 4)
			engine := syncer.New(f.Accounts, nil, syncer.Options{Timeout: tc.timeout,
				FetchMT5: func(context.Context, mt5.Config) (mt5.Result, error) { return testutil.MT5Snapshot(), nil },
				FetchMT5History: func(ctx context.Context, cfg mt5.Config, _ mt5.HistoryRange) (mt5.HistoryResult, error) {
					calls <- cfg.Port
					if index := cfg.Port - 2085; index < len(tc.phases) {
						if tc.waitForExpiry {
							<-ctx.Done()
						}
						return mt5.HistoryResult{}, &mt5.HistoryError{Phase: tc.phases[index], Err: context.DeadlineExceeded}
					}
					return mt5.HistoryResult{Account: mt5.Account{Currency: "USD"}}, nil
				}})
			startEngine(t, engine)
			var run storage.HistoryRun
			eventually(t, func() bool {
				return f.Store.Read.Order("started_at_ms").First(&run, "account_id=?", account.ID).Error == nil && run.State != "running"
			})
			var visited []int
			for len(calls) > 0 {
				visited = append(visited, <-calls)
			}
			if !slices.Equal(visited, tc.ports) || run.State != tc.runState {
				t.Fatalf("visited endpoints %v and finished as %s, expected %v and %s", visited, run.State, tc.ports, tc.runState)
			}
			var coverage storage.HistoryCoverage
			if err := f.Store.Read.First(&coverage, "id=?", run.CoverageID).Error; err != nil || coverage.State != tc.coverageState {
				t.Fatalf("coverage state %q, expected %q: %v", coverage.State, tc.coverageState, err)
			}
			if tc.coverageState == "pending" && (coverage.FailureCount != 0 || coverage.NextAttemptAtMS != now.Add(5*time.Second).UnixMilli()) {
				t.Fatalf("budget deferral was not queued as a retry: %+v", coverage)
			}
			if tc.coverageState == "split" {
				var children []storage.HistoryCoverage
				err := f.Store.Read.Where("parent_id=?", coverage.ID).Order("start_wall").Find(&children).Error
				next := now.Add(time.Second).UnixMilli()
				if err != nil || len(children) != 2 || children[0].NextAttemptAtMS != next || children[1].NextAttemptAtMS != next {
					t.Fatalf("split children were not queued promptly: %+v, %v", children, err)
				}
			}
		})
	}
}

func TestMT5HistoryResumesDeferredWindowBeforeOtherDueWindows(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 11, 15, 53, 0, 0, time.UTC)
	f, account := mt5HistoryFixture(t, &now, 0)
	job, err := f.Accounts.ClaimHistory(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailHistory(ctx, job, accounts.Failure{Code: "timeout", Operation: "history", Canceled: true}, false); err != nil {
		t.Fatal(err)
	}
	recent := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision,
		TimeRulesetID: job.Day.TimeRulesetID, StartWall: job.Day.CurrentWall - 600, EndWall: job.Day.End, State: "pending"}
	if err = f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error { return tx.Create(&recent).Error }); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.ClaimHistory(ctx, account.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatalf("retry started before its scheduled time: %v", err)
	}
	now = now.Add(5 * time.Second)
	resumed, err := f.Accounts.ClaimHistory(ctx, account.ID)
	if err != nil || resumed.Coverage.ID != job.Coverage.ID {
		t.Fatalf("deferred window lost its priority: got %s, expected %s: %v", resumed.Coverage.ID, job.Coverage.ID, err)
	}
}
