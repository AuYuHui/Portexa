package syncer

import (
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
	"portexa/mt5"
)

func dailyPositionExits(job accounts.DailyJob, trades []mt4.Trade, orders []storage.HistoryOrder) []accounts.DailyPositionExit {
	exits := []accounts.DailyPositionExit{}
	if job.MT5Config == nil {
		for _, trade := range trades {
			if trade.Command > 1 || trade.CloseTime < job.Day.Start || trade.CloseTime >= job.Day.End {
				continue
			}
			exits = append(exits, accounts.DailyPositionExit{
				Identifier: int64(trade.Ticket), Ticket: int64(trade.Ticket), Symbol: trade.Symbol, Side: trade.Command,
				OpenTimeWall: trade.OpenTime, CloseWallMS: int64(trade.CloseTime) * 1000,
			})
		}
		return exits
	}
	for _, order := range orders {
		if !order.ClosingTrade() || order.MT5PositionID == nil || *order.MT5PositionID <= 0 {
			continue
		}
		exits = append(exits, accounts.DailyPositionExit{
			Identifier: *order.MT5PositionID, Ticket: order.Ticket, Symbol: order.Symbol, Side: 1 - order.Command,
			CloseWallMS: *order.MT5WallTimeMS, Reversal: *order.MT5Entry == mt5.EntryInOut,
		})
	}
	return exits
}
