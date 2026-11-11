package storage

type CollectionSettings struct {
	ID                      int   `gorm:"column:id;primaryKey" json:"-"`
	SnapshotIntervalSeconds int   `gorm:"column:snapshot_interval_seconds" json:"snapshot_interval_seconds"`
	UpdatedAtMS             int64 `gorm:"column:updated_at_ms" json:"updated_at_ms"`
}

func (CollectionSettings) TableName() string { return "collection_settings" }
