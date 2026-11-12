package storage

import "github.com/shopspring/decimal"

type TransferMatch struct {
	ID                string          `gorm:"primaryKey" json:"id"`
	OutAccountID      string          `json:"out_account_id"`
	OutTicket         int32           `json:"out_ticket"`
	OutRevision       int64           `json:"out_revision"`
	InAccountID       string          `json:"in_account_id"`
	InTicket          int32           `json:"in_ticket"`
	InRevision        int64           `json:"in_revision"`
	OutClassification string          `json:"-"`
	InClassification  string          `json:"-"`
	OutRuleset        string          `json:"-"`
	InRuleset         string          `json:"-"`
	OutMS             int64           `json:"out_ms"`
	InMS              int64           `json:"in_ms"`
	AmountUSD         decimal.Decimal `gorm:"type:text" json:"amount_usd"`
	Reason            string          `json:"reason"`
	CreatedAtMS       int64           `json:"created_at_ms"`
}

func (TransferMatch) TableName() string { return "analytics_transfer_matches" }
