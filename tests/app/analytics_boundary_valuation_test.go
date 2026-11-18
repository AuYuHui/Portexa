package app_test

import (
	"slices"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

func TestAnalyticsBoundarySelectsReconciledLaterValuation(t *testing.T) {
	for _, scenario := range []string{"clean", "dense_before_boundary", "later_trade", "collection_trade", "no_later_sample", "incomplete_bridge", "old_later_sample", "balance_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			f, a, now, start := analyticsFixture(t)
			end := start.Add(3 * time.Minute)
			for _, row := range []struct {
				at                time.Time
				balance, floating float64
			}{{start.Add(-30 * time.Second), 1000, -3}, {end.Add(-30 * time.Second), 1000, -2}} {
				*now = row.at
				analyticsSnapshot(t, f, a, row.at, row.balance, row.floating)
			}
			trades := []mt4.Trade{{Ticket: 78101, Command: 0, OpenTime: uint32(start.Unix()), CloseTime: uint32(end.Add(-2 * time.Second).Unix()), Profit: 7}}
			laterBalance := float64(1007)
			if scenario == "balance_mismatch" {
				laterBalance++
			}
			if scenario == "dense_before_boundary" {
				*now = end.Add(-100 * time.Millisecond)
				analyticsSnapshot(t, f, a, *now, laterBalance, -1)
			}
			if scenario == "later_trade" || scenario == "collection_trade" || scenario == "dense_before_boundary" {
				at := end.Add(10 * time.Second)
				if scenario == "collection_trade" {
					at = end.Add(28 * time.Second)
				}
				trades = append(trades, mt4.Trade{Ticket: 78102, Command: 0, OpenTime: uint32(start.Unix()), CloseTime: uint32(at.Unix()), Profit: 3})
				laterBalance += 3
			}
			if scenario != "no_later_sample" {
				*now = end.Add(28 * time.Second)
				if scenario == "old_later_sample" {
					*now = end.Add(91 * time.Second)
				}
				analyticsSnapshot(t, f, a, *now, laterBalance, -1)
			}
			*now = end.Add(2 * time.Minute)
			if scenario == "incomplete_bridge" {
				*now = end.Add(5 * time.Second)
			}
			analyticsHistory(t, f, a, *now, trades)
			*now = end.Add(2 * time.Minute)
			for _, account := range []string{a.ID, ""} {
				view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: account, Unit: "USD", FromMS: start.UnixMilli(), ToMS: end.UnixMilli()})
				if err != nil {
					t.Fatal(err)
				}
				assertMetric(t, view.Result.Metrics.Realized, "7")
				if scenario == "clean" || scenario == "dense_before_boundary" {
					assertMetric(t, view.Result.Metrics.NetProfit, "9")
					assertMetric(t, view.Result.Metrics.FloatingChange, "2")
					assertMetric(t, view.Result.Days[0].NetProfit, "9")
					if view.Result.Quality.Reconciliation != "passed" {
						t.Fatal("unreconciled fallback", view.Result.Quality)
					}
				} else if view.Result.Metrics.NetProfit.Value != nil {
					t.Fatal("uncertain boundary accepted", view.Result.Metrics)
				} else {
					reason := "boundary_event"
					if scenario == "balance_mismatch" {
						reason = "reconciliation_mismatch"
					}
					if !slices.Contains(view.Result.Metrics.NetProfit.Reasons, reason) {
						t.Fatal("wrong refusal reason", view.Result.Metrics.NetProfit)
					}
				}
			}
		})
	}
}

func TestAnalyticsBoundaryKeepsAccountWindowsIndependent(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id=?", a.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	b := f.Account(t, server, endpoint, 9862, "USD")
	end := start.Add(3 * time.Minute)
	for _, row := range []struct {
		a                 storage.Account
		at                time.Time
		balance, floating float64
	}{
		{b, start.Add(-32 * time.Second), 1000, 0},
		{a, start.Add(-25 * time.Second), 1000, -3},
		{b, end.Add(-32 * time.Second), 1000, -2},
		{a, end.Add(-25 * time.Second), 1006, -1},
		{b, end.Add(28 * time.Second), 1007, -1},
	} {
		*now = row.at
		analyticsSnapshot(t, f, row.a, row.at, row.balance, row.floating)
	}
	*now = end.Add(2 * time.Minute)
	analyticsHistory(t, f, a, *now, []mt4.Trade{
		{Ticket: 78103, Command: 0, OpenTime: uint32(start.Add(-time.Minute).Unix()), CloseTime: uint32(start.Add(-33 * time.Second).Unix()), Profit: 5},
		{Ticket: 78104, Command: 0, OpenTime: uint32(start.Unix()), CloseTime: uint32(start.Add(time.Minute).Unix()), Profit: 6},
	})
	analyticsHistory(t, f, b, *now, []mt4.Trade{{Ticket: 78105, Command: 0, OpenTime: uint32(start.Unix()), CloseTime: uint32(end.Add(-2 * time.Second).Unix()), Profit: 7}})
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{Unit: "USD", FromMS: start.UnixMilli(), ToMS: end.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "14")
	assertMetric(t, view.Result.Days[0].NetProfit, "14")
	assertMetric(t, view.Result.Metrics.Realized, "13")
	assertMetric(t, view.Result.Metrics.FloatingChange, "1")
}
