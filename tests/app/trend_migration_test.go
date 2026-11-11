package app_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func TestAssetTrendUpgradeRetainsDataAndUnknownCadence(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-Trend-Upgrade")
	a := f.Account(t, server, endpoint, 9601, "USD")
	stamp := time.Now()
	snapshot := trendSample(t, f, a, stamp, 1234.56)
	path := filepath.Join(t.TempDir(), "legacy-seven.sqlite")
	if err := f.Store.Backup(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP TABLE account_unit_changes",
		"DROP INDEX snapshots_account_minute",
		"ALTER TABLE account_snapshots DROP COLUMN collection_interval_seconds",
		"PRAGMA user_version=7",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var restored storage.Snapshot
	if err := store.Read.First(&restored, "id=?", snapshot.ID).Error; err != nil ||
		restored.CollectionIntervalSeconds != nil || restored.Balance.String() != "1234.56" {
		t.Fatal("upgrade fabricated cadence or changed money", restored, err)
	}
	var account storage.Account
	if err := store.Read.First(&account, "id=?", a.ID).Error; err != nil || account.CredentialKeyID != a.CredentialKeyID ||
		account.ConfigVersion != a.ConfigVersion {
		t.Fatal("upgrade changed account configuration", err)
	}
	var change storage.AccountUnitChange
	if err := store.Read.First(&change, "account_id=?", a.ID).Error; err != nil ||
		change.UnitRevision != 1 || change.EffectiveAtMS != a.CreatedAtMS {
		t.Fatal("unit baseline not preserved", change, err)
	}
	var auth storage.AuthSettings
	if err := store.Read.First(&auth, "id=1").Error; err != nil || auth.PasswordHash == "" {
		t.Fatal("upgrade lost login settings", err)
	}
	if err := store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&restored).Update("collection_interval_seconds", 10).Error
	}); err == nil {
		t.Fatal("unsupported cadence accepted")
	}
}
