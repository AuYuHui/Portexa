package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestDailyWinRateUsesNetResultsAndIncludesBreakeven(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "UTC")
	job, err := f.Accounts.ClaimDaily(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := dailyResult(job)
	r.History = []mt4.Trade{
		{Ticket: 1, Command: 0, CloseTime: job.Day.Start, Profit: 100.15, Commission: -0.15},
		{Ticket: 2, Command: 1, CloseTime: job.Day.Start + 1, Profit: 2, Commission: -3},
		{Ticket: 3, Command: 0, CloseTime: job.Day.End - 1, Profit: 1, Swap: -0.5, Taxes: -0.5},
		{Ticket: 4, Command: 6, OpenTime: job.Day.Start + 1, Profit: 1000},
		{Ticket: 5, Command: 7, OpenTime: job.Day.Start + 1, Profit: 500},
		{Ticket: 6, Command: 2, CloseTime: job.Day.Start + 1, Profit: 250},
		{Ticket: 7, Command: 1, CloseTime: job.Day.End, Profit: 999},
	}
	report, err := syncer.ConvertDaily(job, r, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if report.ClosedTrades != 3 || *report.WinningTrades != 1 || *report.LosingTrades != 1 || *report.BreakevenTrades != 1 ||
		report.GrossProfit.String() != "100" || report.GrossLoss.String() != "1" || report.NetProfit.String() != "99" {
		t.Fatalf("wrong daily net statistics: %+v", report)
	}
	if err = f.Accounts.PublishDaily(t.Context(), job, report, now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(t.Context(), a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(detail.Account.Daily.Report)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["win_rate"] != "33.33333333" || fields["gross_profit"] != "100" || fields["gross_loss"] != "1" || fields["winning_trades"] != float64(1) {
		t.Fatal("statistics lost on publication/JSON", fields)
	}
	empty := storage.DailyReport{WinningTrades: new(int)}
	body, _ = json.Marshal(empty)
	if err = json.Unmarshal(body, &fields); err != nil || fields["win_rate"] != nil {
		t.Fatal("empty day invented a win rate", string(body), err)
	}
}

func TestMarginLevelSerializesPreciseKnownAndUnavailableValues(t *testing.T) {
	for _, test := range []struct {
		name, margin, equity string
		available            bool
		want                 any
	}{
		{"known", "3", "100.01", true, "3333.66666667"},
		{"zero", "0", "100.01", true, nil},
		{"unavailable", "3", "100.01", false, nil},
		{"negative equity", "100", "-5.01", true, "-5.01"},
		{"large decimal", "0.01", "9007199254740993.01", true, "90071992547409930100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			margin := decimal.RequireFromString(test.margin)
			snapshot := storage.Snapshot{Equity: decimal.RequireFromString(test.equity), Margin: &margin, MarginAvailable: test.available}
			body, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			if err = json.Unmarshal(body, &data); err != nil || data["margin_level"] != test.want {
				t.Fatal(string(body), err)
			}
		})
	}
}
