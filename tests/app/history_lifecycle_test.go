package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestHistoryUnknownEmptyAndOfflineGapRemainDistinct(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	if view := fundingView(t, f, a.ID); view.Deposits != nil || view.Withdrawals != nil || view.CoverageComplete {
		t.Fatal("unknown history is not zero", view)
	}
	for i := 0; i < 2; i++ {
		job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		publishHistory(t, f, job, nil, now)
		now = now.Add(time.Second)
	}
	view := fundingView(t, f, a.ID)
	if view.Deposits == nil || !view.Deposits.IsZero() || !view.Withdrawals.IsZero() || !view.CoverageComplete {
		t.Fatal("known empty history is not zero", view)
	}
	oldEnd := view.CoveredThroughWall
	now = now.AddDate(0, 0, 4)
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&a).Update("next_sync_at_ms", now.Add(time.Hour).UnixMilli()).Error
	}); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var gap storage.HistoryCoverage
	if err = f.Store.Read.First(&gap, "account_id=? AND start_wall=? AND end_wall=?", a.ID, oldEnd-1, job.Day.Start+1).Error; err != nil {
		t.Fatal("offline days silently omitted", err)
	}
	if view = fundingView(t, f, a.ID); view.CoverageComplete {
		t.Fatal("new day marked completely covered before fetching", view)
	}
}

func TestHistorySplitOverlapBlockedAndSnapshotYield(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailHistory(t.Context(), job, accounts.Failure{Code: "resource_limit", Operation: "history"}, true); err != nil {
		t.Fatal(err)
	}
	var children []storage.HistoryCoverage
	if err = f.Store.Read.Where("parent_id=?", job.Coverage.ID).Order("start_wall").Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].StartWall != job.Coverage.StartWall || children[1].EndWall != job.Coverage.EndWall || children[0].EndWall != children[1].StartWall+1 {
		t.Fatal("split dropped a boundary", children)
	}
	var account storage.Account
	if err = f.Store.Read.First(&account, "id=?", a.ID).Error; err != nil || account.HistorySyncPaused || account.FailureCount != 0 || !account.SyncEnabled {
		t.Fatal("split affected live snapshots", err)
	}
	now = now.Add(time.Second)
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailHistory(t.Context(), job, accounts.Failure{Code: "snapshot_deadline", Operation: "history", Canceled: true}, false); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err = f.Store.Read.Model(&storage.HistoryCoverage{}).Where("parent_id=?", job.Coverage.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("snapshot yield split a window", err)
	}
	now = now.Add(5 * time.Second)
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The minimum independently fetchable window must remain an explicit gap.
	job.Coverage.EndWall = job.Coverage.StartWall + 2
	if err = f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error { return tx.Model(&job.Coverage).Update("end_wall", job.Coverage.EndWall).Error }); err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailHistory(t.Context(), job, accounts.Failure{Code: "resource_limit", Operation: "history"}, true); err != nil {
		t.Fatal(err)
	}
	view := fundingView(t, f, a.ID)
	if view.State != "blocked" || view.BlockedWindows != 1 || view.CoverageComplete {
		t.Fatal("unsplittable gap hidden", view)
	}
}

func TestHistoryDurableExclusionRecoveryAndRestore(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	classify(t, f, a.ID, 6, 1, 0, "deposit")
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Accounts.ClaimDaily(t.Context(), a.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("daily overlapped history", err)
	}
	if err = f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Create(&storage.SyncRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID,
			CredentialVersion: 1, ConfigVersion: 1, Kind: "snapshot", State: "running", Attempt: 1}).Error
	}); err == nil {
		t.Fatal("database permitted overlapping fetch")
	}
	backup, destination := filepath.Join(t.TempDir(), "backup.sqlite"), filepath.Join(t.TempDir(), "restored.sqlite")
	if err = f.Store.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	if err = storage.RestoreNew(t.Context(), backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var run storage.HistoryRun
	if err = restored.Read.First(&run, "id=?", job.Run.ID).Error; err != nil || run.State != "interrupted" {
		t.Fatal("restore retained history lock", err)
	}
	var classification storage.CashFlowClassification
	if err = restored.Read.First(&classification, "account_id=? AND ticket=6", a.ID).Error; err != nil || classification.Category != "deposit" {
		t.Fatal("restore lost manual classification", err)
	}
	if err = f.Accounts.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Read.First(&run, "id=?", job.Run.ID).Error; err != nil || run.State != "interrupted" {
		t.Fatal("restart retained history lock", err)
	}
	if _, err = f.Accounts.ClaimHistory(t.Context(), a.ID); err != nil {
		t.Fatal("history could not resume", err)
	}
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "100.01" {
		t.Fatal("recovery erased known facts", got)
	}
}

func TestHistoryUnitChangeDoesNotReuseOldTotals(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	classify(t, f, a.ID, 6, 1, 0, "deposit")
	currency := "USD"
	if err := f.Accounts.UpdateAccount(context.Background(), a.ID, accounts.AccountPatch{ConfirmedCurrency: &currency}); err != nil {
		t.Fatal(err)
	}
	view := fundingView(t, f, a.ID)
	if view.Deposits != nil || view.CoverageComplete {
		t.Fatal("old unit reused in funding", view)
	}
	page, err := f.Accounts.Funds(t.Context(), a.ID, "", 50, false)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("old unit shown as new unit", page, err)
	}
	if _, err = f.Accounts.ClaimHistory(t.Context(), a.ID); !errors.Is(err, accounts.ErrPaused) {
		t.Fatal("history ran without a new unit snapshot", err)
	}
}

func TestHistoryPublicationBudgetHandlesFullBatchAndSourceRevisions(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades := make([]mt4.Trade, accounts.HistoryPublishLimit)
	for i := range trades {
		trades[i] = mt4.Trade{Ticket: int32(50000 + i), Command: 6, OpenTime: job.Coverage.StartWall + uint32(i) + 1, Profit: 1.01}
	}
	for pass := 0; pass < 2; pass++ {
		if pass > 0 {
			now = now.Add(time.Second)
			if err = f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
				t.Fatal(err)
			}
			job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
			if err != nil {
				t.Fatal(err)
			}
			for i := range trades {
				trades[i].Profit = 2.02
			}
		}
		orders, err := syncer.ConvertHistory(job, historyResult(job, trades))
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Accounts.PublishHistory(t.Context(), job, orders, now.UnixMilli()); err != nil {
			t.Fatal("bounded publication failed", pass, err)
		}
	}
	var count int64
	if err = f.Store.Read.Model(&storage.HistoryOrder{}).Where("account_id=?", a.ID).Count(&count).Error; err != nil || count != 2*accounts.HistoryPublishLimit {
		t.Fatal("source revisions lost", count, err)
	}
	view := fundingView(t, f, a.ID)
	if view.UnclassifiedCount != accounts.HistoryPublishLimit || view.Deposits == nil || !view.Deposits.IsZero() {
		t.Fatal("large unclassified batch invented deposits", view)
	}
}
