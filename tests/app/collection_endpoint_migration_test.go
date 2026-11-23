package app_test

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func stripCollectionEndpointSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"DROP TRIGGER analytics_clock_collection_endpoint",
		"ALTER TABLE accounts DROP COLUMN last_collection_endpoint_id",
		"ALTER TABLE accounts DROP COLUMN backup_endpoint_ids",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(statement, err)
		}
	}
}

func TestCollectionEndpointMigrationPreservesExistingCredential(t *testing.T) {
	f := testutil.New(t, false)
	server, primary := f.Server(t, "Synthetic-Old-Endpoint")
	account := f.Account(t, server, primary, 8103, "USD")
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "synthetic-v19.sqlite")
	if err := f.Store.Backup(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stripCollectionEndpointSchema(t, db)
	if _, err = db.Exec("PRAGMA user_version=19"); err != nil {
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
	service := accounts.New(store, f.Vault, f.Accounts.AllowTarget)
	job, err := service.Claim(ctx, account.ID)
	if err != nil || !bytes.Equal(job.Account.CredentialEnvelope, account.CredentialEnvelope) ||
		job.Run.EndpointID != primary.ID || job.Config.Password != testutil.InvestorPassword || len(job.Backups) != 0 {
		t.Fatalf("migration did not preserve the old account binding and credential: %v", err)
	}
}
