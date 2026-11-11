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

func TestDPrimeFundingSeparatesExternalFlowsFromCopyTradingProfitShares(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		comment  string
		amount   float64
		category string
	}{
		{"dbu #IT100000000000000001", 100, "deposit"},
		{"wbu #IT100000000000000002", -25, "withdrawal"},
		{"-PF|1000001|&101", -7, "profit_share_expense"},
		{" \tDBU #it100000000000000003 \t", 30, "deposit"},
		{" WBU #it100000000000000004 ", -5, "withdrawal"},
		{" -pf|1000002|&102 ", -3, "profit_share_expense"},
		{"dbu", 2, "deposit"},
		{"wbu", -1, "withdrawal"},
		{"-PF", -1, "profit_share_expense"},
		{"dbu #IT100000000000000005", -10, ""},
		{"wbu #IT100000000000000006", 10, ""},
		{"-PF|1000003|&103", 10, ""},
		{"dbu", 0, ""},
		{"wbu", 0, ""},
		{"-PF", 0, ""},
		{"xdbu #IT100000000000000007", 50, ""},
		{"wbuX #IT100000000000000008", -50, ""},
		{"-PFother", -50, ""},
		{"dbu #IT", 50, ""},
		{"wbu #ITunknown", -50, ""},
		{"-PF|1000004|", -50, ""},
		{"dbu #IT100000000000000009 wbu", 50, ""},
		{"wbu #IT100000000000000010 refund", -50, ""},
		{"subs|1000005", 0, ""},
	}
	trades := make([]mt4.Trade, 0, len(cases)+4)
	for i, test := range cases {
		trades = append(trades, mt4.Trade{Ticket: int32(100 + i), Command: 6, Comment: test.comment,
			OpenTime: job.Coverage.StartWall + uint32(i) + 1, Profit: test.amount})
	}
	trades = append(trades,
		mt4.Trade{Ticket: 201, Command: 7, Comment: "dbu #IT100000000000000011", OpenTime: job.Coverage.StartWall + 101, Profit: 500},
		mt4.Trade{Ticket: 202, Command: 6, Comment: "dbu #IT100000000000000012", OpenTime: job.Coverage.StartWall + 102, Profit: 100, Commission: -1},
		mt4.Trade{Ticket: 203, Command: 6, Comment: "wbu #IT100000000000000013", OpenTime: job.Coverage.StartWall + 103, Profit: -50, Swap: -1},
		mt4.Trade{Ticket: 204, Command: 6, Comment: "-PF|1000006|&106", OpenTime: job.Coverage.StartWall + 104, Profit: -10, Taxes: -1},
	)
	publishHistory(t, f, job, trades, now)
	view := fundingView(t, f, account.ID)
	if view.Deposits.String() != "132" || view.Withdrawals.String() != "31" || view.AutomaticCount != 9 || view.UnclassifiedCount != 18 {
		t.Fatalf("DPrime totals included profit shares or ambiguous records: %+v", view)
	}
	page, err := f.Accounts.Funds(t.Context(), account.ID, "", 100, false)
	if err != nil || len(page.Items) != len(trades) {
		t.Fatal("missing DPrime funding records", err, len(page.Items))
	}
	entries := map[int32]accounts.FundEntry{}
	for _, entry := range page.Items {
		entries[entry.Ticket] = entry
	}
	for i, test := range cases {
		entry := entries[int32(100+i)]
		if test.category == "" {
			if entry.Classification != nil {
				t.Fatal("ambiguous DPrime annotation classified", test.comment)
			}
			continue
		}
		if entry.Comment != test.comment || entry.Classification == nil || entry.Classification.Category != test.category ||
			entry.Classification.Source != "automatic" || entry.Classification.RuleVersion != accounts.FundingRuleVersion {
			t.Fatal("incorrect DPrime classification", test.comment, entry.Classification)
		}
	}
	for _, ticket := range []int32{201, 202, 203, 204} {
		if entries[ticket].Classification != nil {
			t.Fatal("credit or mixed DPrime flow was classified", ticket)
		}
	}
}

