package app_test

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func TestMT5NativeMigrationRetiresLegacyEndpoints(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	review := accounts.ReviewInput{EvidenceRef: "synthetic verification", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true}
	addAccount := func(name string, port int) storage.Account {
		t.Helper()
		server, endpoint, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{
			Protocol: "mt5", BrokerName: "Synthetic Broker", ServerName: name, IPAddress: "203.0.113.9", Port: port,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Accounts.Review(ctx, endpoint.ID, review); err != nil {
			t.Fatal(err)
		}
		return f.Account(t, server, endpoint, 123456, "USD")
	}
	legacy := addAccount("Synthetic-Legacy", 443)
	native := addAccount("Synthetic-Native", 2085)
	now := time.Now().UnixMilli()
	seedMT5Valuations(t, f, legacy, now-60_000, now)
	path := filepath.Join(t.TempDir(), "synthetic-v14.sqlite")
	if err := f.Store.Backup(ctx, path); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := old.Exec("UPDATE account_snapshots SET leverage=100, margin='64', margin_available=1 WHERE account_id=?", legacy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("ALTER TABLE server_endpoints ADD COLUMN web_hostname TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("UPDATE server_endpoints SET web_hostname = ? WHERE id = ?", "broker.example", legacy.PinnedEndpointID); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TRIGGER endpoint_web_hostname_is_immutable BEFORE UPDATE OF web_hostname ON server_endpoints
		BEGIN SELECT RAISE(ABORT, 'create a new endpoint revision'); END;
		PRAGMA user_version=14;`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var columns int
	if err := store.Read.Raw("SELECT COUNT(*) FROM pragma_table_info('server_endpoints') WHERE name = 'web_hostname'").Scan(&columns).Error; err != nil || columns != 0 {
		t.Fatalf("obsolete hostname column remains: %d, %v", columns, err)
	}
	var migrated storage.Account
	if err := store.Read.First(&migrated, "id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.SyncEnabled || migrated.PinnedEndpointID != legacy.PinnedEndpointID ||
		migrated.ConfigVersion != legacy.ConfigVersion+1 || migrated.CredentialVersion != legacy.CredentialVersion ||
		!bytes.Equal(migrated.CredentialEnvelope, legacy.CredentialEnvelope) {
		t.Fatal("legacy account was not paused with its credential and original binding preserved")
	}
	var snapshots int64
	if err := store.Read.Model(&storage.Snapshot{}).Where("account_id = ?", legacy.ID).Count(&snapshots).Error; err != nil || snapshots != 2 {
		t.Fatalf("previous snapshots were lost: %d, %v", snapshots, err)
	}
	var invalidated int64
	if err := store.Read.Model(&storage.Snapshot{}).Where("account_id = ? AND leverage = 0 AND margin IS NULL AND margin_available = 0", legacy.ID).
		Count(&invalidated).Error; err != nil || invalidated != snapshots {
		t.Fatalf("old MT5 estimates are still visible: %d invalidated, %v", invalidated, err)
	}
	var unchanged storage.Account
	if err := store.Read.First(&unchanged, "id = ?", native.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !unchanged.SyncEnabled || unchanged.PinnedEndpointID != native.PinnedEndpointID || unchanged.ConfigVersion != native.ConfigVersion {
		t.Fatal("migration changed an active native account")
	}

	service := accounts.New(store, f.Vault, f.Accounts.AllowTarget)
	if _, err := service.Review(ctx, legacy.PinnedEndpointID, review); !errors.Is(err, accounts.ErrEndpoint) {
		t.Fatalf("old endpoint allowed renewed approval: %v", err)
	}
	enabled := true
	if err := service.UpdateAccount(ctx, legacy.ID, accounts.AccountPatch{SyncEnabled: &enabled}); !errors.Is(err, accounts.ErrEndpoint) {
		t.Fatalf("old endpoint allowed collection to resume: %v", err)
	}
	endpoint, _, err := service.NewEndpoint(ctx, legacy.ServerID, accounts.EndpointInput{IPAddress: "203.0.113.10", Port: 2085})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Review(ctx, endpoint.ID, review); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateAccount(ctx, legacy.ID, accounts.AccountPatch{SyncEnabled: &enabled, EndpointID: &endpoint.ID}); err != nil {
		t.Fatalf("could not resume on a newly approved native endpoint: %v", err)
	}
	var rebound storage.Account
	if err := store.Read.First(&rebound, "id = ?", legacy.ID).Error; err != nil || !rebound.SyncEnabled || rebound.PinnedEndpointID != endpoint.ID {
		t.Fatalf("account did not switch to the new native endpoint: %v", err)
	}
}
