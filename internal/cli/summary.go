package cli

import (
	"math"

	"portexa/mt4"
)

// Summary produces CLI diagnostics only. Values and identifiers remain in the SDK result.
func Summary(result mt4.Result) map[string]any {
	mode := "network-initial-only"
	if result.SessionConfirmed {
		mode = "network-session-confirmed"
	}
	if result.AccountRead {
		mode = "network-account-confirmed"
	}
	var records []map[string]any
	for _, record := range result.FeatureRecords {
		records = append(records, map[string]any{"type": record.Kind, "mode": record.Mode, "bytes": record.Bytes})
	}
	return map[string]any{
		"mode": mode, "initial_bytes_generated": result.InitialBytes,
		"first_response_decoded": result.FirstResponseDecoded, "server_build": result.ServerBuild,
		"inner_records": records, "authentication_verified": result.SessionConfirmed && result.AccountRead,
		"server_session_confirmed": result.SessionConfirmed,
		"server_config_bytes":      result.Server.ConfigBytes, "server_config_access_points": result.Server.AccessPoints,
		"broker_name_verified":     result.Server.Name != "",
		"account_response_decoded": result.AccountRead, "account_base_bytes": result.AccountBaseBytes,
		"account_money_fields_finite": result.AccountRead, "investor_indicator_set": result.Account.InvestorIndicator != 0,
		"margin_available":              result.Account.MarginAvailable,
		"read_only_permission_verified": false,
		"query_only_profile":            true,
		"trade_order_records":           len(result.Trades), "trade_order_trailing_bytes": result.TradeTrailingBytes,
		"trade_order_kinds": tradeKinds(result.Trades), "trade_time_checks": tradeTimeEvidence(result.Trades),
		"symbol_records": result.Symbols.Count, "group_records": result.Groups.Count,
		"optional_table_status":    result.Optional.Status,
		"history_response_decoded": result.HistoryRead, "history_records": len(result.History),
		"history_complete": result.HistoryComplete, "history_completed_windows": len(result.HistoryCovered),
		"history_order_kinds": tradeKinds(result.History), "history_time_checks": tradeTimeEvidence(result.History),
		"local_reference_matches":    map[string]bool{},
		"account_reference_verified": false,
	}
}

func compareAccount(actual mt4.Account, expected *expectedAccount) map[string]bool {
	result := make(map[string]bool)
	if expected == nil {
		return result
	}
	if expected.Balance != nil {
		result["balance_at_two_decimals"] = math.Round(actual.Balance*100) == math.Round(*expected.Balance*100)
	}
	if expected.Credit != nil {
		result["credit_at_two_decimals"] = math.Round(actual.Credit*100) == math.Round(*expected.Credit*100)
	}
	if expected.Leverage != nil {
		result["leverage"] = actual.Leverage == *expected.Leverage
	}
	if expected.Equity != nil {
		result["equity_at_two_decimals"] = math.Round(actual.Equity*100) == math.Round(*expected.Equity*100)
	}
	if expected.Currency != nil {
		result["currency"] = actual.Currency == *expected.Currency
	}
	if expected.Margin != nil {
		result["margin_at_two_decimals"] = actual.MarginAvailable && math.Round(actual.Margin*100) == math.Round(*expected.Margin*100)
	}
	return result
}

func tradeKinds(records []mt4.Trade) map[string]int {
	result := make(map[string]int)
	names := []string{"buy", "sell", "buy_limit", "sell_limit", "buy_stop", "sell_stop", "balance", "credit"}
	for _, record := range records {
		name := "unsupported"
		if record.Command >= 0 && int(record.Command) < len(names) {
			name = names[record.Command]
		}
		result[name]++
	}
	return result
}

func tradeTimeEvidence(records []mt4.Trade) map[string]any {
	count, plausible, zero40, ordered := 0, true, true, true
	for _, record := range records {
		if record.Command != 0 && record.Command != 1 {
			continue
		}
		count++
		plausible = plausible && record.Time20 >= 946684800 && record.Time20 <= math.MaxInt32
		zero40 = zero40 && record.Time40 == 0
		ordered = ordered && record.Time20 <= record.Time40
	}
	return map[string]any{
		"buy_sell_records_checked": count, "time20_plausible_nonzero": count > 0 && plausible,
		"time40_all_zero": count > 0 && zero40, "time20_le_time40": count > 0 && ordered,
	}
}
