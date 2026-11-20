package accounts

import (
	"context"
	"slices"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

func (s *Service) DeleteAccount(ctx context.Context, id string) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.Select("id", "server_id").First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		now := s.Now()
		if err := tx.Model(&account).Updates(map[string]any{
			"removed_at_ms":       now.UnixMilli(),
			"sync_enabled":        false,
			"credential_envelope": gorm.Expr("X''"),
			"credential_key_id":   "",
			"config_version":      gorm.Expr("config_version + 1"),
		}).Error; err != nil {
			return err
		}
		settings, err := readCollectionSettings(tx)
		if err != nil {
			return err
		}
		if slices.Contains(settings.MarketRestExcludedAccountIDs, id) {
			excluded := slices.DeleteFunc(slices.Clone(settings.MarketRestExcludedAccountIDs), func(candidate string) bool {
				return candidate == id
			})
			if _, err := updateRestPolicy(tx, &settings, CollectionInput{MarketRestExcludedAccountIDs: &excluded}, now.UnixMilli()); err != nil {
				return err
			}
			settings.UpdatedAtMS = now.UnixMilli()
			if err := tx.Save(&settings).Error; err != nil {
				return err
			}
		}
		var server storage.Server
		if err := tx.First(&server, "id = ?", account.ServerID).Error; err != nil {
			return err
		}
		return refreshClockCalibration(tx, &server, now)
	})
}
