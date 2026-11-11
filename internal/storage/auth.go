package storage

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

func (s *Store) Auth(ctx context.Context) (AuthSettings, error) {
	var settings AuthSettings
	err := s.Read.WithContext(ctx).First(&settings, "id = ?", 1).Error
	return settings, err
}

// SetPassword initializes the singleton when expected is nil, or atomically
// changes the password and revokes sessions for the expected auth generation.
func (s *Store) SetPassword(ctx context.Context, hash string, expected *int64, now int64) error {
	return s.Write(ctx, High, func(tx *gorm.DB) error {
		var settings AuthSettings
		err := tx.First(&settings, "id = ?", 1).Error
		if expected == nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				if err != nil {
					return err
				}
				return ErrConflict
			}
			return tx.Create(&AuthSettings{ID: 1, PasswordHash: hash, AuthVersion: 1, ChangedAtMS: now}).Error
		}
		if err != nil {
			return err
		}
		if settings.AuthVersion != *expected {
			return ErrDenied
		}
		if err = tx.Model(&AuthSettings{}).Where("id = ?", 1).Updates(map[string]any{
			"password_hash": hash, "auth_version": settings.AuthVersion + 1, "changed_at_ms": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&Session{}).Where("revoked_at_ms IS NULL").Update("revoked_at_ms", now).Error
	})
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	return s.Write(ctx, High, func(tx *gorm.DB) error {
		var settings AuthSettings
		if err := tx.First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		if settings.AuthVersion != session.AuthVersion {
			return ErrDenied
		}
		return tx.Create(&session).Error
	})
}

func (s *Store) Session(ctx context.Context, hash []byte, now int64) (Session, error) {
	var session Session
	err := s.Read.WithContext(ctx).Table("sessions AS s").Select("s.*").
		Joins("JOIN auth_settings AS a ON a.id = 1 AND a.auth_version = s.auth_version").
		Where("s.token_sha256 = ? AND s.revoked_at_ms IS NULL AND s.idle_expires_at_ms > ? AND s.expires_at_ms > ?", hash, now, now).
		Take(&session).Error
	return session, err
}

func (s *Store) RevokeSession(ctx context.Context, hash []byte, now int64) error {
	return s.Write(ctx, High, func(tx *gorm.DB) error {
		return tx.Model(&Session{}).Where("token_sha256 = ? AND revoked_at_ms IS NULL", hash).Update("revoked_at_ms", now).Error
	})
}

func (s *Store) TouchSession(ctx context.Context, session Session, now, idleUntil int64) error {
	if idleUntil > session.ExpiresAtMS {
		idleUntil = session.ExpiresAtMS
	}
	return s.Write(ctx, High, func(tx *gorm.DB) error {
		result := tx.Model(&Session{}).Where("token_sha256 = ? AND revoked_at_ms IS NULL AND idle_expires_at_ms > ? AND expires_at_ms > ?", session.TokenSHA256, now, now).
			Updates(map[string]any{"last_interactive_at_ms": now, "idle_expires_at_ms": idleUntil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDenied
		}
		return nil
	})
}
