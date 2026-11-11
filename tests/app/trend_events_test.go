package app_test

import (
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/mt4"
)

func TestAssetTrendOnlyMarksConfirmedCashFlowsAndTimes(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	start := now
	f, account := dailyFixture(t, &now, "UTC")
	now = now.Add(10 * time.Minute)
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	wall := uint32(start.Add(time.Minute).Unix())
	publishHistory(t, f, job, []mt4.Trade{
		{Ticket: 88001, Command: 6, OpenTime: wall, Profit: 100, Comment: "Deposit"},
		{Ticket: 88002, Command: 6, OpenTime: wall + 1, Profit: -20, Comment: "Withdrawal"},
		{Ticket: 88003, Command: 6, OpenTime: wall + 2, Profit: 7, Comment: "Operation 42"},
		{Ticket: 88004, Command: 7, OpenTime: wall + 3, Profit: 50},
		{Ticket: 88005, Command: 6, OpenTime: wall + 4, Profit: 25, Comment: "Deposit"},
	}, now)
	classify(t, f, account.ID, 88005, 1, 0, "unclassified")
	input := trendRange(start, now, "native")
	view, err := f.Accounts.AccountTrend(t.Context(), account.ID, input)
	if err != nil || len(view.Events) != 2 || view.Events[0].Kind != "deposit" ||
		view.Events[0].TimeMS != int64(wall)*1000 || view.Events[0].Amount.String() != "100" || view.Events[0].Currency != "USC" {
		t.Fatal("invented or misplaced cash flow", view.Events, err)
	}
	input.Unit = "USD"
	view, err = f.Accounts.AccountTrend(t.Context(), account.ID, input)
	if err != nil || len(view.Events) != 2 || view.Events[0].Amount.String() != "1" || view.Events[1].Amount.String() != "-0.2" {
		t.Fatal("marker conversion disagrees with snapshot units", view.Events, err)
	}
}

func TestAssetTrendAmbiguousDSTCashFlowIsNotMarked(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "Europe/Helsinki")
	now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	trendSample(t, f, account, now, 1000)
	job, err := f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishHistory(t, f, job, nil, now)
	now = now.Add(time.Second)
	job, err = f.Accounts.ClaimHistory(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := uint32(time.Date(2026, 10, 25, 3, 30, 0, 0, time.UTC).Unix())
	unique := ambiguous + 3600
	publishHistory(t, f, job, []mt4.Trade{
		{Ticket: 89001, Command: 6, OpenTime: ambiguous, Profit: 100, Comment: "Deposit"},
		{Ticket: 89002, Command: 6, OpenTime: unique, Profit: 100, Comment: "Deposit"},
	}, now)
	view, err := f.Accounts.AccountTrend(t.Context(), account.ID, trendRange(
		time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), "native"))
	expected := time.Date(2026, 10, 25, 2, 30, 0, 0, time.UTC).UnixMilli()
	if err != nil || len(view.Events) != 1 || view.Events[0].Ticket != 89002 ||
		view.Events[0].TimeMS != expected || view.UnplacedEvents != 1 {
		t.Fatal("DST ambiguity silently resolved", view.Events, view.UnplacedEvents, err)
	}
	// A later rule must not reinterpret an event before its effective date.
	if _, err := f.Accounts.ConfirmServerTime(t.Context(), account.ServerID, accounts.ServerTimeInput{
		PreviousRulesetID: job.Day.TimeRulesetID, TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-later-rule",
	}); err != nil {
		t.Fatal(err)
	}
	view, err = f.Accounts.AccountTrend(t.Context(), account.ID, trendRange(
		time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), "native"))
	if err != nil || len(view.Events) != 1 || view.Events[0].TimeMS != expected {
		t.Fatal("later clock rule rewrote a historical event", view.Events, err)
	}
}
