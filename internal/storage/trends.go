package storage

type AccountUnitChange struct {
	AccountID     string `gorm:"primaryKey"`
	UnitRevision  int64  `gorm:"primaryKey"`
	EffectiveAtMS int64
}

func (AccountUnitChange) TableName() string { return "account_unit_changes" }
