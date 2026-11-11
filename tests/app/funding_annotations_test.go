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

func TestDINAndDOUTProtocolCommentsIdentifyCashFlows(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		comment  string
		amount   float64
		command  int32
		category string
	}{
		{"D(DIN100000000000000001)", 100, 6, "deposit"},
		{"W(DOUT100000000000000002)", -25, 6, "withdrawal"},
		{" d(din100000000000000003) ", 30, 6, "deposit"},
		{"w(dout100000000000000004)", -5, 6, "withdrawal"},
		{"D(DIN100000000000000005)", -10, 6, ""},
		{"W(DOUT100000000000000006)", 10, 6, ""},
		{"D(DIN100000000000000007)", 500, 7, ""},
		{"D(DOUT100000000000000008)", 10, 6, ""},
		{"W(DIN100000000000000009)", -10, 6, ""},
		{"DIN100000000000000010", 10, 6, ""},
		{"D(DIN100000000000000011", 10, 6, ""},
		{"Rebate", 4, 6, "rebate"},
	}
	trades := make([]mt4.Trade, len(cases))
	for i, test := range cases {
		trades[i] = mt4.Trade{Ticket: int32(100 + i), Command: test.command, Comment: test.comment,
			OpenTime: job.Coverage.StartWall + uint32(i) + 1, Profit: test.amount}
	}
	publishHistory(t, f, job, trades, now)
	view := fundingView(t, f, account.ID)
	if view.Deposits.String() != "130" || view.Withdrawals.String() != "30" || view.AutomaticCount != 5 || view.UnclassifiedCount != 6 {
		t.Fatalf("broker cash-flow annotations were not recognized: %+v", view)
	}
	page, err := f.Accounts.Funds(t.Context(), account.ID, "", 100, false)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[int32]accounts.FundEntry{}
	for _, entry := range page.Items {
		entries[entry.Ticket] = entry
	}
	for i, test := range cases {
		entry := entries[int32(100+i)]
		if test.category == "" {
			if entry.Classification != nil {
				t.Fatal("ambiguous or credit record classified", i)
			}
			continue
		}
		if entry.Classification == nil || entry.Classification.Category != test.category || entry.Classification.Source != "automatic" {
			t.Fatal("wrong automatic category for synthetic annotation", i)
		}
	}
}

func TestFundingRuleUpgradeReevaluatesCachedAnnotationsWithoutRefetch(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades := []mt4.Trade{
		{Ticket: 101, Command: 6, OpenTime: job.Coverage.StartWall + 1, Profit: 100, Comment: "D(DIN100000000000000001)"},
		{Ticket: 102, Command: 6, OpenTime: job.Coverage.StartWall + 2, Profit: -25, Comment: "W(DOUT100000000000000002)"},
		{Ticket: 103, Command: 6, OpenTime: job.Coverage.StartWall + 3, Profit: 3, Comment: "Operation 42"},
		{Ticket: 104, Command: 6, OpenTime: job.Coverage.StartWall + 4, Profit: 2, Comment: "Rebate"},
		{Ticket: 105, Command: 6, OpenTime: job.Coverage.StartWall + 5, Profit: 30, Comment: "D(DIN100000000000000005)"},
		{Ticket: 106, Command: 6, OpenTime: job.Coverage.StartWall + 6, Profit: 50, Comment: "Deposit"},
		{Ticket: 107, Command: 7, OpenTime: job.Coverage.StartWall + 7, Profit: 500, Comment: "D(DIN100000000000000007)"},
	}
	// Put the only unknown record behind more than one bounded query batch.
	for i := 0; i < 60; i++ {
		trades = append(trades, mt4.Trade{Ticket: int32(200 + i), Command: 6, OpenTime: job.Coverage.StartWall + uint32(100+i), Profit: 1, Comment: "Deposit"})
	}
	orders, err := syncer.ConvertHistory(job, historyResult(job, trades))
	if err != nil {
		t.Fatal(err)
	}
	heads := make([]storage.HistoryOrderHead, len(orders))
	for i := range orders {
		order := &orders[i]
		order.Revision = 1
		order.AutomaticRuleVersion = "balance-comment-v1"
		switch order.Comment {
		case "Deposit":
			order.AutomaticCategory, order.AutomaticReason = "deposit", "备注含入金标识"
		case "Rebate":
			order.AutomaticCategory, order.AutomaticReason = "rebate", "备注含返佣标识"
		}
		heads[i] = storage.HistoryOrderHead{AccountID: account.ID, Ticket: order.Ticket, Revision: 1}
	}
	if err = f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&orders, 100).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(&heads, 100).Error; err != nil {
			return err
		}
		if err := tx.Model(&job.Run).Updates(map[string]any{"state": "succeeded", "finished_at_ms": now.UnixMilli()}).Error; err != nil {
			return err
		}
		return tx.Model(&job.Coverage).Updates(map[string]any{"state": "complete", "checked_at_ms": now.UnixMilli()}).Error
	}); err != nil {
		t.Fatal(err)
	}
	classify(t, f, account.ID, 105, 1, 0, "rebate")
	view := fundingView(t, f, account.ID)
	if view.Deposits.String() != "210" || view.Withdrawals.String() != "25" || view.AutomaticCount != 64 || view.ManualCount != 1 || view.UnclassifiedCount != 1 {
		t.Fatalf("cached rule results were not reevaluated consistently: %+v", view)
	}
	page, err := f.Accounts.Funds(t.Context(), account.ID, "", 1, true)
	if err != nil || len(page.Items) != 1 || page.Items[0].Ticket != 103 || page.NextCursor != "" {
		t.Fatal("exception pagination retained newly recognized cash flows or lost the unknown row", page, err)
	}
	page, err = f.Accounts.Funds(t.Context(), account.ID, "", 100, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if entry.Revision != 1 {
			t.Fatal("rule change invented a source revision")
		}
		if entry.Ticket == 101 || entry.Ticket == 102 {
			if entry.Classification == nil || entry.Classification.Source != "automatic" || entry.Classification.RuleVersion == "balance-comment-v1" {
				t.Fatal("list still exposes stale automatic classification", entry.Ticket)
			}
		}
		if entry.Ticket == 105 && (entry.Classification == nil || entry.Classification.Source != "manual" || entry.Classification.Category != "rebate") {
			t.Fatal("rule upgrade overwrote manual correction")
		}
	}
	var runs, revisions int64
	f.Store.Read.Model(&storage.HistoryRun{}).Count(&runs)
	f.Store.Read.Model(&storage.HistoryOrder{}).Count(&revisions)
	if runs != 1 || revisions != int64(len(trades)) {
		t.Fatal("read-only reclassification fetched history or rewrote source rows")
	}
}
