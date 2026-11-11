package app_test

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestFundingAutomaticallyClassifiesExplicitCommentsAndOnlyRequestsExceptions(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		comment  string
		amount   float64
		category string
	}{
		{"Deposit #synthetic", 100, "deposit"}, {"Withdrawal", -20, "withdrawal"},
		{"入金", 50, "deposit"}, {"提现", -10, "withdrawal"},
		{"Rebate", 5, "rebate"}, {"Cashback", 3, "rebate"}, {"手续费", -2, "fee"},
		{"Internal transfer", 100, "transfer_in"}, {"Transfer out", -50, "transfer_out"},
		{"Profit sharing", 7, "profit_share_income"}, {"Profit share", -3, "profit_share_expense"},
		{"Bonus", 10, "adjustment"},
		{"", 99, ""}, {"Operation 42", 23, ""}, {"Deposit bonus", 25, ""},
		{"Withdrawal fee", -3, ""}, {"Deposit reversal", -100, ""}, {"Cancelled deposit", 100, ""},
		{"Withdrawal", 50, ""}, {"Deposit", -50, ""}, {"存款利息", 1, ""}, {"Deposit refund", 100, ""},
	}
	trades := make([]mt4.Trade, 0, len(cases)+2)
	for i, test := range cases {
		trades = append(trades, mt4.Trade{Ticket: int32(200 + i), Command: 6, OpenTime: job.Coverage.StartWall + uint32(i) + 1, Profit: test.amount, Comment: test.comment})
	}
	trades = append(trades, mt4.Trade{Ticket: 500, Command: 7, OpenTime: job.Coverage.StartWall + 100, Profit: 500, Comment: "Deposit"})
	trades = append(trades, mt4.Trade{Ticket: 501, Command: 6, OpenTime: job.Coverage.StartWall + 101, Profit: 100, Commission: -1, Comment: "Deposit"})
	publishHistory(t, f, job, trades, now)
	view := fundingView(t, f, a.ID)
	if view.Deposits.String() != "150" || view.Withdrawals.String() != "30" || view.AutomaticCount != 12 || view.ManualCount != 0 || view.UnclassifiedCount != 11 {
		t.Fatalf("incorrect automatic totals: %+v", view)
	}
	page, err := f.Accounts.Funds(t.Context(), a.ID, "", 100, false)
	if err != nil {
		t.Fatal(err)
	}
	byTicket := map[int32]accounts.FundEntry{}
	for _, entry := range page.Items {
		byTicket[entry.Ticket] = entry
	}
	for i, test := range cases {
		entry := byTicket[int32(200+i)]
		if test.category == "" {
			if entry.Classification != nil {
				t.Fatal("ambiguous comment guessed", test.comment)
			}
			continue
		}
		if entry.Comment != test.comment || entry.Classification == nil || entry.Classification.Source != "automatic" || entry.Classification.RuleVersion != accounts.FundingRuleVersion || entry.Classification.Category != test.category {
			t.Fatal("automatic classification missing or wrong", test.comment, entry)
		}
	}
	unknown, err := f.Accounts.Funds(t.Context(), a.ID, "", 100, true)
	if err != nil || len(unknown.Items) != 11 {
		t.Fatal("exception filter includes automatic or credit records", len(unknown.Items), err)
	}
	// Correcting an automatic result requires no invented classification ledger
	// revision; subsequent identical backfills preserve the explicit correction.
	classify(t, f, a.ID, 200, 1, 0, "rebate")
	view = fundingView(t, f, a.ID)
	if view.Deposits.String() != "50" || view.AutomaticCount != 11 || view.ManualCount != 1 {
		t.Fatal("manual correction did not override automatic result", view)
	}
	if err = f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, trades, now)
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "50" {
		t.Fatal("repeat fetch overwrote correction", got)
	}
	// A source annotation correction is a new immutable revision and causes
	// automatic evaluation of that source; the old correction is not carried over.
	now = now.Add(time.Second)
	if err = f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades[0].Comment = "Deposit corrected"
	publishHistory(t, f, job, trades, now)
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "150" {
		t.Fatal("new source was not automatically re-evaluated", got)
	}
}

func TestUnpublishedHistoryStagingStaysInvisibleAndIsCleaned(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	order := storage.HistoryOrder{AccountID: a.ID, Ticket: 9090, Revision: 1, SourceRunID: job.Run.ID, SourceHash: make([]byte, 32),
		UnitRevision: a.UnitRevision, NativeCurrency: "USC", Command: 6, Comment: "Deposit", AutomaticCategory: "deposit"}
	if err = f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error { return tx.Create(&order).Error }); err != nil {
		t.Fatal(err)
	}
	if view := fundingView(t, f, a.ID); view.Deposits != nil || view.UnclassifiedCount != 0 {
		t.Fatal("staged history became visible", view)
	}
	page, err := f.Accounts.Funds(t.Context(), a.ID, "", 50, false)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("staged history appeared in paging", page, err)
	}
	if err = f.Accounts.FailHistory(t.Context(), job, accounts.Failure{Code: "connection", Operation: "history"}, false); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err = f.Store.Read.Model(&storage.HistoryOrder{}).Where("source_run_id=?", job.Run.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("unpublished staging not discarded", err, count)
	}
}

func TestLegacySourceHashUpgradeKeepsExistingManualCorrection(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades := []mt4.Trade{{Ticket: 777, Command: 6, OpenTime: job.Coverage.StartWall + 1, Profit: 100}}
	orders, err := syncer.ConvertHistory(job, historyResult(job, trades))
	if err != nil {
		t.Fatal(err)
	}
	legacy := orders[0]
	legacy.Revision = 1
	legacy.SourceHash = make([]byte, 32) // Represents a pre-comment digest format.
	if err = f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		if err := tx.Create(&legacy).Error; err != nil {
			return err
		}
		if err := tx.Create(&storage.HistoryOrderHead{AccountID: a.ID, Ticket: legacy.Ticket, Revision: 1}).Error; err != nil {
			return err
		}
		if err := tx.Model(&job.Run).Updates(map[string]any{"state": "succeeded", "finished_at_ms": now.UnixMilli()}).Error; err != nil {
			return err
		}
		return tx.Model(&job.Coverage).Updates(map[string]any{"state": "complete", "checked_at_ms": now.UnixMilli()}).Error
	}); err != nil {
		t.Fatal(err)
	}
	classify(t, f, a.ID, 777, 1, 0, "deposit")
	if err = f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, trades, now)
	page, err := f.Accounts.Funds(t.Context(), a.ID, "", 50, false)
	if err != nil || len(page.Items) != 1 || page.Items[0].Revision != 1 || page.Items[0].NeedsReview || page.Items[0].Classification.Source != "manual" || fundingView(t, f, a.ID).Deposits.String() != "100" {
		t.Fatal("hash upgrade invalidated unchanged manual classification", page, err)
	}
}
