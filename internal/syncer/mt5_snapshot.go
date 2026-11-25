package syncer

import (
	"context"
	"errors"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt5"
)

func convertMT5(job accounts.Job, result mt5.Result, from, to int64) (storage.Snapshot, []storage.Position, error) {
	a := result.Account
	currency := strings.ToUpper(strings.TrimSpace(a.Currency))
	if from > to || len(currency) == 0 || len(currency) > 12 || !utf8.ValidString(currency) ||
		!finite(a.Balance) || !finite(a.Credit) {
		return storage.Snapshot{}, nil, ErrIncomplete
	}
	isUSC := job.Account.Currency != nil && *job.Account.Currency == "USC" && (currency == "USD" || currency == "USC")
	nativeCurrency := currency
	if isUSC {
		nativeCurrency = "USC"
	}
	snapshot := storage.Snapshot{ID: storage.NewID(), AccountID: job.Account.ID, SyncRunID: job.Run.ID,
		ObservedFromMS: from, ObservedToMS: to, NativeCurrency: nativeCurrency, UnitRevision: job.Account.UnitRevision,
		Balance: decimal.NewFromFloat(a.Balance), Credit: decimal.NewFromFloat(a.Credit), Leverage: a.Leverage}
	if job.Account.Currency != nil && (*job.Account.Currency == currency || isUSC) && job.Account.UnitVerifiedAtMS != nil {
		snapshot.USDScale = job.Account.USDScale
	}
	positions := make([]storage.Position, 0, len(result.Positions)+len(result.Orders))
	seen := make(map[uint64]bool, len(result.Positions))
	for _, p := range result.Positions {
		if p.Side > 1 || seen[p.ID] || p.VolumeUnits == 0 {
			return storage.Snapshot{}, nil, ErrIncomplete
		}
		position, err := makeMT5Position(job.Account.ID, snapshot.ID, p)
		if err != nil {
			return storage.Snapshot{}, nil, err
		}
		seen[p.ID] = true
		snapshot.FloatingNetProfit = snapshot.FloatingNetProfit.Add(position.NetProfit())
		positions = append(positions, position)
	}
	clear(seen)
	for _, order := range result.Orders {
		if order.Type < 2 || order.Type > 7 || seen[order.Ticket] || !finite(order.TriggerPrice) {
			return storage.Snapshot{}, nil, ErrIncomplete
		}
		position, err := makeMT5Position(job.Account.ID, snapshot.ID, mt5.Position{
			ID: order.Ticket, Symbol: order.Symbol, Side: order.Type, Digits: order.Digits,
			OpenTime: order.SetupTime, VolumeUnits: order.RemainingVolumeUnits, OpenPrice: order.Price,
			CurrentPrice: order.CurrentPrice, SL: order.SL, TP: order.TP,
		})
		if err != nil {
			return storage.Snapshot{}, nil, err
		}
		if order.Type >= 6 && order.TriggerPrice != 0 {
			trigger := decimal.NewFromFloat(order.TriggerPrice)
			position.TriggerPrice = &trigger
		}
		seen[order.Ticket] = true
		positions = append(positions, position)
	}
	if a.MarginAvailable {
		if !finite(a.Margin) || a.Margin < 0 {
			return storage.Snapshot{}, nil, ErrIncomplete
		}
		margin := decimal.NewFromFloat(a.Margin)
		snapshot.Margin, snapshot.MarginAvailable = &margin, true
	}
	// Broker adjustments such as blocked funds may be absent from the snapshot.
	snapshot.Equity = snapshot.Balance.Add(snapshot.Credit).Add(snapshot.FloatingNetProfit)
	return snapshot, positions, nil
}

const maxMT5BrowserTicket = 1<<53 - 1

func makeMT5Position(accountID, snapshotID string, position mt5.Position) (storage.Position, error) {
	const maxVolumeHundredths = 1<<31 - 1
	p := position
	if p.ID == 0 || p.ID > maxMT5BrowserTicket || p.Digits > 16 || p.VolumeUnits/1_000_000 > maxVolumeHundredths ||
		p.OpenTime == 0 || p.Symbol == "" || len(p.Symbol) > 128 || !utf8.ValidString(p.Symbol) {
		return storage.Position{}, ErrIncomplete
	}
	for _, value := range []float64{p.OpenPrice, p.CurrentPrice, p.SL, p.TP, p.Profit, p.Commission, p.Swap} {
		if !finite(value) {
			return storage.Position{}, ErrIncomplete
		}
	}
	lots := decimal.New(int64(p.VolumeUnits), -8)
	return storage.Position{AccountID: accountID, SnapshotID: snapshotID, Ticket: int64(p.ID),
		Command: int32(p.Side), Digits: int32(p.Digits), Symbol: p.Symbol, OpenTimeWall: p.OpenTime,
		MT5Identifier: p.Identifier, MT5NativeTicket: p.NativeTicket,
		VolumeHundredths: int32(p.VolumeUnits / 1_000_000), ExactLots: &lots,
		OpenPrice: decimal.NewFromFloat(p.OpenPrice), CurrentPrice: decimal.NewFromFloat(p.CurrentPrice),
		SL: decimal.NewFromFloat(p.SL), TP: decimal.NewFromFloat(p.TP), Profit: decimal.NewFromFloat(p.Profit),
		Commission: decimal.NewFromFloat(p.Commission), Swap: decimal.NewFromFloat(p.Swap)}, nil
}

func classifyMT5(err error) accounts.Failure {
	var network net.Error
	failure := accounts.Failure{Code: "connection", Operation: "mt5_snapshot"}
	var history *mt5.HistoryError
	if errors.As(err, &history) {
		failure.Operation = string(history.Phase)
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, storage.ErrConflict), errors.Is(err, accounts.ErrEndpoint):
		failure.Code, failure.Canceled = "configuration_changed", true
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		failure.Code = "timeout"
	case errors.Is(err, mt5.ErrReadOnly):
		failure.Code, failure.Permanent = "read_only_required", true
	case errors.Is(err, mt5.ErrRejected):
		failure.Code, failure.Permanent = "server_rejected", true
	case errors.Is(err, mt5.ErrConfig):
		failure.Code, failure.Permanent = "invalid_config", true
	case errors.Is(err, mt5.ErrResourceLimit):
		failure.Code = "resource_limit"
		failure.Permanent = history == nil || history.Phase != mt5.HistoryDeals
	case errors.Is(err, mt5.ErrConnection):
		failure.Code = "connection"
	case errors.Is(err, mt5.ErrProtocol):
		failure.Code = "invalid_response"
	case errors.Is(err, ErrIncomplete):
		failure.Code = "snapshot_invalid"
	}
	return failure
}
