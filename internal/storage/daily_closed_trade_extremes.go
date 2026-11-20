package storage

import "github.com/shopspring/decimal"

type DailyClosedTrade struct {
	Ticket      int64           `json:"ticket,string"`
	Symbol      string          `json:"symbol"`
	CloseWallMS int64           `json:"close_wall_ms"`
	NetProfit   decimal.Decimal `json:"net_profit"`
}

type DailyClosedTradeExtremes struct {
	Profit *DailyClosedTrade `json:"profit"`
	Loss   *DailyClosedTrade `json:"loss"`
}

type DailyClosedTradeExtremesRecord struct {
	ReportID string                   `gorm:"primaryKey"`
	Summary  DailyClosedTradeExtremes `gorm:"serializer:json"`
}

func (DailyClosedTradeExtremesRecord) TableName() string { return "daily_closed_trade_extremes" }
