package syncer

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt5"
)

func (e *Engine) readMT5Deals(ctx context.Context, cfg *mt5.Config, window mt5.HistoryRange, currency string, limit int) ([]storage.HistoryOrder, error) {
	result, err := e.fetchMT5History(ctx, *cfg, window)
	cfg.Password = ""
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	accCurrency := strings.ToUpper(strings.TrimSpace(result.Account.Currency))
	if currency == "" || (accCurrency != currency && !(currency == "USC" && accCurrency == "USD")) {
		return nil, ErrIncomplete
	}
	if limit > 0 && len(result.Deals) > limit {
		return nil, ErrHistoryTooLarge
	}
	orders := make([]storage.HistoryOrder, 0, len(result.Deals))
	seen := make(map[uint64]bool, len(result.Deals))
	for _, deal := range result.Deals {
		if seen[deal.Ticket] || deal.WallTimeMS < int64(window.Start)*1000 ||
			deal.WallTimeMS >= int64(window.End)*1000 {
			return nil, ErrIncomplete
		}
		order, err := normalizeMT5Deal(deal)
		if err != nil {
			return nil, err
		}
		seen[deal.Ticket] = true
		order.NativeCurrency = currency
		orders = append(orders, order)
	}
	return orders, nil
}

func normalizeMT5Deal(deal mt5.Deal) (storage.HistoryOrder, error) {
	if deal.Ticket == 0 || deal.Ticket > maxMT5BrowserTicket || deal.Order > maxMT5BrowserTicket ||
		deal.PositionID > maxMT5BrowserTicket || deal.WallTimeMS <= 0 || deal.Digits > 16 ||
		deal.VolumeUnits/1_000_000 > 1<<31-1 || !utf8.ValidString(deal.Symbol) || len(deal.Symbol) > 128 {
		return storage.HistoryOrder{}, ErrIncomplete
	}
	for _, value := range []float64{deal.Price, deal.SL, deal.TP, deal.Profit, deal.Commission, deal.Swap, deal.Fee} {
		if !finite(value) {
			return storage.HistoryOrder{}, ErrIncomplete
		}
	}
	command := int32(6)
	switch deal.Type {
	case mt5.DealBuy, mt5.DealSell:
		if deal.Entry > mt5.EntryOutBy || deal.Symbol == "" || deal.VolumeUnits == 0 {
			return storage.HistoryOrder{}, ErrIncomplete
		}
		command = int32(deal.Type)
	case mt5.DealBuyCanceled:
		command = 0
	case mt5.DealSellCanceled:
		command = 1
	case mt5.DealCredit:
		command = 7
	}

	lots := decimal.New(int64(deal.VolumeUnits), -8)
	fee := decimal.NewFromFloat(deal.Fee)
	orderTicket, positionID := int64(deal.Order), int64(deal.PositionID)
	order := storage.HistoryOrder{Ticket: int64(deal.Ticket), Command: command, Symbol: deal.Symbol, Digits: int32(deal.Digits),
		Comment: strings.ToValidUTF8(deal.Comment, "\uFFFD"), VolumeHundredths: int32(deal.VolumeUnits / 1_000_000),
		MT5DealType: &deal.Type, MT5Entry: &deal.Entry, MT5OrderTicket: &orderTicket, MT5PositionID: &positionID,
		MT5WallTimeMS: &deal.WallTimeMS, EventTimeWall: uint32(deal.WallTimeMS / 1000), ExactLots: &lots, Fee: &fee,
		ClosePrice: decimal.NewFromFloat(deal.Price), SL: decimal.NewFromFloat(deal.SL), TP: decimal.NewFromFloat(deal.TP),
		Profit: decimal.NewFromFloat(deal.Profit), Commission: decimal.NewFromFloat(deal.Commission), Swap: decimal.NewFromFloat(deal.Swap)}
	order.NetProfit = order.Profit.Add(order.Commission).Add(order.Swap).Add(fee)
	if order.ClosingTrade() {
		order.CloseTimeWall = order.EventTimeWall
	}
	return order, nil
}

func (e *Engine) convertMT5History(ctx context.Context, job accounts.HistoryJob, config *mt5.Config) ([]storage.HistoryOrder, error) {
	orders, err := e.readMT5Deals(ctx, config, mt5.HistoryRange{Start: job.Coverage.StartWall, End: job.Coverage.EndWall},
		job.Run.NativeCurrency, accounts.HistoryPublishLimit)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		order := &orders[i]
		order.AccountID, order.SourceRunID, order.UnitRevision = job.Account.ID, job.Run.ID, job.Account.UnitRevision
		if err := hashHistoryOrder(order, order.Comment); err != nil {
			return nil, err
		}
	}
	return orders, nil
}
