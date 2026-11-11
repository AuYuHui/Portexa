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
	profit, adjustment, credit := decimal.Zero, decimal.Zero, decimal.Zero
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
		switch trade.Command {
		case 0, 1:
			profit = profit.Add(net)
			report.ClosedTrades++
		case 6:
			adjustment = adjustment.Add(net)
		case 7:
			credit = credit.Add(net)
		}
	}
	change := profit.Add(adjustment)
	report.State, report.FinishedAtMS, report.NativeCurrency = "succeeded", &finished, currency
	report.NetProfit, report.BalanceAdjustment, report.BalanceChange, report.CreditChange = &profit, &adjustment, &change, &credit
	return report, nil
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
