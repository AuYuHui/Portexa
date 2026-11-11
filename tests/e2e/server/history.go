package main

import (
	"time"

	"portexa/mt4"
)

// Stable tickets and values are independent of the requested window, so
// overlapping backfills exercise real deduplication instead of inventing edits.
func syntheticHistory(span mt4.HistoryRange) []mt4.Trade {
	zone, _ := time.LoadLocation("Europe/Helsinki")
	now := time.Now().In(zone)
	start := uint32(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix())
	trades := []mt4.Trade{
		{Ticket: 69901, Command: 6, OpenTime: start - 365*86400 + 300, Profit: 1000, Comment: "Deposit"},
		{Ticket: 69902, Command: 6, OpenTime: start - 2*86400 + 300, Profit: 5, Comment: "Rebate"},
		{Ticket: 69903, Command: 6, OpenTime: start - 2*86400 + 360, Profit: -3, Comment: "Fee"},
		{Ticket: 69904, Command: 6, OpenTime: start - 2*86400 + 420, Profit: 7, Comment: "Operation 42"},
		{Ticket: 70001, Command: 0, OpenTime: start - 100, CloseTime: start + 120, Profit: 12.34, Commission: -0.11, Swap: -0.11, Taxes: -0.02},
		{Ticket: 70002, Command: 1, OpenTime: start + 60, CloseTime: start + 240, Profit: -2.50, Commission: -0.10},
		{Ticket: 70003, Command: 6, OpenTime: start + 300, Profit: 100, Comment: "Deposit"},
		{Ticket: 70004, Command: 6, CloseTime: start + 360, Profit: -20, Comment: "Withdrawal"},
		{Ticket: 70005, Command: 7, OpenTime: start + 400, Profit: 25},
	}
	filtered := make([]mt4.Trade, 0, len(trades))
	for _, trade := range trades {
		stamp := trade.CloseTime
		if stamp == 0 {
			stamp = trade.OpenTime
		}
		if stamp >= span.Start && stamp < span.End {
			filtered = append(filtered, trade)
		}
	}
	return filtered
}
