package app_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"portexa/internal/storage"
)

func TestStatisticsMigrationKeepsOldDailyAmountsWithoutInventingWins(t *testing.T) {
	path := legacyDatabase(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"002_server_directory.sql", "003_daily_reports.sql", "004_trade_clock.sql"} {
		schema, err := os.ReadFile("../../internal/storage/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(schema)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`
		INSERT INTO servers(id,protocol,broker_name,server_name,created_at_ms)
		VALUES('statistics-server','mt4','Synthetic','Synthetic-Statistics',1);
		INSERT INTO server_endpoints(id,server_id,revision,ip_address,port,state,created_at_ms)
		VALUES('statistics-endpoint','statistics-server',1,'203.0.113.9',443,'pending',1);
		INSERT INTO server_time_rule_sets(id,server_id,revision,rules_json,created_at_ms)
		VALUES('statistics-rule','statistics-server',1,'[]',1);
		INSERT INTO accounts(id,server_id,pinned_endpoint_id,login,label,currency,credential_envelope,credential_key_id,credential_version,created_at_ms)
		VALUES('statistics-account','statistics-server','statistics-endpoint',7991,'合成旧日报账户','USC',X'0102','synthetic-key',1,1);
		INSERT INTO daily_reports(id,account_id,server_id,endpoint_id,config_version,unit_revision,time_ruleset_id,report_date,time_zone,
			window_start,window_end,state,started_at_ms,finished_at_ms,native_currency,net_profit,balance_adjustment,balance_change,credit_change,closed_trades)
		VALUES('statistics-daily','statistics-account','statistics-server','statistics-endpoint',1,1,'statistics-rule','2026-11-11','UTC',100,200,
			'succeeded',1000,1001,'USC','25.01','100','125.01','0',7);
		PRAGMA user_version=4;`); err != nil {
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
	var report storage.DailyReport
	if err = store.Read.First(&report, "id='statistics-daily'").Error; err != nil {
		t.Fatal(err)
	}
	if report.NetProfit.String() != "25.01" || report.BalanceChange.String() != "125.01" || report.ClosedTrades != 7 || report.WinningTrades != nil || report.LosingTrades != nil || report.GrossProfit != nil || report.GrossLoss != nil {
		t.Fatal("migration changed known money or invented win/loss details", report)
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(body, &data); err != nil || data["win_rate"] != nil {
		t.Fatal("legacy report has fabricated win rate", string(body), err)
	}
	var account storage.Account
	if err = store.Read.First(&account, "id='statistics-account'").Error; err != nil || account.Currency == nil || *account.Currency != "USC" || string(account.CredentialEnvelope) != "\x01\x02" || account.NextHistorySyncAtMS != 0 || account.HistorySyncPaused {
		t.Fatal("migration changed old account", err)
	}
	for _, model := range []any{&storage.HistoryOrder{}, &storage.HistoryRun{}, &storage.HistoryCoverage{}, &storage.CashFlowClassification{}} {
		var count int64
		if err = store.Read.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("migration invented source facts", err, count)
		}
	}
	var violations []struct{ Table string }
	if err = store.Read.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil || len(violations) > 0 {
		t.Fatal("migration broke foreign keys", err, violations)
	}
}
