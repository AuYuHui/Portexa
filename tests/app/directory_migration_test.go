package app_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"portexa/internal/storage"
)

func legacyDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../../internal/storage/migrations/001_phase1.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=1; INSERT INTO auth_settings(id,password_hash,auth_version,changed_at_ms) VALUES(1,'synthetic-preserved-hash',7,123)", storage.ApplicationID)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertDirectoryUpgrade(t *testing.T, path string) {
	t.Helper()
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, err := store.Auth(context.Background())
	if err != nil || settings.PasswordHash != "synthetic-preserved-hash" || settings.AuthVersion != 7 {
		t.Fatal("upgrade changed login settings", err)
	}
	var version int
	if err = store.Read.Raw("PRAGMA user_version").Scan(&version).Error; err != nil || version != 2 {
		t.Fatal("schema version", version, err)
	}
	var count int64
	if err = store.Read.Table("server_directory_evidence").Count(&count).Error; err != nil {
		t.Fatal("directory migration missing", err)
	}
}

func TestDirectoryMigrationPreservesVersionOneLogin(t *testing.T) {
	assertDirectoryUpgrade(t, legacyDatabase(t))
}

func TestRestoreVersionOneBackupUpgradesBeforeUse(t *testing.T) {
	backup := legacyDatabase(t)
	target := filepath.Join(t.TempDir(), "db", "restored.sqlite")
	if err := storage.RestoreNew(context.Background(), backup, target); err != nil {
		t.Fatal(err)
	}
	assertDirectoryUpgrade(t, target)
}
