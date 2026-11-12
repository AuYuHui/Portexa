package app_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"portexa/internal/accounts"
	"portexa/internal/storage"
)

// Used only when constructing an older synthetic fixture from a current backup.
func stripAnalyticsSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'analytics_%'")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if _, err := db.Exec(`DROP TRIGGER "` + strings.ReplaceAll(name, `"`, `""`) + `"`); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"analytics_transfer_matches", "analytics_revision"} {
		if _, err := db.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnalyticsMigrationPreservesSourcesManualClassificationsAndCredentials(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	classify(t, f, a.ID, 6, 1, 0, "deposit")
	path := filepath.Join(t.TempDir(), "synthetic-version-eight.sqlite")
	if err := f.Store.Backup(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stripAnalyticsSchema(t, db)
	if _, err := db.Exec("PRAGMA user_version=8"); err != nil {
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
	var classification storage.CashFlowClassification
	if err := store.Read.Where("account_id=? AND ticket=6", a.ID).First(&classification).Error; err != nil || classification.Category != "deposit" || classification.Revision != 1 {
		t.Fatal("classification changed", classification, err)
	}
	var restored storage.Account
	if err := store.Read.First(&restored, "id=?", a.ID).Error; err != nil || string(restored.CredentialEnvelope) != string(a.CredentialEnvelope) {
		t.Fatal("credentials changed", err)
	}
	service := accounts.New(store, f.Vault, f.Accounts.AllowTarget)
	if _, err := service.ClassifyFund(t.Context(), a.ID, 6, accounts.ClassificationInput{TradeRevision: 1, ClassificationRevision: 1, Category: "capital_adjustment", Reason: "合成资本纠错核对"}); err != nil {
		t.Fatal(err)
	}
	var problems []struct{ Table string }
	if err := store.Read.Raw("PRAGMA foreign_key_check").Scan(&problems).Error; err != nil || len(problems) != 0 {
		t.Fatal("migration damaged references", problems, err)
	}
}
