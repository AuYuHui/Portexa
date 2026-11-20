package testutil

import (
	"time"

	"portexa/mt5"
)

func MT5Snapshot() mt5.Result {
	const partialOrderTicket = 8_000_000_002
	return mt5.Result{
		Account: mt5.Account{Balance: 2000, Credit: 50, Currency: "USD", Leverage: 200},
		Positions: []mt5.Position{{ID: partialOrderTicket, Symbol: "TESTUSD", Side: 1, Digits: 5,
			OpenTime: 1_780_000_000, VolumeUnits: 123456, OpenPrice: 1.23456, CurrentPrice: 1.23457,
			Profit: 5, Commission: -0.1, Swap: -0.2}},
		Orders: []mt5.PendingOrder{{Ticket: partialOrderTicket, Symbol: "TESTUSD", Type: 7, Digits: 5,
			SetupTime: 1_780_000_000, RemainingVolumeUnits: 654321, Price: 1.23, TriggerPrice: 1.2299, CurrentPrice: 1.23457}},
	}
}

const MT5UnknownFundingTicket = 8_000_001_011

func MT5Deals(day time.Time) []mt5.Deal {
	today := day.UnixMilli()
	old := today - 40_000
	trade := func(ticket int, at int64, dealType mt5.DealType, entry mt5.DealEntry, profit, commission, swap, fee float64) mt5.Deal {
		return mt5.Deal{Ticket: uint64(8_000_001_000 + ticket), Order: 8_000_000_022, PositionID: 8_000_000_032,
			WallTimeMS: at, Symbol: "TESTUSD", Digits: 5, Type: dealType, Entry: entry, Price: 1.23456,
			VolumeUnits: 123456, Profit: profit, Commission: commission, Swap: swap, Fee: fee}
	}
	fund := func(ticket int, at int64, dealType mt5.DealType, amount float64, comment string) mt5.Deal {
		return mt5.Deal{Ticket: uint64(8_000_001_000 + ticket), WallTimeMS: at, Type: dealType, Profit: amount, Comment: comment}
	}
	return []mt5.Deal{
		trade(1, today+111, mt5.DealBuy, mt5.EntryIn, 0, -1, 0, -0.5),
		trade(2, today+222, mt5.DealSell, mt5.EntryOut, 10.5, 0, -0.5, -1),
		trade(3, today+333, mt5.DealBuy, mt5.EntryInOut, -4, -0.5, 0, 0),
		trade(4, today+444, mt5.DealSell, mt5.EntryOutBy, 0, 0, 0, 0),
		trade(5, today+555, mt5.DealSellCanceled, mt5.EntryOut, 0, 0, 0, 0),
		fund(6, today+666, mt5.DealBalance, 50, "deposit"),
		fund(7, today+700, mt5.DealBalance, -20, "withdrawal"),
		fund(8, today+777, mt5.DealCommissionDaily, -3, "settlement"),
		fund(9, today+800, mt5.DealInterest, 1, ""),
		fund(10, today+833, mt5.DealDividend, 2, ""),
		fund(11, today+855, mt5.DealBalance, 7, "synthetic unspecified"),
		fund(12, today+999, mt5.DealCredit, 4, ""),
		trade(13, old+345, mt5.DealBuy, mt5.EntryIn, 0, -1, 0, 0),
		trade(14, old+456, mt5.DealSell, mt5.EntryOut, 20, -1, 0, -1),
		fund(15, old+567, mt5.DealCommissionDaily, -2, ""),
		fund(16, old+678, mt5.DealDividendFranked, 3, ""),
	}
}

func MT5History(window mt5.HistoryRange, deals []mt5.Deal) mt5.HistoryResult {
	result := mt5.HistoryResult{Account: mt5.Account{Currency: "USD"}}
	for _, deal := range deals {
		if stamp := deal.WallTimeMS / 1000; stamp >= int64(window.Start) && stamp < int64(window.End) {
			result.Deals = append(result.Deals, deal)
		}
	}
	return result
}
