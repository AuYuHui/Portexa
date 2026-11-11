package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"portexa/internal/httpapi"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func decodeLogs(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			return records
		} else if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestHTTPLogsUseRoutePatternsAndRedactedFields(t *testing.T) {
	f := testutil.New(t, true)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	server, err := httpapi.New(f.Auth, f.Accounts, noopScheduler{}, httpapi.Config{Origin: "https://portexa.test", Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginHTTP(t, server)
	pathSecret, querySecret := "synthetic-private-account-id", "synthetic-query-secret"
	w := request(server, http.MethodGet, "/api/accounts/"+pathSecret+"?token="+querySecret, "", cookie, csrf, "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	request(server, http.MethodPost, "/api/auth/login", `{"password":"synthetic-wrong-secret"}`, nil, "", "https://portexa.test")
	request(server, http.MethodGet, "/missing-static", "", nil, "", "")
	request(server, "SYNTHETIC_SECRET_METHOD", "/"+pathSecret, "", nil, "", "https://portexa.test")
	for _, secret := range []string{testutil.Password, "synthetic-wrong-secret", pathSecret, querySecret, cookie.Value, csrf, "127.0.0.1:45678", "https://portexa.test"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("HTTP logs exposed request data")
		}
	}
	records := decodeLogs(t, output.Bytes())
	if len(records) != 5 {
		t.Fatal("missing HTTP log records", len(records))
	}
	for _, record := range records {
		if record["msg"] != "http_request" || record["component"] != "http" || record["duration_ms"].(float64) < 0 {
			t.Fatal("invalid HTTP log record")
		}
	}
	if record := records[1]; record["route"] != "GET /api/accounts/{id}" || record["status"] != float64(w.Code) || record["bytes"] != float64(w.Body.Len()) || record["level"] != "WARN" {
		t.Fatal("route, status, or byte count is incorrect", record)
	}
	if records[3]["status"] != float64(503) || records[3]["level"] != "ERROR" || records[4]["method"] != "OTHER" {
		t.Fatal("status severity or method sanitization is incorrect")
	}
}

func TestSyncLogsContainOutcomeWithoutConnectionOrAccountData(t *testing.T) {
	for _, success := range []bool{true, false} {
		name := "failure"
		if success {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			f := testutil.New(t, false)
			server, endpoint := f.Server(t, "Synthetic-Log-Server")
			account := f.Account(t, server, endpoint, 81234567, "USD")
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			fetch := func(context.Context, mt4.Config, mt4.Options) (mt4.Result, error) {
				if success {
					return testutil.Result("USD"), nil
				}
				return mt4.Result{}, errors.New("synthetic-payload-secret " + testutil.InvestorPassword + " 203.0.113.9")
			}
			engine := syncer.New(f.Accounts, fetch, syncer.Options{Interval: time.Hour, Logger: logger})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- engine.Run(ctx) }()
			stopped := false
			t.Cleanup(func() {
				cancel()
				if !stopped {
					<-done
				}
			})
			eventually(t, func() bool {
				detail, err := f.Accounts.Detail(context.Background(), account.ID, "")
				return err == nil && detail.Account.LastRun != nil && detail.Account.LastRun.FinishedAtMS != nil
			})
			cancel()
			err := <-done
			stopped = true
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{testutil.InvestorPassword, "synthetic-payload-secret", "203.0.113.9", "81234567", "Synthetic-Log-Server", "EURUSD"} {
				if strings.Contains(output.String(), secret) {
					t.Fatal("sync logs exposed credentials, targets, payloads, or positions")
				}
			}
			records := decodeLogs(t, output.Bytes())
			want := "sync_failed"
			if success {
				want = "sync_completed"
			}
			if len(records) != 2 || records[0]["msg"] != "sync_started" || records[1]["msg"] != want || records[1]["account_id"] != account.ID || records[1]["run_id"] == "" {
				t.Fatal("missing sync outcome", records)
			}
			if !success && (records[1]["code"] != "snapshot_invalid" || records[1]["level"] != "WARN") {
				t.Fatal("missing safe failure classification")
			}
		})
	}
}
