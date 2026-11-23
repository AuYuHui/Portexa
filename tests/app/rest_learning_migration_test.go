package app_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"portexa/internal/storage"
)

func stripMT5Schema(t *testing.T, db *sql.DB) {
	t.Helper()
	stripCollectionEndpointSchema(t, db)
	for _, statement := range []string{
		"DROP INDEX history_mt5_deal_page",
		"ALTER TABLE history_order_revisions DROP COLUMN mt5_deal_type",
		"ALTER TABLE history_order_revisions DROP COLUMN mt5_entry",
		"ALTER TABLE history_order_revisions DROP COLUMN mt5_order_ticket",
		"ALTER TABLE history_order_revisions DROP COLUMN mt5_position_id",
		"ALTER TABLE history_order_revisions DROP COLUMN mt5_wall_time_ms",
		"ALTER TABLE history_order_revisions DROP COLUMN exact_lots",
		"ALTER TABLE history_order_revisions DROP COLUMN fee",
		"ALTER TABLE positions DROP COLUMN trigger_price",
		"ALTER TABLE positions DROP COLUMN exact_lots",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(statement, err)
		}
	}
}

func stripRestLearningSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stripMT5Schema(t, db)
	for _, q := range []string{
		"DROP TABLE market_activity_observations", "DROP TABLE market_learning_events", "DROP TABLE market_learning_models", "DROP TABLE market_learning_groups", "DROP TABLE market_rest_windows",
		"ALTER TABLE account_snapshots DROP COLUMN rest_window_id", "ALTER TABLE market_rest_policies DROP COLUMN learning_enabled", "ALTER TABLE market_rest_policies DROP COLUMN learning_epoch_ms",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
}
func TestRestLearningMigrationPreservesOldSnapshotsAndPolicy(t *testing.T) {
	f, a, now, start := restFixture(t)
	*now = start
	restSnapshot(t, f, a, start, "XAUUSD.c", 2)
	*now = start.Add(60000000000)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 2)
	path := filepath.Join(t.TempDir(), "synthetic-v11.sqlite")
	if err := f.Store.Backup(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stripMT5Schema(t, db)
	// Keep the old rest verification and policy intact while removing only v12.
	if _, err = db.Exec("ALTER TABLE account_snapshots DROP COLUMN rest_window_id"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TABLE market_activity_observations", "DROP TABLE market_learning_events", "DROP TABLE market_learning_models", "DROP TABLE market_learning_groups", "DROP TABLE market_rest_windows", "ALTER TABLE market_rest_policies DROP COLUMN learning_enabled", "ALTER TABLE market_rest_policies DROP COLUMN learning_epoch_ms", "PRAGMA user_version=11"} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var rows []storage.Snapshot
	if err = store.Read.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[1].RestVerified {
		t.Fatal("lost prior rest proof")
	}
	for _, r := range rows {
		if r.RestWindowID != "" {
			t.Fatal("invented learned history")
		}
	}
	var policy storage.MarketRestPolicy
	if err = store.Read.First(&policy, "revision=?", rows[1].RestPolicyRevision).Error; err != nil || policy.LearningEnabled {
		t.Fatal("rewrote old policy", err)
	}
	var count int64
	store.Read.Model(&storage.MarketActivityObservation{}).Count(&count)
	if count != 0 {
		t.Fatal("invented quote evidence")
	}
}
