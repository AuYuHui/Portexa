package storage

type MarketLearningGroup struct {
	ID               string `gorm:"primaryKey" json:"id"`
	ServerID         string `json:"server_id"`
	EndpointID       string `json:"-"`
	Symbol           string `json:"symbol"`
	EpochMS          int64  `gorm:"column:epoch_ms" json:"-"`
	LastObservedMS   int64  `gorm:"column:last_observed_ms" json:"last_observed_ms"`
	NextReviewMS     int64  `gorm:"column:next_review_ms" json:"-"`
	ReviewedAtMS     int64  `gorm:"column:reviewed_at_ms" json:"reviewed_at_ms"`
	ValidWeekends    int    `json:"valid_weekends"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
	CandidateModelID string `json:"-"`
	ActiveModelID    string `json:"-"`
}

func (MarketLearningGroup) TableName() string { return "market_learning_groups" }

type MarketActivityObservation struct {
	ID                                                                 int64 `gorm:"primaryKey"`
	GroupID, AccountID, SnapshotID, PreviousSnapshotID, SourceRunID    string
	ConfigVersion, UnitRevision                                        int64
	ObservedFromMS                                                     int64 `gorm:"column:observed_from_ms"`
	ObservedToMS                                                       int64 `gorm:"column:observed_to_ms"`
	PreviousToMS                                                       int64 `gorm:"column:previous_to_ms"`
	MaxGapMS                                                           int64 `gorm:"column:max_gap_ms"`
	Healthy, Comparable, QuoteChanged, PositionChanged, AccountChanged bool
	QuotePrice, QuoteAnchor                                            string
}

func (MarketActivityObservation) TableName() string { return "market_activity_observations" }

type MarketLearningModel struct {
	ID               string `gorm:"primaryKey" json:"id"`
	GroupID          string `json:"-"`
	Algorithm        string `json:"algorithm"`
	CreatedAtMS      int64  `gorm:"column:created_at_ms" json:"created_at_ms"`
	TrainedThroughMS int64  `gorm:"column:trained_through_ms" json:"trained_through_ms"`
	ExpiresAtMS      int64  `gorm:"column:expires_at_ms" json:"expires_at_ms"`
	CloseMinute      int    `json:"close_minute"`
	OpenMinute       int    `json:"open_minute"`
	CloseMinMinute   int    `json:"close_min_minute"`
	CloseMaxMinute   int    `json:"close_max_minute"`
	OpenMinMinute    int    `json:"open_min_minute"`
	OpenMaxMinute    int    `json:"open_max_minute"`
	TrainingKeysJSON string `gorm:"column:training_keys_json" json:"-"`
	EvidenceSHA256   string `gorm:"column:evidence_sha256" json:"evidence_sha256"`
}

func (MarketLearningModel) TableName() string { return "market_learning_models" }

type MarketLearningEvent struct {
	ID             int64 `gorm:"primaryKey"`
	ModelID, State string
	EffectiveAtMS  int64 `gorm:"column:effective_at_ms"`
	BlockedUntilMS int64 `gorm:"column:blocked_until_ms"`
	Reason         string
	SourceRunID    *string
	ValidationKey  string
}

func (MarketLearningEvent) TableName() string { return "market_learning_events" }

type MarketRestWindow struct {
	ID             string `gorm:"primaryKey"`
	AccountID      string
	PolicyRevision int64
	Kind           string
	FromMS         int64  `gorm:"column:from_ms"`
	UntilMS        int64  `gorm:"column:until_ms"`
	EffectiveAtMS  int64  `gorm:"column:effective_at_ms"`
	ModelIDsJSON   string `gorm:"column:model_ids_json"`
}

func (MarketRestWindow) TableName() string { return "market_rest_windows" }
