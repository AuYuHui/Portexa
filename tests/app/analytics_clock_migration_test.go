package app_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

func TestAnalyticsClockMigrationPreservesEvidenceAndFencesChanges(t *testing.T) {
	f, a, _, _ := calibratedAnalyticsFixture(t)
	path := filepath.Join(t.TempDir(), "synthetic-version-nine.sqlite")
	if err := f.Store.Backup(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"analytics_clock_insert", "analytics_clock_accept", "analytics_clock_delete", "analytics_clock_account", "analytics_clock_endpoint"} {
		if _, err := db.Exec("DROP TRIGGER " + name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version=9"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var restored storage.Account
	if err := store.Read.First(&restored, "id=?", a.ID).Error; err != nil || string(restored.CredentialEnvelope) != string(a.CredentialEnvelope) {
		t.Fatal("migration changed credentials", err)
	}
	var count int64
	if err := store.Read.Model(&storage.ClockObservation{}).Where("state='accepted'").Count(&count).Error; err != nil || count != 3 {
		t.Fatal("lost calibration evidence", count, err)
	}
	var before, after int64
	if err := store.Read.Table("analytics_revision").Where("id=1").Pluck("revision", &before).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Where("account_id=? AND ticket=?", a.ID, 71001).Delete(&storage.ClockObservation{}).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Read.Table("analytics_revision").Where("id=1").Pluck("revision", &after).Error; err != nil || after <= before {
		t.Fatal("calibration changes escaped the read fence", before, after, err)
	}
}
