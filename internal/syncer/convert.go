// Package syncer schedules read-only snapshots without changing the frozen SDK.
package syncer

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

var ErrIncomplete = errors.New("incomplete or invalid snapshot")

func Convert(job accounts.Job, result mt4.Result, from, to int64) (storage.Snapshot, []storage.Position, error) {
	if !result.SessionConfirmed || !result.AccountRead || from > to {
		return storage.Snapshot{}, nil, ErrIncomplete
	}
	a := result.Account
	currency := strings.ToUpper(strings.TrimSpace(a.Currency))
	if currency == "" || len(currency) > 12 || !utf8.ValidString(currency) {
		return storage.Snapshot{}, nil, ErrIncomplete
	}
	for _, number := range []float64{a.Balance, a.Credit, a.Equity} {
		if !finite(number) {
			return storage.Snapshot{}, nil, ErrIncomplete
		}
	}
	snapshot := storage.Snapshot{ID: storage.NewID(), AccountID: job.Account.ID, SyncRunID: job.Run.ID,
		ObservedFromMS: from, ObservedToMS: to, NativeCurrency: currency, UnitRevision: job.Account.UnitRevision,
		Balance: decimal.NewFromFloat(a.Balance), Credit: decimal.NewFromFloat(a.Credit), Equity: decimal.NewFromFloat(a.Equity), Leverage: a.Leverage}
	if job.Account.Currency != nil && *job.Account.Currency == currency && job.Account.UnitVerifiedAtMS != nil {
		snapshot.USDScale = job.Account.USDScale
	}
	if a.MarginAvailable {
		if !finite(a.Margin) || a.Margin < 0 {
			return snapshot, nil, ErrIncomplete
		}
		margin := decimal.NewFromFloat(a.Margin)
		snapshot.Margin, snapshot.MarginAvailable = &margin, true
	}
	positions := make([]storage.Position, 0, len(result.Trades))
	seen := make(map[int32]bool, len(result.Trades))
	for _, trade := range result.Trades {
		if trade.Command < 0 || trade.Command > 5 || trade.CloseTime != 0 || trade.Ticket <= 0 || seen[trade.Ticket] || trade.VolumeHundredths < 0 || trade.Digits < 0 || trade.Digits > 16 || !utf8.ValidString(trade.Symbol) || len(trade.Symbol) > 128 {
			return snapshot, nil, ErrIncomplete
		}
		seen[trade.Ticket] = true
		for _, number := range []float64{trade.OpenPrice, trade.ClosePrice, trade.SL, trade.TP, trade.Profit, trade.Commission, trade.Swap, trade.Taxes} {
			if !finite(number) {
				return snapshot, nil, ErrIncomplete
			}
		}
		position := storage.Position{AccountID: job.Account.ID, SnapshotID: snapshot.ID, Ticket: trade.Ticket,
			Symbol: trade.Symbol, Command: trade.Command, Digits: trade.Digits, VolumeHundredths: trade.VolumeHundredths,
			OpenTimeWall: trade.OpenTime, CloseTimeWall: trade.CloseTime, OpenPrice: decimal.NewFromFloat(trade.OpenPrice),
			CurrentPrice: decimal.NewFromFloat(trade.ClosePrice), SL: decimal.NewFromFloat(trade.SL), TP: decimal.NewFromFloat(trade.TP),
			Profit: decimal.NewFromFloat(trade.Profit), Commission: decimal.NewFromFloat(trade.Commission), Swap: decimal.NewFromFloat(trade.Swap), Taxes: decimal.NewFromFloat(trade.Taxes)}
		positions = append(positions, position)
		if trade.Command <= 1 {
			snapshot.FloatingNetProfit = snapshot.FloatingNetProfit.Add(position.NetProfit())
		}
	}
	return snapshot, positions, nil
}

func finite(value float64) bool { return !math.IsInf(value, 0) && !math.IsNaN(value) }
