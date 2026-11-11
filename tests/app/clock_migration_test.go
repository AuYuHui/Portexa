package app_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"portexa/internal/storage"
)

func TestTradeClockMigrationPreservesVersionThreeData(t *testing.T) {
	path := legacyDatabase(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"002_server_directory.sql", "003_daily_reports.sql"} {
		schema, err := os.ReadFile("../../internal/storage/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO servers(id,protocol,broker_name,server_name,created_at_ms)
		VALUES('synthetic-v3-server','mt4','Synthetic','Synthetic-V3',1);
		INSERT INTO server_endpoints(id,server_id,revision,ip_address,port,state,created_at_ms)
		VALUES('synthetic-v3-endpoint','synthetic-v3-server',1,'203.0.113.9',443,'pending',1);
		INSERT INTO accounts(id,server_id,pinned_endpoint_id,login,label,currency,credential_envelope,credential_key_id,credential_version,created_at_ms,daily_sync_paused,daily_failure_count)
		VALUES('synthetic-v3-account','synthetic-v3-server','synthetic-v3-endpoint',7960,'合成版本三账户','USC',X'0102','synthetic-key',1,1,1,3);
		PRAGMA user_version=3;`); err != nil {
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
	var account storage.Account
	if err := store.Read.First(&account, "id = 'synthetic-v3-account'").Error; err != nil ||
		account.Label != "合成版本三账户" || account.Currency == nil || *account.Currency != "USC" ||
		string(account.CredentialEnvelope) != "\x01\x02" || !account.DailySyncPaused || account.DailyFailureCount != 3 ||
		account.NextClockSyncAtMS != 0 || account.ClockFailureCount != 0 || account.ClockSyncPaused {
		t.Fatal("clock migration changed existing private configuration", err)
	}
	settings, err := store.Auth(context.Background())
	if err != nil || settings.AuthVersion != 7 || settings.PasswordHash != "synthetic-preserved-hash" {
		t.Fatal("clock migration changed authentication", err)
	}
	var count int64
	for _, model := range []any{&storage.ClockObservation{}, &storage.ClockCalibration{}, &storage.ClockRun{}} {
		if err := store.Read.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("migration invented clock evidence", err)
		}
	}
	var violations []struct{ Table string }
	if err := store.Read.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) != 0 {
		t.Fatal("clock migration broke foreign keys", err)
	}
}

func TestTradeClockRestoreInterruptsCalibrationAndPausesCollection(t *testing.T) {
	now := time.Now().Add(-2 * time.Minute)
	f, account, _ := pendingClockFixture(t, &now)
	ctx := context.Background()
	job, err := f.Accounts.ClaimClock(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	backup, destination := filepath.Join(t.TempDir(), "backup.sqlite"), filepath.Join(t.TempDir(), "restored.sqlite")
	if err := f.Store.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := storage.RestoreNew(ctx, backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var run storage.ClockRun
	if err := restored.Read.First(&run, "id = ?", job.Run.ID).Error; err != nil || run.State != "interrupted" {
		t.Fatal("restore retained the running calibration lock", err)
	}
	var current storage.Account
	if err := restored.Read.First(&current, "id = ?", account.ID).Error; err != nil || current.SyncEnabled || current.ConfigVersion == account.ConfigVersion {
		t.Fatal("restore retained collection authorization or old configuration", err)
	}
}
