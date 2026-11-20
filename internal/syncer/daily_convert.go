package syncer

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

// ConvertDaily counts closed trades by close wall-clock time. Balance/credit
// records may only carry an open timestamp. Positive adjustments are not
// classified as deposits: they can also be rebates or other balance operations.
func ConvertDaily(job accounts.DailyJob, result mt4.Result, finished int64) (storage.DailyReport, error) {
	report := job.Run
	if !result.SessionConfirmed || !result.AccountRead || !result.HistoryRead || !result.HistoryComplete ||
		finished < report.StartedAtMS || !coversDay(result.HistoryCovered, job.Day.Start, job.Day.End) {
		return report, ErrIncomplete
	}
	currency := strings.ToUpper(strings.TrimSpace(result.Account.Currency))
	if currency == "" || currency != job.NativeCurrency {
		return report, ErrIncomplete
	}
	var totals dailyTotals
	seen := map[int32]bool{}
	for _, trade := range result.History {
		if trade.Ticket <= 0 || seen[trade.Ticket] || trade.Command < 0 || trade.Command > 7 {
			return report, ErrIncomplete
		}
		seen[trade.Ticket] = true
		for _, value := range []float64{trade.Profit, trade.Commission, trade.Swap, trade.Taxes} {
			if !finite(value) {
				return report, ErrIncomplete
			}
		}
		stamp := trade.CloseTime
		if trade.Command >= 6 && stamp == 0 {
			stamp = trade.OpenTime
		}
		if trade.Command <= 1 && stamp == 0 {
			return report, ErrIncomplete
		}
		if stamp < job.Day.Start || stamp >= job.Day.End {
			continue
		}
		net := decimal.NewFromFloat(trade.Profit).Add(decimal.NewFromFloat(trade.Commission)).
			Add(decimal.NewFromFloat(trade.Swap)).Add(decimal.NewFromFloat(trade.Taxes))
		totals.add(trade.Command, net, trade.Command <= 1)
	}
	return totals.report(report, currency, finished), nil
}

type dailyTotals struct {
	profit, adjustment, credit decimal.Decimal
	grossProfit, grossLoss     decimal.Decimal
	wins, losses, breakeven    int
}

func (t *dailyTotals) add(command int32, net decimal.Decimal, closed bool) {
	switch command {
	case 0, 1:
		t.profit = t.profit.Add(net)
		if !closed {
			return
		}
		switch net.Sign() {
		case 1:
			t.wins++
			t.grossProfit = t.grossProfit.Add(net)
		case -1:
			t.losses++
			t.grossLoss = t.grossLoss.Add(net.Abs())
		default:
			t.breakeven++
		}
	case 6:
		t.adjustment = t.adjustment.Add(net)
	case 7:
		t.credit = t.credit.Add(net)
	}
}

func (t dailyTotals) report(report storage.DailyReport, currency string, finished int64) storage.DailyReport {
	change := t.profit.Add(t.adjustment)
	report.State, report.FinishedAtMS, report.NativeCurrency = "succeeded", &finished, currency
	report.NetProfit, report.BalanceAdjustment, report.BalanceChange, report.CreditChange = &t.profit, &t.adjustment, &change, &t.credit
	report.ClosedTrades = t.wins + t.losses + t.breakeven
	report.WinningTrades, report.LosingTrades, report.BreakevenTrades = &t.wins, &t.losses, &t.breakeven
	report.GrossProfit, report.GrossLoss = &t.grossProfit, &t.grossLoss
	return report
}

func coversDay(covered []mt4.HistoryRange, start, end uint32) bool {
	if start >= end || len(covered) == 0 {
		return false
	}
	intervals := append([]mt4.HistoryRange(nil), covered...)
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })
	cursor := start
	for _, interval := range intervals {
		if interval.Start >= interval.End || interval.Start > cursor || interval.Start < start || interval.End > end {
			return false
		}
		if interval.End > cursor {
			cursor = interval.End
		}
	}
	return cursor == end
}
