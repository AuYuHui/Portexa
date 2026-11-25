package app_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt5"
)

func TestMT5DailyDealReplyLimitDoesNotPauseCollection(t *testing.T) {
	now := time.Now()
	fixture, account := mt5HistoryFixture(t, &now, 0)
	engine := syncer.New(fixture.Accounts, nil, syncer.Options{
		FetchMT5History: func(context.Context, mt5.Config, mt5.HistoryRange) (mt5.HistoryResult, error) {
			return mt5.HistoryResult{}, &mt5.HistoryError{Phase: mt5.HistoryDeals, Err: mt5.ErrResourceLimit}
		},
	})
	startEngine(t, engine)
	eventually(t, func() bool {
		var current storage.Account
		return fixture.Store.Read.First(&current, "id=?", account.ID).Error == nil && current.DailyFailureCount == 1
	})
	var current storage.Account
	if err := fixture.Store.Read.First(&current, "id=?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !current.SyncEnabled || current.DailySyncPaused || current.NextDailySyncAtMS != now.Add(time.Minute).UnixMilli() {
		t.Fatal("a large reply should retain normal snapshot collection and retry the daily report")
	}
}

func TestRecoverResumesOnlyMT5JobsStoppedByOldReplyLimit(t *testing.T) {
	for _, phase := range []mt5.HistoryPhase{mt5.HistoryDeals, mt5.HistoryAccountSync} {
		t.Run(string(phase), func(t *testing.T) {
			ctx := t.Context()
			now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
			fixture, account := mt5HistoryFixture(t, &now, 0)
			daily, err := fixture.Accounts.ClaimDaily(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			errorCode := "resource_limit"
			if err := fixture.Accounts.FailDaily(ctx, daily, accounts.Failure{
				Code: errorCode, Operation: string(phase), Permanent: true,
			}); err != nil {
				t.Fatal(err)
			}
			reasons := []string{"history", string(mt5.HistoryDeals)}
			windows := make([]storage.HistoryCoverage, 0, len(reasons))
			err = fixture.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
				for i, reason := range reasons {
					start := daily.Day.Start + uint32(2*i)
					window := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID,
						UnitRevision: account.UnitRevision, TimeRulesetID: daily.Day.TimeRulesetID,
						StartWall: start, EndWall: start + 2, State: "blocked", NextAttemptAtMS: now.UnixMilli()}
					if err := tx.Create(&window).Error; err != nil {
						return err
					}
					windows = append(windows, window)
					run := storage.HistoryRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID,
						EndpointID: daily.Run.EndpointID, CoverageID: window.ID, ConfigVersion: account.ConfigVersion,
						UnitRevision: account.UnitRevision, NativeCurrency: "USD", State: "failed",
						StartedAtMS: now.UnixMilli(), ErrorCode: &errorCode, CompletionReason: &reason}
					if err := tx.Create(&run).Error; err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.Accounts.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			var current storage.Account
			if err := fixture.Store.Read.First(&current, "id=?", account.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.DailySyncPaused != (phase == mt5.HistoryAccountSync) {
				t.Fatalf("wrong paused state for daily phase %s", phase)
			}
			if phase == mt5.HistoryDeals && (current.DailyFailureCount != 0 || current.NextDailySyncAtMS != 0) {
				t.Fatal("daily report did not become eligible immediately")
			}
			for i, state := range []string{"pending", "blocked"} {
				var got storage.HistoryCoverage
				if err := fixture.Store.Read.First(&got, "id=?", windows[i].ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.State != state || i == 0 && got.NextAttemptAtMS != 0 {
					t.Fatalf("history window %d has state %s, expected %s", i, got.State, state)
				}
			}
		})
	}
}
