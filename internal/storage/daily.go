package storage

import "github.com/shopspring/decimal"

// DailyReport is one atomic observation of a server calendar day, not a ledger
// or a claim that future activity in the requested day has already occurred.
type DailyReport struct {
	ID                string           `gorm:"column:id;primaryKey" json:"-"`
	AccountID         string           `gorm:"column:account_id" json:"-"`
	ServerID          string           `gorm:"column:server_id" json:"-"`
	EndpointID        string           `gorm:"column:endpoint_id" json:"-"`
	ConfigVersion     int64            `gorm:"column:config_version" json:"-"`
	UnitRevision      int64            `gorm:"column:unit_revision" json:"-"`
	TimeRulesetID     string           `gorm:"column:time_ruleset_id" json:"-"`
	ReportDate        string           `gorm:"column:report_date" json:"date"`
	TimeZone          string           `gorm:"column:time_zone" json:"time_zone"`
	WindowStart       uint32           `gorm:"column:window_start" json:"window_start_wall"`
	WindowEnd         uint32           `gorm:"column:window_end" json:"window_end_wall"`
	State             string           `gorm:"column:state" json:"-"`
	StartedAtMS       int64            `gorm:"column:started_at_ms" json:"observed_from_ms"`
	FinishedAtMS      *int64           `gorm:"column:finished_at_ms" json:"observed_to_ms"`
	NativeCurrency    string           `gorm:"column:native_currency" json:"native_currency"`
	NetProfit         *decimal.Decimal `gorm:"column:net_profit;type:text" json:"net_profit"`
	BalanceAdjustment *decimal.Decimal `gorm:"column:balance_adjustment;type:text" json:"balance_adjustment"`
	BalanceChange     *decimal.Decimal `gorm:"column:balance_change;type:text" json:"balance_change"`
	CreditChange      *decimal.Decimal `gorm:"column:credit_change;type:text" json:"credit_change"`
	ClosedTrades      int              `gorm:"column:closed_trades" json:"closed_trades"`
	ErrorCode         *string          `gorm:"column:error_code" json:"-"`
	RawStatus         *int             `gorm:"column:raw_status" json:"-"`
	CompletionReason  *string          `gorm:"column:completion_reason" json:"-"`
}

func (DailyReport) TableName() string { return "daily_reports" }
