package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"portexa/internal/cli"
	"portexa/mt4"
	"portexa/mt4/tests/support"
)

func TestCLIReportsCountsWithoutValuesOrCredentials(t *testing.T) {
	address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true})
	input := fmt.Sprintf(`{"target":%q,"server":%q,"login":%d,"password":%q,"password_type":"investor","expected_account":{"balance":1234.50,"credit":6.25,"leverage":100,"equity":1251.50,"margin":1250,"currency":"USD"}}`, address, support.Server, support.Login, support.Password)
	var output, diagnostics bytes.Buffer
	err := cli.Run(context.Background(), []string{"--authenticate", "--history-days", "2"}, strings.NewReader(input), &output, &diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{support.Password, "1234.5", "6.25", "TEST.USD"} {
		if strings.Contains(output.String()+diagnostics.String(), private) {
			t.Fatal("CLI exposed a fixture credential, value or order detail")
		}
	}
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["authentication_verified"] != true || report["history_records"] != float64(1) || report["trade_order_records"] != float64(1) {
		t.Fatal("CLI lost the SDK synchronization result")
	}
	if report["account_reference_verified"] != true || report["query_only_profile"] != true || report["investor_password_declared"] != true {
		t.Fatal("full reference verification or read-only declarations were lost")
	}
	for _, match := range report["local_reference_matches"].(map[string]any) {
		if match != true {
			t.Fatal("local reference comparison did not match the fixture")
		}
	}
}

func TestCLIWholeHistoryIncludesFutureServerWallClockAndClassifiesErrors(t *testing.T) {
	address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryBounds: [][2]uint32{{0, 2147483647}}})
	input := fmt.Sprintf(`{"target":%q,"server":%q,"login":%d,"password":%q,"password_type":"investor","expected_account":{"balance":1234.505005}}`, address, support.Server, support.Login, support.Password)
	var out, diagnostic bytes.Buffer
	if err := cli.Run(context.Background(), []string{"--authenticate", "--history-all"}, strings.NewReader(input), &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["account_reference_verified"] != false || report["local_reference_matches"].(map[string]any)["balance_at_two_decimals"] != false {
		t.Fatal("nearby amounts with different display rounding were accepted")
	}
	out.Reset()
	if err := cli.Run(context.Background(), nil, strings.NewReader(`{"target":"127.0.0.1:1","login":1,"password":"private-fixture"}`), &out, &diagnostic); err == nil {
		t.Fatal("missing investor declaration accepted")
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["error_code"] != "read_only_required" || report["error_operation"] != "config" || strings.Contains(out.String(), "private-fixture") {
		t.Fatal("early failure was not reported safely")
	}
}

func TestSummaryMapsCommandsAndPreservesPartialState(t *testing.T) {
	result := mt4.Result{SessionConfirmed: true, Trades: []mt4.Trade{{Command: 3}}}
	report := cli.Summary(result)
	if report["authentication_verified"] != false || report["account_money_fields_finite"] != false || report["trade_order_kinds"].(map[string]int)["sell_limit"] != 1 {
		t.Fatal("summary confused a partial result or a trade command")
	}
}

func TestCLIValidationDoesNotEchoInput(t *testing.T) {
	for _, input := range []string{`not-json-secret`, `{"unexpected":"fixture-secret"}`, `{"login":0,"password":"fixture-secret"}`} {
		var out, diagnostic bytes.Buffer
		err := cli.Run(context.Background(), nil, strings.NewReader(input), &out, &diagnostic)
		if err == nil || strings.Contains(err.Error()+out.String()+diagnostic.String(), "fixture-secret") {
			t.Fatal("invalid CLI input was accepted or echoed")
		}
	}
}
