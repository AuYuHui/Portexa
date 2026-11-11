package app_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func historyResult(job accounts.HistoryJob, trades []mt4.Trade) mt4.Result {
	result := testutil.Result(job.Run.NativeCurrency)
	result.HistoryRead, result.HistoryComplete = true, true
	result.HistoryCovered = []mt4.HistoryRange{{Start: job.Coverage.StartWall, End: job.Coverage.EndWall}}
	result.History = trades
	return result
}

func publishHistory(t *testing.T, f testutil.Fixture, job accounts.HistoryJob, trades []mt4.Trade, now time.Time) {
	t.Helper()
	orders, err := syncer.ConvertHistory(job, historyResult(job, trades))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishHistory(t.Context(), job, orders, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func fundingFixture(t *testing.T) (testutil.Fixture, storage.Account, *time.Time, []mt4.Trade) {
	t.Helper()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades := []mt4.Trade{
		{Ticket: 1, Command: 0, CloseTime: job.Day.Start + 1, Profit: 999},
		{Ticket: 6, Command: 6, OpenTime: job.Day.Start + 2, Profit: 100.01},
		{Ticket: 7, Command: 6, OpenTime: job.Day.Start + 3, Profit: -30.02},
		{Ticket: 8, Command: 7, OpenTime: job.Day.Start + 4, Profit: 50},
		{Ticket: 9, Command: 6, OpenTime: job.Day.Start - 100, Profit: 4.03},
		{Ticket: 10, Command: 6, OpenTime: job.Day.Start - 200, Profit: -5},
	}
	publishHistory(t, f, job, trades, now)
	now = now.Add(time.Second)
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, trades, now)
	return f, a, &now, trades
}

func fundingView(t *testing.T, f testutil.Fixture, id string) accounts.FundingView {
	t.Helper()
	detail, err := f.Accounts.Detail(t.Context(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	return detail.Account.Funding
}

func classify(t *testing.T, f testutil.Fixture, id string, ticket int32, trade, classification int64, category string) {
	t.Helper()
	if _, err := f.Accounts.ClassifyFund(t.Context(), id, ticket, accounts.ClassificationInput{
		TradeRevision: trade, ClassificationRevision: classification, Category: category, Reason: "合成账单核对依据",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFundingOnlyCountsConfirmedExternalFlowsAndRevisions(t *testing.T) {
	f, a, now, trades := fundingFixture(t)
	view := fundingView(t, f, a.ID)
	if !view.CoverageComplete || view.Deposits == nil || view.Deposits.String() != "0" || view.Withdrawals.String() != "0" || view.UnclassifiedCount != 4 || view.ConfirmedCount != 0 {
		t.Fatalf("invented deposits or incomplete coverage: %+v", view)
	}
	classify(t, f, a.ID, 6, 1, 0, "deposit")
	classify(t, f, a.ID, 7, 1, 0, "withdrawal")
	classify(t, f, a.ID, 9, 1, 0, "rebate")
	classify(t, f, a.ID, 10, 1, 0, "transfer_out")
	view = fundingView(t, f, a.ID)
	if view.Deposits.String() != "100.01" || view.Withdrawals.String() != "30.02" || view.UnclassifiedCount != 0 || view.ConfirmedCount != 4 {
		t.Fatalf("incorrect classified totals: %+v", view)
	}
	// A repeat response must retain source and classification revisions.
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, trades, *now)
	var count int64
	if err = f.Store.Read.Model(&storage.HistoryOrder{}).Where("account_id=?", a.ID).Count(&count).Error; err != nil || count != 6 {
		t.Fatal("repeat history added revisions", count, err)
	}
	// A source change invalidates the old manual classification atomically.
	*now = now.Add(time.Second)
	if err = f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	trades[1].Profit = 99.99
	publishHistory(t, f, job, trades, *now)
	view = fundingView(t, f, a.ID)
	if view.Deposits.String() != "0" || view.Withdrawals.String() != "30.02" || view.UnclassifiedCount != 1 {
		t.Fatal("obsolete classification counted", view)
	}
	page, err := f.Accounts.Funds(t.Context(), a.ID, "", 50, true)
	if err != nil || len(page.Items) != 1 || page.Items[0].Ticket != 6 || !page.Items[0].NeedsReview || page.Items[0].Revision != 2 {
		t.Fatal("changed source not reviewable", page, err)
	}
	classify(t, f, a.ID, 6, 2, 1, "deposit")
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "99.99" {
		t.Fatal(got)
	}
	// A classification correction is append-only and immediately changes totals.
	classify(t, f, a.ID, 6, 2, 2, "rebate")
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "0" {
		t.Fatal(got)
	}
}

func TestFundingClassificationRejectsStaleForeignCreditAndWrongDirection(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	for _, test := range []struct {
		ticket                int32
		trade, classification int64
		category              string
		conflict              bool
	}{
		{6, 1, 0, "withdrawal", false}, {7, 1, 0, "deposit", false}, {8, 1, 0, "deposit", false},
		{1, 1, 0, "deposit", false}, {6, 2, 0, "deposit", true}, {6, 1, 1, "deposit", true}, {6, 1, 0, "unknown", false},
	} {
		_, err := f.Accounts.ClassifyFund(t.Context(), a.ID, test.ticket, accounts.ClassificationInput{TradeRevision: test.trade, ClassificationRevision: test.classification, Category: test.category, Reason: "合成核对"})
		if err == nil || test.conflict && !errors.Is(err, storage.ErrConflict) {
			t.Fatal("invalid classification accepted", test, err)
		}
	}
	if _, err := f.Accounts.ClassifyFund(t.Context(), a.ID, 6, accounts.ClassificationInput{TradeRevision: 1, Category: "deposit"}); err == nil {
		t.Fatal("missing reason accepted")
	}
	if _, err := f.Accounts.ClassifyFund(t.Context(), a.ID, 6, accounts.ClassificationInput{
		TradeRevision: 1, Category: "deposit", Reason: strings.Repeat("合成", 200) + "\n账单已核对",
	}); err != nil {
		t.Fatal("valid multiline Unicode reason rejected", err)
	}
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id=?", a.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	other := f.Account(t, server, endpoint, 9991, "USC")
	if _, err := f.Accounts.ClassifyFund(t.Context(), other.ID, 6, accounts.ClassificationInput{TradeRevision: 1, Category: "deposit", Reason: "合成核对"}); err == nil {
		t.Fatal("foreign source accepted")
	}
}

func TestFundingPagingEmptyAndFailurePreserveKnownFacts(t *testing.T) {
	f, a, now, _ := fundingFixture(t)
	seen := map[int32]bool{}
	cursor := ""
	for {
		page, err := f.Accounts.Funds(t.Context(), a.ID, cursor, 2, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Items {
			if seen[entry.Ticket] {
				t.Fatal("duplicate page row")
			}
			seen[entry.Ticket] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatal("trade included or fund missing", seen)
	}
	for _, cursor := range []string{"bad", "1:0", "1:-1", "9999999999999:1", "1:2147483648"} {
		if _, err := f.Accounts.Funds(t.Context(), a.ID, cursor, 50, false); err == nil {
			t.Fatal("bad cursor accepted", cursor)
		}
	}
	classify(t, f, a.ID, 6, 1, 0, "deposit")
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, nil, *now)
	if got := fundingView(t, f, a.ID).Deposits.String(); got != "100.01" {
		t.Fatal("empty window deleted historical facts", got)
	}
	*now = now.Add(time.Second)
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err = f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.FailHistory(t.Context(), job, accounts.Failure{Code: "connection", Operation: "history"}, false); err != nil {
		t.Fatal(err)
	}
	view := fundingView(t, f, a.ID)
	if !view.CoverageComplete || view.State != "failed" || view.Deposits.String() != "100.01" {
		t.Fatal("failure erased prior published state", view)
	}
}

func TestHistoryConverterRequiresCompleteFiniteMatchingWindow(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*mt4.Result){
		func(r *mt4.Result) { r.HistoryComplete = false }, func(r *mt4.Result) { r.HistoryRead = false },
		func(r *mt4.Result) { r.HistoryCovered = nil }, func(r *mt4.Result) { r.Account.Currency = "USD" },
		func(r *mt4.Result) {
			r.History = []mt4.Trade{{Ticket: 1, Command: 6, OpenTime: job.Coverage.StartWall + 1, Profit: math.NaN()}}
		},
		func(r *mt4.Result) { r.History = []mt4.Trade{{Ticket: 1, Command: 0}} },
		func(r *mt4.Result) { r.History = []mt4.Trade{{Ticket: 1, Command: 9}} },
		func(r *mt4.Result) { r.History = []mt4.Trade{{Ticket: 1, Command: 6}, {Ticket: 1, Command: 6}} },
	} {
		r := historyResult(job, nil)
		edit(&r)
		if _, err := syncer.ConvertHistory(job, r); !errors.Is(err, syncer.ErrIncomplete) {
			t.Fatal("invalid history accepted", err)
		}
	}
	r := historyResult(job, make([]mt4.Trade, accounts.HistoryPublishLimit+1))
	if _, err := syncer.ConvertHistory(job, r); !errors.Is(err, syncer.ErrHistoryTooLarge) {
		t.Fatal("publication budget not enforced", err)
	}
	label := "修改后的合成账户"
	if err = f.Accounts.UpdateAccount(context.Background(), a.ID, accounts.AccountPatch{Label: &label}); err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishHistory(t.Context(), job, nil, job.Run.StartedAtMS); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("obsolete configuration published", err)
	}
}
