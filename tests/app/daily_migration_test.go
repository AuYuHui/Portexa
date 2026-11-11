package app_test

import (
	"database/sql"
	"os"
	"testing"

	"portexa/internal/storage"
)

func TestDailyMigrationPreservesVersionTwoAccounts(t *testing.T) {
	path := legacyDatabase(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.ReadFile("../../internal/storage/migrations/002_server_directory.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(directory)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		INSERT INTO servers(id,protocol,broker_name,server_name,created_at_ms)
		VALUES('synthetic-server','mt4','Synthetic','Synthetic-V2',1);
		INSERT INTO server_endpoints(id,server_id,revision,ip_address,port,state,created_at_ms)
		VALUES('synthetic-endpoint','synthetic-server',1,'203.0.113.9',443,'pending',1);
		INSERT INTO accounts(id,server_id,pinned_endpoint_id,login,label,currency,credential_envelope,credential_key_id,credential_version,created_at_ms)
		VALUES('synthetic-account','synthetic-server','synthetic-endpoint',7201,'合成旧账户','USC',X'01','synthetic-key',1,1);
		PRAGMA user_version=2;
	`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var account storage.Account
	if err = store.Read.First(&account, "id = 'synthetic-account'").Error; err != nil || account.Label != "合成旧账户" ||
		account.Currency == nil || *account.Currency != "USC" || account.NextDailySyncAtMS != 0 || account.DailySyncPaused || account.DailyFailureCount != 0 {
		t.Fatal("migration changed existing account", err)
	}
	var violations []struct{ Table string }
	if err = store.Read.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) != 0 {
		t.Fatal("foreign keys after upgrade", err, violations)
	}
	var count int64
	if err = store.Read.Model(&storage.DailyReport{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("migration invented daily statistics", err)
	}
}
