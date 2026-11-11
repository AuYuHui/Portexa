package syncer

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

var ErrHistoryTooLarge = errors.New("history window exceeds publication budget")

// ConvertHistory never publishes a partial SDK response. Amounts and raw server
// wall clocks remain intact; business classifications belong to accounts.
func ConvertHistory(job accounts.HistoryJob, result mt4.Result) ([]storage.HistoryOrder, error) {
	if !result.SessionConfirmed || !result.AccountRead || !result.HistoryRead || !result.HistoryComplete ||
		strings.ToUpper(strings.TrimSpace(result.Account.Currency)) != job.Run.NativeCurrency ||
		!coversDay(result.HistoryCovered, job.Coverage.StartWall, job.Coverage.EndWall) {
		return nil, ErrIncomplete
	}
	if len(result.History) > accounts.HistoryPublishLimit {
		return nil, ErrHistoryTooLarge
	}
	orders := make([]storage.HistoryOrder, 0, len(result.History))
	seen := make(map[int32]bool, len(result.History))
	for _, trade := range result.History {
		if trade.Ticket <= 0 || seen[trade.Ticket] || trade.Command < 0 || trade.Command > 7 || trade.Digits < 0 || trade.Digits > 16 ||
			trade.VolumeHundredths < 0 || !utf8.ValidString(trade.Symbol) || len(trade.Symbol) > 128 {
			return nil, ErrIncomplete
		}
		seen[trade.Ticket] = true
		for _, value := range []float64{trade.OpenPrice, trade.ClosePrice, trade.SL, trade.TP, trade.Profit, trade.Commission, trade.Swap, trade.Taxes} {
			if !finite(value) {
				return nil, ErrIncomplete
			}
		}
		stamp := trade.CloseTime
		if trade.Command >= 6 && stamp == 0 {
			stamp = trade.OpenTime
		}
		if stamp == 0 {
			return nil, ErrIncomplete
		}
		if stamp < job.Coverage.StartWall || stamp >= job.Coverage.EndWall {
			continue
		}
		order := storage.HistoryOrder{AccountID: job.Account.ID, Ticket: trade.Ticket, SourceRunID: job.Run.ID,
			UnitRevision: job.Account.UnitRevision, NativeCurrency: job.Run.NativeCurrency,
			Command: trade.Command, Symbol: trade.Symbol, Digits: trade.Digits, VolumeHundredths: trade.VolumeHundredths,
			Comment:      strings.ToValidUTF8(trade.Comment, "\uFFFD"),
			OpenTimeWall: trade.OpenTime, CloseTimeWall: trade.CloseTime, EventTimeWall: stamp,
			OpenPrice: decimal.NewFromFloat(trade.OpenPrice), ClosePrice: decimal.NewFromFloat(trade.ClosePrice),
			SL: decimal.NewFromFloat(trade.SL), TP: decimal.NewFromFloat(trade.TP), Profit: decimal.NewFromFloat(trade.Profit),
			Commission: decimal.NewFromFloat(trade.Commission), Swap: decimal.NewFromFloat(trade.Swap), Taxes: decimal.NewFromFloat(trade.Taxes)}
		order.NetProfit = order.Profit.Add(order.Commission).Add(order.Swap).Add(order.Taxes)
		body, err := json.Marshal(struct {
			UnitRevision int64
			Order        storage.HistoryOrder
			CommentBytes []byte
		}{order.UnitRevision, order, []byte(trade.Comment)})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(body)
		order.SourceHash = append([]byte(nil), digest[:]...)
		orders = append(orders, order)
	}
	return orders, nil
}
