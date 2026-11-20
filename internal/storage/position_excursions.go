package storage

import "github.com/shopspring/decimal"

type PositionExcursion struct {
	AccountID      string `gorm:"primaryKey"`
	UnitRevision   int64  `gorm:"primaryKey"`
	NativeCurrency string `gorm:"primaryKey"`
	Identifier     int64  `gorm:"primaryKey;autoIncrement:false"`
	Ticket         int64  `gorm:"primaryKey;autoIncrement:false"`
	Command        int32  `gorm:"primaryKey"`
	OpenTimeWall   uint32 `gorm:"primaryKey"`
	Symbol         string
	FirstSampleMS  int64           `gorm:"column:first_sample_ms"`
	LastSampleMS   int64           `gorm:"column:last_sample_ms"`
	Lowest         decimal.Decimal `gorm:"type:text"`
	LowestMS       int64           `gorm:"column:lowest_ms"`
	Highest        decimal.Decimal `gorm:"type:text"`
	HighestMS      int64           `gorm:"column:highest_ms"`
}

func (PositionExcursion) TableName() string { return "position_excursions" }

type PositionExcursionPoint struct {
	Amount decimal.Decimal `json:"amount"`
	Ticket string          `json:"ticket"`
	Symbol string          `json:"symbol"`
	AtMS   int64           `json:"at_ms"`
}

type DailyPositionExcursions struct {
	Closed  int                     `json:"closed"`
	Sampled int                     `json:"sampled"`
	Loss    *PositionExcursionPoint `json:"loss"`
	Profit  *PositionExcursionPoint `json:"profit"`
}

type DailyPositionExcursionRecord struct {
	ReportID string                  `gorm:"primaryKey"`
	Summary  DailyPositionExcursions `gorm:"serializer:json"`
}

func (DailyPositionExcursionRecord) TableName() string { return "daily_position_excursions" }
