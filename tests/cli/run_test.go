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
	input := fmt.Sprintf(`{"target":%q,"server":%q,"login":%d,"password":%q,"expected_account":{"balance":1234.50,"credit":6.25,"leverage":100}}`, address, support.Server, support.Login, support.Password)
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
	for _, match := range report["local_reference_matches"].(map[string]any) {
		if match != true {
			t.Fatal("local reference comparison did not match the fixture")
		}
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
