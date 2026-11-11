package app_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func TestLabelEditPreservesUnitEvidence(t *testing.T) {
	f := testutil.New(t, false)
	server, endpoint := f.Server(t, "Synthetic-Units")
	account := f.Account(t, server, endpoint, 5000, "USD")
	ctx := context.Background()
	job, err := f.Accounts.Claim(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	snapshot, positions, err := syncer.Convert(job, testutil.Result("USD"), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.Publish(ctx, job, snapshot, positions, now+60000); err != nil {
		t.Fatal(err)
	}
	label, currency, confirmed := "新标签", "USD", true
	if err = f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{Label: &label, ConfirmedCurrency: &currency, ConfirmUnit: &confirmed}); err != nil {
		t.Fatal(err)
	}
	detail, err := f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || !detail.Account.UnitConfirmed || detail.Account.Snapshot.UnitRevision != 1 {
		t.Fatal("label invalidated units", err)
	}
	currency = "USC"
	if err = f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{ConfirmedCurrency: &currency, ConfirmUnit: &confirmed}); err != nil {
		t.Fatal(err)
	}
	detail, err = f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || detail.Account.UnitConfirmed || !detail.Account.ConfiguredUnitConfirmed {
		t.Fatal("old snapshot got new units", err)
	}
}

func TestEveryReadConnectionHasPragmasAndIsReadOnly(t *testing.T) {
	f := testutil.New(t, false)
	db, err := f.Store.Read.DB()
	if err != nil {
		t.Fatal(err)
	}
	var held []*sql.Conn
	defer func() {
		for _, conn := range held {
			conn.Close()
		}
	}()
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, conn)
		var foreignKeys, busy int
		var mode string
		if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err = conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatal(err)
		}
		if err = conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || busy != 1000 || mode != "wal" {
			t.Fatal("connection pragmas", foreignKeys, busy, mode)
		}
		if _, err = conn.ExecContext(ctx, "CREATE TABLE forbidden(x INTEGER)"); err == nil {
			t.Fatal("read pool allowed writes")
		}
	}
}

func TestLatestRunAfterInterruptedRetryUsesStartedTime(t *testing.T) {
	f := testutil.New(t, false)
	now := time.Now()
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Restart")
	a := f.Account(t, server, endpoint, 5010, "USD")
	ctx := context.Background()
	first, err := f.Accounts.Claim(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err = f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := f.Accounts.Claim(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.DueAtMS != second.Run.DueAtMS {
		t.Fatal("fixture must exercise identical due times")
	}
	detail, err := f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.LastRun.ID != second.Run.ID {
		t.Fatal("displayed interrupted predecessor", err)
	}
	var plan []struct{ Detail string }
	if err = f.Store.Read.Raw("EXPLAIN QUERY PLAN SELECT * FROM sync_runs WHERE account_id = ? ORDER BY started_at_ms DESC, id DESC LIMIT 1", a.ID).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range plan {
		if strings.Contains(row.Detail, "TEMP B-TREE") {
			t.Fatal("latest run uses an unindexed sort", row.Detail)
		}
	}
}
