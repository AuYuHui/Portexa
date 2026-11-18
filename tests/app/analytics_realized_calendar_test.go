package app_test

import (
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"portexa/internal/analytics"
)

func TestAnalyticsMonthlyRealizedIndependentOfMidnightValuation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, monthEnd := range []bool{false, true} {
			input := performanceInput("1000", "1111", "1122")
			date := 12
			if monthEnd {
				date = 30
			}
			from := time.Date(2026, 11, date, 15, 59, 0, 0, time.UTC).UnixMilli()
			shift := from - input.FromMS
			input.FromMS += shift
			input.ToMS += shift
			balances := []int64{1000, 1105, 1112}
			for i := range input.Samples {
				input.Samples[i].TimeMS += shift
				input.Samples[i].ObservedFromMS += shift
				input.Samples[i].ObservedToMS += shift
				input.Samples[i].Balance = decimal.NewFromInt(balances[i])
				input.Samples[i].Floating = input.Samples[i].Equity.Sub(input.Samples[i].Balance)
			}
			if missing {
				input.Samples[1].Reason = "missing_samples"
			}
			input.Events = []analytics.Event{
				{TimeMS: from + 10_000, Kind: "trade", Amount: decimal.NewFromInt(5)},
				{TimeMS: from + 20_000, Kind: "deposit", Amount: decimal.NewFromInt(100)},
				{TimeMS: from + analytics.Minute + 10_000, Kind: "trade", Amount: decimal.NewFromInt(7)},
			}
			result := calculatePerformance(t, input)
			if len(result.Days) != 2 {
				t.Fatal("expected two Beijing dates", result.Days)
			}
			assertMetric(t, result.Days[0].Realized, "5")
			assertMetric(t, result.Days[1].Realized, "7")
			for i, day := range result.Days {
				if day.Trades == nil || *day.Trades != 1 {
					t.Fatal("daily trade count must survive missing valuations", day)
				}
				if missing {
					if day.NetProfit.Value != nil || day.FloatingChange.Value != nil {
						t.Fatal("missing midnight valuation was invented", day)
					}
				} else {
					assertMetric(t, day.FloatingChange, []string{"6", "4"}[i])
					assertMetric(t, day.NetProfit, "11")
				}
			}
			if monthEnd {
				if len(result.Months) != 2 {
					t.Fatal("month boundary not respected", result.Months)
				}
				assertMetric(t, result.Months[0].Realized, "5")
				assertMetric(t, result.Months[1].Realized, "7")
			} else {
				assertMetric(t, result.Months[0].Realized, "12")
				if !missing {
					assertMetric(t, result.Months[0].FloatingChange, "10")
					assertMetric(t, result.Months[0].NetProfit, "22")
				}
			}
			for _, month := range result.Months {
				if !month.Partial {
					t.Fatal("selected partial month must be marked", month)
				}
				if missing && (month.NetProfit.Value != nil || month.FloatingChange.Value != nil) {
					t.Fatal("monthly valuation must remain unavailable", month)
				}
			}
			if !monthEnd && !missing {
				input.ScopedReasons = []analytics.ScopedReason{{FromMS: from + 1, ToMS: from + 2, Reason: "history_incomplete"}}
				incomplete := calculatePerformance(t, input)
				assertMetric(t, incomplete.Days[1].Realized, "7")
				if incomplete.Days[0].Realized.Value != nil || incomplete.Months[0].Realized.Value != nil {
					t.Fatal("known-day subtotal must not masquerade as a complete month", incomplete.Months)
				}
			}
		}
	}
}

func TestAnalyticsMonthlyRealizedRequiresCompleteTradeEvidence(t *testing.T) {
	input := performanceInput("1000", "1000", "1000")
	input.Samples[0].Reason = "missing_samples"
	result := calculatePerformance(t, input)
	assertMetric(t, result.Days[0].Realized, "0")
	assertMetric(t, result.Months[0].Realized, "0")
	if result.Months[0].FloatingChange.Value != nil {
		t.Fatal("missing floating change must not be zero")
	}
	input.LedgerReasons = []string{"history_incomplete"}
	result = calculatePerformance(t, input)
	if result.Days[0].Realized.Value != nil || result.Months[0].Realized.Value != nil ||
		!slices.Contains(result.Months[0].Realized.Reasons, "history_incomplete") {
		t.Fatal("unknown trades must not produce zero realized profit", result)
	}
}
