package storage

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

func (report DailyReport) MarshalJSON() ([]byte, error) {
	type fields DailyReport
	var winRate *decimal.Decimal
	if report.WinningTrades != nil && report.ClosedTrades > 0 {
		value := decimal.NewFromInt(int64(*report.WinningTrades)).Mul(decimal.NewFromInt(100)).DivRound(decimal.NewFromInt(int64(report.ClosedTrades)), 8)
		winRate = &value
	}
	return json.Marshal(struct {
		fields
		WinRate *decimal.Decimal `json:"win_rate"`
	}{fields(report), winRate})
}

func (snapshot Snapshot) MarshalJSON() ([]byte, error) {
	type fields Snapshot
	var level *decimal.Decimal
	if snapshot.MarginAvailable && snapshot.Margin != nil && snapshot.Margin.IsPositive() {
		value := snapshot.Equity.Mul(decimal.NewFromInt(100)).DivRound(*snapshot.Margin, 8)
		level = &value
	}
	return json.Marshal(struct {
		fields
		MarginLevel *decimal.Decimal `json:"margin_level"`
	}{fields(snapshot), level})
}
