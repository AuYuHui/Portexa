package app_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func dailyFixture(t *testing.T, now *time.Time, zone string) (testutil.Fixture, storage.Account) {
	t.Helper()
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return *now }
	server, endpoint := f.Server(t, "Synthetic-Daily")
	if zone != "" {
		if _, err := f.Accounts.ConfirmServerTime(context.Background(), server.ID, accounts.ServerTimeInput{
			TimeZone: zone, EvidenceRef: "synthetic-server-clock", Confirm: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	a := f.Account(t, server, endpoint, 7101, "USC")
	job, err := f.Accounts.Claim(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, positions, err := syncer.Convert(job, testutil.Result("USC"), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.Publish(context.Background(), job, snapshot, positions, now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return f, a
}

func dailyResult(job accounts.DailyJob) mt4.Result {
	result := testutil.Result("USC")
	result.HistoryRead, result.HistoryComplete = true, true
	result.HistoryCovered = []mt4.HistoryRange{{Start: job.Day.Start, End: job.Day.End}}
	return result
}

func TestDailyProfitUsesCloseDateFeesAndNativeCurrency(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "Europe/Helsinki")
	ctx := context.Background()
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := dailyResult(job)
	result.History = []mt4.Trade{
		{Ticket: 1, Command: 0, OpenTime: job.Day.Start - 100, CloseTime: job.Day.Start, Profit: 100.15, Commission: -0.12, Swap: -0.03, Taxes: -0.01},
		{Ticket: 2, Command: 1, CloseTime: job.Day.End - 1, Profit: -40.01, Commission: -0.11, Swap: -0.12, Taxes: -0.01},
		{Ticket: 3, Command: 0, CloseTime: job.Day.Start - 1, Profit: 999},
		{Ticket: 4, Command: 1, CloseTime: job.Day.End, Profit: 999},
		{Ticket: 5, Command: 2, CloseTime: job.Day.Start + 1, Profit: 333},
		{Ticket: 6, Command: 6, OpenTime: job.Day.Start + 5, Profit: 200.01},
		{Ticket: 7, Command: 6, CloseTime: job.Day.Start + 8, Profit: -30.02},
		{Ticket: 8, Command: 7, OpenTime: job.Day.Start + 7, Profit: 50},
	}
	report, err := syncer.ConvertDaily(job, result, now.Add(time.Second).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if report.NativeCurrency != "USC" || report.NetProfit.String() != "59.74" || report.BalanceAdjustment.String() != "169.99" ||
		report.BalanceChange.String() != "229.73" || report.CreditChange.String() != "50" || report.ClosedTrades != 2 {
		t.Fatalf("wrong daily statistics: %+v", report)
	}
	if err = f.Accounts.PublishDaily(ctx, job, report, now.Add(5*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Report == nil || detail.Account.Daily.Report.NetProfit.String() != "59.74" {
		t.Fatal("published report unavailable", err)
	}
	if detail.Account.Snapshot.Equity.String() != "1062.13" || len(detail.Positions) != 1 {
		t.Fatal("history replaced the live snapshot or positions")
	}
}

func TestDailyRequiresCompleteCoverageAndAcceptsKnownEmptyDay(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimDaily(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*mt4.Result)
	}{
		{"partial", func(r *mt4.Result) { r.HistoryComplete = false }},
		{"unread", func(r *mt4.Result) { r.HistoryRead = false }},
		{"missing coverage", func(r *mt4.Result) { r.HistoryCovered = nil }},
		{"gap", func(r *mt4.Result) {
			r.HistoryCovered = []mt4.HistoryRange{{Start: job.Day.Start, End: job.Day.Start + 10}, {Start: job.Day.Start + 11, End: job.Day.End}}
		}},
		{"currency changed", func(r *mt4.Result) { r.Account.Currency = "USD" }},
		{"nonfinite fee", func(r *mt4.Result) {
			r.History = []mt4.Trade{{Ticket: 1, Command: 0, CloseTime: job.Day.Start + 1, Commission: math.NaN()}}
		}},
		{"duplicate ticket", func(r *mt4.Result) {
			r.History = []mt4.Trade{{Ticket: 1, Command: 6, OpenTime: job.Day.Start}, {Ticket: 1, Command: 6, OpenTime: job.Day.Start}}
		}},
		{"unclosed trade", func(r *mt4.Result) { r.History = []mt4.Trade{{Ticket: 1, Command: 0, OpenTime: job.Day.Start}} }},
		{"unknown command", func(r *mt4.Result) { r.History = []mt4.Trade{{Ticket: 1, Command: 8, CloseTime: job.Day.Start + 1}} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			r := dailyResult(job)
			test.edit(&r)
			if _, err := syncer.ConvertDaily(job, r, now.UnixMilli()); !errors.Is(err, syncer.ErrIncomplete) {
				t.Fatal("unusable history accepted", err)
			}
		})
	}
	r := dailyResult(job)
	r.HistoryCovered = []mt4.HistoryRange{{Start: job.Day.Start + 9, End: job.Day.End}, {Start: job.Day.Start, End: job.Day.Start + 10}}
	report, err := syncer.ConvertDaily(job, r, now.UnixMilli())
	if err != nil || report.NetProfit == nil || report.NetProfit.String() != "0" || report.BalanceChange.String() != "0" || report.ClosedTrades != 0 {
		t.Fatal("complete empty day is not a known zero", err)
	}
}

func TestDailyFailureKeepsSuccessButRolloverAndUnitChangeDoNotReuseIt(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := dailyResult(job)
	r.History = []mt4.Trade{{Ticket: 1, Command: 0, CloseTime: job.Day.Start + 5, Profit: 12.34}}
	report, err := syncer.ConvertDaily(job, r, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishDaily(ctx, job, report, now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err = f.Accounts.RequestDaily(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailDaily(ctx, job, accounts.Failure{Code: "connection", Operation: "history"}); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.State != "failed" || detail.Account.Daily.Report == nil || detail.Account.Daily.Report.NetProfit.String() != "12.34" {
		t.Fatal("failure cleared a valid report", err)
	}
	var account storage.Account
	if err = f.Store.Read.First(&account, "id = ?", a.ID).Error; err != nil || account.DailyFailureCount != 1 || account.FailureCount != 0 || account.NextSyncAtMS != now.Add(time.Hour-time.Second).UnixMilli() {
		t.Fatal("daily failure changed snapshot scheduling", err)
	}
	label := "合成账户新名称"
	if err = f.Accounts.UpdateAccount(ctx, a.ID, accounts.AccountPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	detail, err = f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Report == nil {
		t.Fatal("label edit erased known daily money", err)
	}
	currency, confirmed := "USD", true
	if err = f.Accounts.UpdateAccount(ctx, a.ID, accounts.AccountPatch{ConfirmedCurrency: &currency, ConfirmUnit: &confirmed}); err != nil {
		t.Fatal(err)
	}
	detail, err = f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Report != nil {
		t.Fatal("old unit reused", err)
	}
	now = now.AddDate(0, 0, 1)
	detail, err = f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Date != "2026-11-12" || detail.Account.Daily.Report != nil {
		t.Fatal("yesterday reused as today", err)
	}
}

func TestDailyConfigurationChangeRejectsAnInFlightResult(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	label := "配置已修改的合成账户"
	if err = f.Accounts.UpdateAccount(ctx, a.ID, accounts.AccountPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	report, err := syncer.ConvertDaily(job, dailyResult(job), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishDaily(ctx, job, report, now.Add(time.Minute).UnixMilli()); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("old account configuration published", err)
	}
	if err = f.Accounts.FailDaily(ctx, job, accounts.Failure{Canceled: true, Code: "configuration_changed"}); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Report != nil {
		t.Fatal("canceled result became visible", err)
	}
}

func TestDailyLabelEditDoesNotResumeRejectedHistory(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailDaily(ctx, job, accounts.Failure{Code: "server_rejected", Permanent: true}); err != nil {
		t.Fatal(err)
	}
	label := "仅修改合成名称"
	if err = f.Accounts.UpdateAccount(ctx, a.ID, accounts.AccountPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	var current storage.Account
	if err = f.Store.Read.First(&current, "id = ?", a.ID).Error; err != nil || !current.DailySyncPaused || !current.SyncEnabled {
		t.Fatal("label edit resumed rejected history or paused snapshots", err)
	}
	if err = f.Accounts.RequestDaily(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Read.First(&current, "id = ?", a.ID).Error; err != nil || current.DailySyncPaused {
		t.Fatal("explicit history refresh did not resume it", err)
	}
}
