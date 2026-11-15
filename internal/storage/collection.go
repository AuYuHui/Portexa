package storage

type CollectionSettings struct {
	ID                           int      `gorm:"column:id;primaryKey" json:"-"`
	SnapshotIntervalSeconds      int      `gorm:"column:snapshot_interval_seconds" json:"snapshot_interval_seconds"`
	UpdatedAtMS                  int64    `gorm:"column:updated_at_ms" json:"updated_at_ms"`
	MarketRestPolicyRevision     int64    `gorm:"column:market_rest_policy_revision" json:"market_rest_policy_revision"`
	MarketRestTemplate           string   `gorm:"-" json:"market_rest_template"`
	MarketRestExcludedAccountIDs []string `gorm:"-" json:"market_rest_excluded_account_ids"`
	MarketRestLearningEnabled    bool     `gorm:"-" json:"market_rest_learning_enabled"`
	MarketRestLearningEpochMS    int64    `gorm:"-" json:"-"`
}

func (CollectionSettings) TableName() string { return "collection_settings" }

type MarketRestPolicy struct {
	Revision             int64  `gorm:"column:revision;primaryKey"`
	Template             string `gorm:"column:template"`
	EffectiveAtMS        int64  `gorm:"column:effective_at_ms"`
	ExcludedAccountsJSON string `gorm:"column:excluded_accounts_json"`
	LearningEnabled      bool   `gorm:"column:learning_enabled"`
	LearningEpochMS      int64  `gorm:"column:learning_epoch_ms"`
}

func (MarketRestPolicy) TableName() string { return "market_rest_policies" }
