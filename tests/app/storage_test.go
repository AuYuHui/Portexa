package app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/internal/vault"
	"portexa/tests/testutil"
)

func TestSQLiteConstraintsAndAtomicSnapshots(t *testing.T) {
	f := testutil.New(t, false)
	t.Log("SQLite", f.Store.Version())
	ctx := context.Background()
	s1, e1 := f.Server(t, "Synthetic-One")
	s2, e2 := f.Server(t, "Synthetic-Two")
	a1 := f.Account(t, s1, e1, 1001, "USD")
	_ = f.Account(t, s2, e2, 1001, "USC") // Login is unique only within a server.
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Model(&storage.Account{}).Where("id = ?", a1.ID).Update("pinned_endpoint_id", e2.ID).Error
	}); err == nil {
		t.Fatal("cross-server endpoint accepted")
	}
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()
		return tx.Create(&storage.SyncRun{ID: storage.NewID(), AccountID: a1.ID, ServerID: s1.ID, EndpointID: e2.ID, Kind: "snapshot", State: "running", Attempt: 1, StartedAtMS: &now}).Error
	}); err == nil {
		t.Fatal("cross-server run accepted")
	}
	job, err := f.Accounts.Claim(ctx, a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	snapshot, positions, err := syncer.Convert(job, testutil.Result("USD"), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.Publish(ctx, job, snapshot, positions, now); err != nil {
		t.Fatal(err)
	}
	job2, err := f.Accounts.Claim(ctx, a1.ID)
	if err != nil {
		t.Fatal(err)
	}
	broken, positions2, _ := syncer.Convert(job2, testutil.Result("USD"), now, now+1)
	positions2 = append(positions2, positions2[0]) // Force rollback after snapshot insert and old-position delete.
	if err = f.Accounts.Publish(ctx, job2, broken, positions2, now); err == nil {
		t.Fatal("duplicate position published")
	}
	detail, err := f.Accounts.Detail(ctx, a1.ID, "")
	if err != nil || detail.Account.Snapshot.ID != snapshot.ID || len(detail.Positions) != 1 {
		t.Fatal("rollback lost previous snapshot", err)
	}
	var count int64
	f.Store.Read.Model(&storage.Snapshot{}).Where("id = ?", broken.ID).Count(&count)
	if count != 0 {
		t.Fatal("partial snapshot survived rollback")
	}
	if detail.Account.Snapshot.FloatingNetProfit.String() != "12.12" {
		t.Fatal("decimal addition", detail.Account.Snapshot.FloatingNetProfit)
	}
	if _, err = f.Accounts.Detail(ctx, a1.ID, "profit"); err == nil {
		t.Fatal("amount sorting accepted")
	}
}

func TestBackupRestoreAndKeyIsolation(t *testing.T) {
	f := testutil.New(t, true)
	ctx := context.Background()
	server, endpoint := f.Server(t, "Synthetic-Backup")
	account := f.Account(t, server, endpoint, 1002, "USD")
	token, _, err := f.Auth.Login(ctx, testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = f.Store.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.Backup(ctx, backup); err == nil {
		t.Fatal("backup overwrote existing file")
	}
	restoredPath := filepath.Join(t.TempDir(), "db", "restored.sqlite")
	if err = storage.RestoreNew(ctx, backup, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var restoredAccount storage.Account
	if err = restored.Read.First(&restoredAccount, "id = ?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restoredAccount.SyncEnabled {
		t.Fatal("restore enabled real connections")
	}
	other, _ := vault.New([]byte(strings.Repeat("b", 32)))
	if _, err = other.Open(restoredAccount.CredentialEnvelope, restoredAccount.CredentialKeyID, vault.AAD(account.ID, server.ID, endpoint.ID, 1)); err == nil {
		t.Fatal("wrong key decrypted account")
	}
	service := accounts.New(restored, f.Vault, nil)
	list, err := service.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal("restore lost account", err)
	}
	if _, err = testAuth(restored).Check(ctx, token); err == nil {
		t.Fatal("restore reused old session")
	}
}
