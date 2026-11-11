package storage

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type HistoryCoverage struct {
	ID              string `gorm:"primaryKey"`
	AccountID       string
	UnitRevision    int64
	TimeRulesetID   string
	ParentID        *string
	StartWall       uint32
	EndWall         uint32
	State           string
	CheckedAtMS     *int64
	NextAttemptAtMS int64
	FailureCount    int64
}

func (HistoryCoverage) TableName() string { return "history_coverage" }

type HistoryRun struct {
	ID               string  `gorm:"primaryKey" json:"-"`
	AccountID        string  `json:"-"`
	ServerID         string  `json:"-"`
	EndpointID       string  `json:"-"`
	CoverageID       string  `json:"-"`
	ConfigVersion    int64   `json:"-"`
	UnitRevision     int64   `json:"-"`
	NativeCurrency   string  `json:"-"`
	State            string  `json:"state"`
	StartedAtMS      int64   `json:"started_at_ms"`
	FinishedAtMS     *int64  `json:"finished_at_ms"`
	ErrorCode        *string `json:"error_code"`
	RawStatus        *int    `json:"raw_status"`
	CompletionReason *string `json:"-"`
}

func (HistoryRun) TableName() string { return "history_runs" }

type HistoryOrder struct {
	AccountID            string          `gorm:"primaryKey" json:"-"`
	Ticket               int32           `gorm:"primaryKey" json:"ticket"`
	Revision             int64           `gorm:"primaryKey" json:"revision"`
	SourceRunID          string          `json:"-"`
	SourceHash           []byte          `json:"-"`
	UnitRevision         int64           `json:"-"`
	NativeCurrency       string          `json:"native_currency"`
	Command              int32           `json:"command"`
	Symbol               string          `json:"symbol"`
	Comment              string          `json:"comment"`
	AutomaticCategory    string          `json:"-"`
	AutomaticReason      string          `json:"-"`
	AutomaticRuleVersion string          `json:"-"`
	Digits               int32           `json:"digits"`
	VolumeHundredths     int32           `json:"volume_hundredths"`
	OpenTimeWall         uint32          `json:"open_time_wall"`
	CloseTimeWall        uint32          `json:"close_time_wall"`
	EventTimeWall        uint32          `json:"event_time_wall"`
	OpenPrice            decimal.Decimal `gorm:"type:text" json:"open_price"`
	ClosePrice           decimal.Decimal `gorm:"type:text" json:"close_price"`
	SL                   decimal.Decimal `gorm:"type:text" json:"sl"`
	TP                   decimal.Decimal `gorm:"type:text" json:"tp"`
	Profit               decimal.Decimal `gorm:"type:text" json:"profit"`
	Commission           decimal.Decimal `gorm:"type:text" json:"commission"`
	Swap                 decimal.Decimal `gorm:"type:text" json:"swap"`
	Taxes                decimal.Decimal `gorm:"type:text" json:"taxes"`
	NetProfit            decimal.Decimal `gorm:"type:text" json:"net_profit"`
}

func (HistoryOrder) TableName() string { return "history_order_revisions" }

type HistoryOrderHead struct {
	AccountID string `gorm:"primaryKey"`
	Ticket    int32  `gorm:"primaryKey"`
	Revision  int64
}

func (HistoryOrderHead) TableName() string { return "history_order_heads" }

type CashFlowClassification struct {
	AccountID          string `gorm:"primaryKey" json:"-"`
	Ticket             int32  `gorm:"primaryKey" json:"-"`
	Revision           int64  `gorm:"primaryKey" json:"revision"`
	BasedTradeRevision int64  `json:"based_trade_revision"`
	Category           string `json:"category"`
	Reason             string `json:"reason"`
	CreatedAtMS        int64  `json:"created_at_ms"`
	Source             string `gorm:"-" json:"source"`
	RuleVersion        string `gorm:"-" json:"rule_version"`
}

func (CashFlowClassification) TableName() string { return "cash_flow_classifications" }

// Unpublished source rows are invisible behind history_order_heads. Failed or
// interrupted staging can be discarded without changing any published fact.
func DiscardUnpublishedHistory(tx *gorm.DB) error {
	return tx.Exec(`DELETE FROM history_order_revisions AS o
		WHERE o.source_run_id IN (SELECT id FROM history_runs WHERE state IN ('failed','canceled','interrupted'))
		AND NOT EXISTS (SELECT 1 FROM history_order_heads h
			WHERE h.account_id=o.account_id AND h.ticket=o.ticket AND h.revision=o.revision)`).Error
}
