package app_test

import (
	"database/sql"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"

	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

// Build genuine pre-rest synthetic schemas, rather than only downgrading the
// user_version number while leaving the new tables/columns in place.
func stripMarketRestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"ALTER TABLE collection_settings DROP COLUMN market_rest_policy_revision",
		"ALTER TABLE account_snapshots DROP COLUMN rest_policy_revision",
		"ALTER TABLE account_snapshots DROP COLUMN rest_verified",
		"ALTER TABLE account_snapshots DROP COLUMN state_fingerprint",
		"ALTER TABLE accounts DROP COLUMN rest_blocked_until_ms",
		"DROP TRIGGER IF EXISTS analytics_rest_policy",
		"DROP TABLE market_rest_policies",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(statement, err)
		}
	}
}

func TestMarketRestMigrationPreservesSourcesAndDoesNotInventEvidence(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	*now = start
	analyticsSnapshot(t, f, a, start, 1000, 0)
	path := filepath.Join(t.TempDir(), "synthetic-v10.sqlite")
	if err := f.Store.Backup(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stripMarketRestSchema(t, db)
	if _, err = db.Exec("UPDATE collection_settings SET snapshot_interval_seconds=120,updated_at_ms=123"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=10"); err != nil {
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
	var settings storage.CollectionSettings
	if err = store.Read.First(&settings, "id=1").Error; err != nil || settings.SnapshotIntervalSeconds != 120 || settings.UpdatedAtMS != 123 {
		t.Fatal("lost collection settings", settings, err)
	}
	var policy storage.MarketRestPolicy
	if err = store.Read.First(&policy, "revision=?", settings.MarketRestPolicyRevision).Error; err != nil || policy.Template != marketrest.Template || policy.EffectiveAtMS <= 0 {
		t.Fatal(policy, err)
	}
	var restored storage.Account
	if err = store.Read.First(&restored, "id=?", a.ID).Error; err != nil || string(restored.CredentialEnvelope) != string(a.CredentialEnvelope) {
		t.Fatal("changed credentials", err)
	}
	var count int64
	if err = store.Read.Model(&storage.Snapshot{}).Where("rest_verified=1 OR rest_policy_revision<>0 OR state_fingerprint<>''").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("invented historical proof", count, err)
	}
	err = store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE market_rest_policies SET template='off' WHERE revision=1").Error
	})
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatal("mutable historical rest rule", err)
	}
	var problems []map[string]any
	if err = store.Read.Raw("PRAGMA foreign_key_check").Scan(&problems).Error; err != nil || len(problems) > 0 {
		t.Fatal(problems, err)
	}
}
