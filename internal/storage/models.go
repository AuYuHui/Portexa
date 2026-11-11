package storage

import "github.com/shopspring/decimal"

type AuthSettings struct {
	ID           int    `gorm:"column:id;primaryKey" json:"-"`
	PasswordHash string `gorm:"column:password_hash" json:"-"`
	AuthVersion  int64  `gorm:"column:auth_version" json:"-"`
	ChangedAtMS  int64  `gorm:"column:changed_at_ms" json:"-"`
}

func (AuthSettings) TableName() string { return "auth_settings" }

type Session struct {
	TokenSHA256         []byte `gorm:"column:token_sha256;primaryKey" json:"-"`
	AuthVersion         int64  `gorm:"column:auth_version" json:"-"`
	CreatedAtMS         int64  `gorm:"column:created_at_ms" json:"-"`
	LastInteractiveAtMS int64  `gorm:"column:last_interactive_at_ms" json:"-"`
	IdleExpiresAtMS     int64  `gorm:"column:idle_expires_at_ms" json:"-"`
	ExpiresAtMS         int64  `gorm:"column:expires_at_ms" json:"-"`
	RevokedAtMS         *int64 `gorm:"column:revoked_at_ms" json:"-"`
}

func (Session) TableName() string { return "sessions" }

type Server struct {
	ID                  string  `gorm:"column:id;primaryKey" json:"id"`
	Protocol            string  `gorm:"column:protocol" json:"protocol"`
	BrokerName          string  `gorm:"column:broker_name" json:"broker_name"`
	ServerName          string  `gorm:"column:server_name" json:"server_name"`
	ActiveTimeRulesetID *string `gorm:"column:active_time_ruleset_id" json:"-"`
	CreatedAtMS         int64   `gorm:"column:created_at_ms" json:"created_at_ms"`
	RetiredAtMS         *int64  `gorm:"column:retired_at_ms" json:"retired_at_ms"`
}

func (Server) TableName() string { return "servers" }

type Endpoint struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	ServerID         string  `gorm:"column:server_id" json:"server_id"`
	Revision         int64   `gorm:"column:revision" json:"revision"`
	IPAddress        string  `gorm:"column:ip_address" json:"ip_address"`
	Port             int     `gorm:"column:port" json:"port"`
	State            string  `gorm:"column:state" json:"state"`
	EvidenceRef      *string `gorm:"column:evidence_ref" json:"evidence_ref"`
	EvidenceSHA256   []byte  `gorm:"column:evidence_sha256" json:"-"`
	ReviewedAtMS     *int64  `gorm:"column:reviewed_at_ms" json:"reviewed_at_ms"`
	RiskAcceptedAtMS *int64  `gorm:"column:risk_accepted_at_ms" json:"risk_accepted_at_ms"`
	CreatedAtMS      int64   `gorm:"column:created_at_ms" json:"created_at_ms"`
}

func (Endpoint) TableName() string { return "server_endpoints" }

func (e Endpoint) Approved() bool {
	return e.State == "reviewed" && e.EvidenceRef != nil && *e.EvidenceRef != "" &&
		len(e.EvidenceSHA256) == 32 && e.ReviewedAtMS != nil && e.RiskAcceptedAtMS != nil
}

type Account struct {
	ID                       string           `gorm:"column:id;primaryKey" json:"-"`
	ServerID                 string           `gorm:"column:server_id" json:"-"`
	PinnedEndpointID         string           `gorm:"column:pinned_endpoint_id" json:"-"`
	Login                    uint32           `gorm:"column:login" json:"-"`
	Label                    string           `gorm:"column:label" json:"-"`
	Currency                 *string          `gorm:"column:currency" json:"-"`
	USDScale                 *decimal.Decimal `gorm:"column:usd_scale;type:text" json:"-"`
	UnitRevision             int64            `gorm:"column:unit_revision" json:"-"`
	UnitVerifiedAtMS         *int64           `gorm:"column:unit_verified_at_ms" json:"-"`
	CredentialEnvelope       []byte           `gorm:"column:credential_envelope" json:"-"`
	CredentialKeyID          string           `gorm:"column:credential_key_id" json:"-"`
	CredentialVersion        int64            `gorm:"column:credential_version" json:"-"`
	ConfigVersion            int64            `gorm:"column:config_version" json:"-"`
	SyncEnabled              bool             `gorm:"column:sync_enabled" json:"-"`
	NextSyncAtMS             int64            `gorm:"column:next_sync_at_ms" json:"-"`
	FailureCount             int64            `gorm:"column:failure_count" json:"-"`
	CurrentSnapshotID        *string          `gorm:"column:current_snapshot_id" json:"-"`
	IncludedAtMS             *int64           `gorm:"column:included_at_ms" json:"-"`
	InitialStatisticalEquity *decimal.Decimal `gorm:"column:initial_statistical_equity;type:text" json:"-"`
	RemovedAtMS              *int64           `gorm:"column:removed_at_ms" json:"-"`
	CreatedAtMS              int64            `gorm:"column:created_at_ms" json:"-"`
}