func TestDPrimeFundingReevaluatesV2CacheAndPreservesManualCorrections(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades := []mt4.Trade{
		{Ticket: 101, Command: 6, OpenTime: job.Coverage.StartWall + 1, Profit: 120, Comment: "dbu #IT100000000000000001"},
		{Ticket: 102, Command: 6, OpenTime: job.Coverage.StartWall + 2, Profit: -25, Comment: "wbu #IT100000000000000002"},
		{Ticket: 103, Command: 6, OpenTime: job.Coverage.StartWall + 3, Profit: -7, Comment: "-PF|1000001|&101"},
		{Ticket: 104, Command: 6, OpenTime: job.Coverage.StartWall + 4, Profit: 0, Comment: "subs|1000002"},
		{Ticket: 105, Command: 6, OpenTime: job.Coverage.StartWall + 5, Profit: 30, Comment: "dbu #IT100000000000000005"},
		{Ticket: 106, Command: 6, OpenTime: job.Coverage.StartWall + 6, Profit: -8, Comment: "wbu #IT100000000000000006"},
	}
	// Simulate the persisted unknown results from before DPrime rules existed.
	orders, err := syncer.ConvertHistory(job, historyResult(job, trades))
	if err != nil {
		t.Fatal(err)
	}
	heads := make([]storage.HistoryOrderHead, len(orders))
	for i := range orders {
		order := &orders[i]
		order.Revision = 1
		order.AutomaticCategory, order.AutomaticReason = "", ""
		order.AutomaticRuleVersion = "balance-comment-v2"
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
	classify(t, f, account.ID, 106, 1, 0, "unclassified")
	view := fundingView(t, f, account.ID)
	if view.Deposits.String() != "120" || view.Withdrawals.String() != "25" || view.AutomaticCount != 3 || view.ManualCount != 1 || view.UnclassifiedCount != 2 {
		t.Fatalf("DPrime cache upgrade disagrees with manual corrections: %+v", view)
	}
	page, err := f.Accounts.Funds(t.Context(), account.ID, "", 100, false)
	if err != nil || len(page.Items) != len(trades) {
		t.Fatal("missing DPrime cache upgrade records", err, len(page.Items))
	}
	for _, entry := range page.Items {
		if entry.Revision != 1 || entry.AutomaticRuleVersion != "balance-comment-v2" {
			t.Fatal("read-only rule upgrade rewrote source or cached data", entry.Ticket)
		}
		switch entry.Ticket {
		case 101, 102, 103:
			if entry.Classification == nil || entry.Classification.Source != "automatic" || entry.Classification.RuleVersion != accounts.FundingRuleVersion {
				t.Fatal("list retained stale DPrime classification", entry.Ticket)
			}
		case 105, 106:
			if entry.Classification == nil || entry.Classification.Source != "manual" {
				t.Fatal("DPrime rule upgrade overwrote manual correction", entry.Ticket)
			}
		}
	}
	cursor := ""
	for i, ticket := range []int32{106, 104} {
		unknown, err := f.Accounts.Funds(t.Context(), account.ID, cursor, 1, true)
		if err != nil || len(unknown.Items) != 1 || unknown.Items[0].Ticket != ticket {
			t.Fatal("DPrime pending page included automatically recognized records", unknown, err)
		}
		if (unknown.NextCursor == "") != (i == 1) {
			t.Fatal("incorrect pending DPrime pagination cursor", unknown.NextCursor)
		}
		cursor = unknown.NextCursor
	}
	var runs, revisions int64
	if err = f.Store.Read.Model(&storage.HistoryRun{}).Where("account_id=?", account.ID).Count(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Read.Model(&storage.HistoryOrder{}).Where("account_id=?", account.ID).Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if runs != 1 || revisions != int64(len(trades)) {
		t.Fatal("DPrime rule upgrade refetched history or added source revisions", runs, revisions)
	}
}
