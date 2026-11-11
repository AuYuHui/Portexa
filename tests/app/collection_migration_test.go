package app_test

import (
	"database/sql"
	"os"
	"testing"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
)

func TestCollectionSettingsUpgradeFromVersionSix(t *testing.T) {
	path := legacyDatabase(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"002_server_directory.sql", "003_daily_reports.sql", "004_trade_clock.sql", "005_account_statistics.sql", "006_history_comments.sql"} {
		schema, err := os.ReadFile("../../internal/storage/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version=6"); err != nil {
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
	service := accounts.New(store, nil, nil)
	settings, err := service.CollectionSettings(t.Context())
	if err != nil || settings.SnapshotIntervalSeconds != 60 {
		t.Fatal("upgrade did not retain the previous default", settings, err)
	}
	var auth storage.AuthSettings
	if err := store.Read.First(&auth, "id=1").Error; err != nil || auth.PasswordHash != "synthetic-preserved-hash" || auth.AuthVersion != 7 {
		t.Fatal("upgrade changed login settings", err)
	}
	if err := store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&settings).Update("snapshot_interval_seconds", 10).Error
	}); err == nil {
		t.Fatal("database accepted an unsupported cycle")
	}
	if err := store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Create(&storage.CollectionSettings{ID: 2, SnapshotIntervalSeconds: 30}).Error
	}); err == nil {
		t.Fatal("database accepted a second global configuration")
	}
}