func (Account) TableName() string { return "accounts" }

type SyncRun struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	AccountID         string  `gorm:"column:account_id" json:"account_id"`
	ServerID          string  `gorm:"column:server_id" json:"-"`
	EndpointID        string  `gorm:"column:endpoint_id" json:"-"`
	CredentialVersion int64   `gorm:"column:credential_version" json:"-"`
	ConfigVersion     int64   `gorm:"column:config_version" json:"-"`
	Kind              string  `gorm:"column:kind" json:"kind"`
	State             string  `gorm:"column:state" json:"state"`
	DueAtMS           int64   `gorm:"column:due_at_ms" json:"due_at_ms"`
	NotBeforeMS       int64   `gorm:"column:not_before_ms" json:"not_before_ms"`
	StartedAtMS       *int64  `gorm:"column:started_at_ms" json:"started_at_ms"`
	FinishedAtMS      *int64  `gorm:"column:finished_at_ms" json:"finished_at_ms"`
	Attempt           int64   `gorm:"column:attempt" json:"-"`
	ErrorCode         *string `gorm:"column:error_code" json:"error_code"`
	ErrorOperation    *string `gorm:"column:error_operation" json:"error_operation"`
	RawStatus         *int    `gorm:"column:raw_status" json:"raw_status"`
	CompletionReason  *string `gorm:"column:completion_reason" json:"completion_reason"`
}

func (SyncRun) TableName() string { return "sync_runs" }

type Snapshot struct {
	ID                string           `gorm:"column:id;primaryKey" json:"id"`
	AccountID         string           `gorm:"column:account_id" json:"account_id"`
	SyncRunID         string           `gorm:"column:sync_run_id" json:"-"`
	ObservedFromMS    int64            `gorm:"column:observed_from_ms" json:"observed_from_ms"`
	ObservedToMS      int64            `gorm:"column:observed_to_ms" json:"observed_to_ms"`
	NativeCurrency    string           `gorm:"column:native_currency" json:"native_currency"`
	USDScale          *decimal.Decimal `gorm:"column:usd_scale;type:text" json:"usd_scale"`
	UnitRevision      int64            `gorm:"column:unit_revision" json:"unit_revision"`
	Balance           decimal.Decimal  `gorm:"column:balance;type:text" json:"balance"`
	Credit            decimal.Decimal  `gorm:"column:credit;type:text" json:"credit"`
	Equity            decimal.Decimal  `gorm:"column:equity;type:text" json:"equity"`
	FloatingNetProfit decimal.Decimal  `gorm:"column:floating_net_profit;type:text" json:"floating_net_profit"`
	Margin            *decimal.Decimal `gorm:"column:margin;type:text" json:"margin"`
	MarginAvailable   bool             `gorm:"column:margin_available" json:"margin_available"`
	Leverage          uint32           `gorm:"column:leverage" json:"leverage"`
}

func (Snapshot) TableName() string { return "account_snapshots" }

type Position struct {
	AccountID        string          `gorm:"column:account_id;primaryKey" json:"-"`
	Ticket           int32           `gorm:"column:ticket;primaryKey" json:"ticket"`
	SnapshotID       string          `gorm:"column:snapshot_id" json:"-"`
	Symbol           string          `gorm:"column:symbol" json:"symbol"`
	Command          int32           `gorm:"column:command" json:"command"`
	Digits           int32           `gorm:"column:digits" json:"digits"`
	VolumeHundredths int32           `gorm:"column:volume_hundredths" json:"volume_hundredths"`
	OpenTimeWall     uint32          `gorm:"column:open_time_wall" json:"open_time_wall"`
	CloseTimeWall    uint32          `gorm:"column:close_time_wall" json:"close_time_wall"`
	OpenPrice        decimal.Decimal `gorm:"column:open_price;type:text" json:"open_price"`
	CurrentPrice     decimal.Decimal `gorm:"column:current_price;type:text" json:"current_price"`
	SL               decimal.Decimal `gorm:"column:sl;type:text" json:"sl"`
	TP               decimal.Decimal `gorm:"column:tp;type:text" json:"tp"`
	Profit           decimal.Decimal `gorm:"column:profit;type:text" json:"profit"`
	Commission       decimal.Decimal `gorm:"column:commission;type:text" json:"commission"`
	Swap             decimal.Decimal `gorm:"column:swap;type:text" json:"swap"`
	Taxes            decimal.Decimal `gorm:"column:taxes;type:text" json:"taxes"`
}

func (Position) TableName() string { return "positions" }

func (p Position) NetProfit() decimal.Decimal {
	return p.Profit.Add(p.Commission).Add(p.Swap).Add(p.Taxes)
}
