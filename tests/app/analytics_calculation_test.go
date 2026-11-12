package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"portexa/internal/analytics"
)

func performanceInput(values ...string) analytics.Input {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	input := analytics.Input{FromMS: from, ToMS: from + int64(len(values)-1)*analytics.Minute, Currency: "USD", Fingerprint: "synthetic-v1", Events: []analytics.Event{}}
	for i, value := range values {
		at := from + int64(i)*analytics.Minute
		equity := decimal.RequireFromString(value)
		input.Samples = append(input.Samples, analytics.Sample{TimeMS: at, ObservedFromMS: at - 500, ObservedToMS: at, ID: value,
			Balance: decimal.NewFromInt(1000), Equity: equity, Floating: equity.Sub(decimal.NewFromInt(1000)), Tolerance: analytics.Tolerance})
		if i > 0 {
			input.Coverage = append(input.Coverage, true)
		}
	}
	return input
}

func assertMetric(t *testing.T, m analytics.Metric, want string) {
	t.Helper()
	if m.Value == nil || !m.Value.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("metric = %v (%v), want %s", m.Value, m.Reasons, want)
	}
}

func calculatePerformance(t *testing.T, input analytics.Input) analytics.Result {
	t.Helper()
	result, err := analytics.Calculate(t.Context(), input, true)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAnalyticsCashFlowWeightedReturnAndCredit(t *testing.T) {
	input := performanceInput("1000", "1530")
	input.Samples[1].Balance = decimal.NewFromInt(1500)
	input.Samples[1].Floating = decimal.NewFromInt(30)
	input.Events = []analytics.Event{{TimeMS: input.FromMS + 30_000, Kind: "deposit", Amount: decimal.NewFromInt(500)}}
	result := calculatePerformance(t, input)
	assertMetric(t, result.Metrics.NetProfit, "30")
	assertMetric(t, result.Metrics.ReturnPct, "2.4")
	assertMetric(t, result.Metrics.Realized, "0")
	assertMetric(t, result.Metrics.FloatingChange, "30")
	if result.Quality.Reconciliation != "passed" {
		t.Fatal(result.Quality)
	}
	input.Samples[1].Credit = decimal.NewFromInt(200)
	input.Samples[1].Equity = input.Samples[1].Equity.Add(decimal.NewFromInt(200))
	input.Events = append(input.Events, analytics.Event{TimeMS: input.FromMS + 40_000, Kind: "credit", Amount: decimal.NewFromInt(200)})
	result = calculatePerformance(t, input)
	assertMetric(t, result.Metrics.NetProfit, "30")
	assertMetric(t, result.Metrics.ReturnPct, "2.4")
}

func TestAnalyticsWithdrawalDoesNotProduceLossOrDrawdown(t *testing.T) {
	input := performanceInput("1000", "500")
	input.Samples[1].Balance = decimal.NewFromInt(500)
	input.Samples[1].Floating = decimal.Zero
	input.Events = []analytics.Event{{TimeMS: input.FromMS + 30_000, Kind: "withdrawal", Amount: decimal.NewFromInt(-500)}}
	result := calculatePerformance(t, input)
	assertMetric(t, result.Metrics.NetProfit, "0")
	assertMetric(t, result.Metrics.ReturnPct, "0")
	assertMetric(t, result.Metrics.MaxDrawdownPct, "0")
}

func TestAnalyticsFloatingDrawdownAndRecovery(t *testing.T) {
	input := performanceInput("1000", "1200", "900", "1200")
	result := calculatePerformance(t, input)
	assertMetric(t, result.Metrics.NetProfit, "200")
	assertMetric(t, result.Metrics.ReturnPct, "20")
	assertMetric(t, result.Metrics.MaxDrawdownPct, "25")
	assertMetric(t, result.Metrics.CurrentDrawdownPct, "0")
	if result.Drawdown.PeakMS != input.FromMS+analytics.Minute || result.Drawdown.TroughMS != input.FromMS+2*analytics.Minute ||
		result.Drawdown.RecoveredMS == nil || *result.Drawdown.RecoveredMS != input.ToMS {
		t.Fatal(result.Drawdown)
	}
}

func TestAnalyticsNetProfitBridgeIncludesSignedFeesAndFloatingChange(t *testing.T) {
	input := performanceInput("1010", "1025")
	input.Samples[1].Balance = decimal.NewFromInt(1035)
	input.Samples[1].Floating = decimal.NewFromInt(-10)
	input.Events = []analytics.Event{
		{TimeMS: input.FromMS + 15_000, Kind: "trade", Amount: decimal.NewFromInt(40)},
		{TimeMS: input.FromMS + 30_000, Kind: "fee", Amount: decimal.NewFromInt(-5)},
	}
	result := calculatePerformance(t, input)
	assertMetric(t, result.Metrics.NetProfit, "15")
	assertMetric(t, result.Metrics.Realized, "40")
	assertMetric(t, result.Metrics.Costs, "-5")
	assertMetric(t, result.Metrics.FloatingChange, "-20")
	bridge := result.Metrics.Realized.Value.Add(*result.Metrics.Costs.Value).Add(*result.Metrics.FloatingChange.Value)
	if !bridge.Equal(*result.Metrics.NetProfit.Value) {
		t.Fatal("profit bridge does not reconcile")
	}
}

func TestAnalyticsMetricSpecificGates(t *testing.T) {
	t.Run("unknown fund keeps realized trades", func(t *testing.T) {
		input := performanceInput("1000", "1010")
		input.Samples[1].Balance = decimal.NewFromInt(1010)
		input.Samples[1].Floating = decimal.Zero
		input.Events = []analytics.Event{{TimeMS: input.FromMS + 30_000, Kind: "unclassified", Amount: decimal.NewFromInt(10)}}
		result := calculatePerformance(t, input)
		assertMetric(t, result.Metrics.Realized, "0")
		if result.Metrics.NetProfit.Value != nil || result.Metrics.ReturnPct.Value != nil {
			t.Fatal("unknown flow became performance")
		}
	})
	t.Run("interior gap keeps endpoint profit", func(t *testing.T) {
		input := performanceInput("1000", "1010", "1020")
		input.Samples[1].Reason = "missing_samples"
		result := calculatePerformance(t, input)
		assertMetric(t, result.Metrics.NetProfit, "20")
		if result.Metrics.ReturnPct.Value != nil || result.Metrics.MaxDrawdownPct.Value != nil {
			t.Fatal("gap became a complete path")
		}
	})
	t.Run("history missing is not zero", func(t *testing.T) {
		input := performanceInput("1000", "1000")
		input.Coverage[0] = false
		result := calculatePerformance(t, input)
		if result.Metrics.Realized.Value != nil || result.Metrics.NetProfit.Value != nil {
			t.Fatal("missing history became zero")
		}
	})
	t.Run("balance mismatch blocks percentage", func(t *testing.T) {
		input := performanceInput("1000", "1010")
		input.Samples[1].Balance = decimal.NewFromInt(1010)
		input.Samples[1].Floating = decimal.Zero
		result := calculatePerformance(t, input)
		if result.Metrics.NetProfit.Value != nil || result.Quality.Reconciliation != "failed" {
			t.Fatal("unexplained balance growth accepted", result)
		}
		assertMetric(t, result.Metrics.Realized, "0")
	})
	t.Run("event boundary remains uncertain", func(t *testing.T) {
		input := performanceInput("1000", "1500")
		input.Samples[1].Balance = decimal.NewFromInt(1500)
		input.Samples[1].Floating = decimal.Zero
		input.Events = []analytics.Event{{TimeMS: input.ToMS, Kind: "deposit", Amount: decimal.NewFromInt(500)}}
		result := calculatePerformance(t, input)
		if result.Metrics.NetProfit.Value != nil {
			t.Fatal("boundary event assigned without evidence")
		}
	})
	t.Run("zero and negative equity do not restart old chain", func(t *testing.T) {
		input := performanceInput("1000", "0", "1100")
		result := calculatePerformance(t, input)
		assertMetric(t, result.Metrics.NetProfit, "100")
		if result.Metrics.ReturnPct.Value != nil || result.Metrics.MaxDrawdownPct.Value != nil {
			t.Fatal("bankruptcy hidden by a restart")
		}
	})
}

func TestAnalyticsCalendarUsesBeijingAndActualProfitNotClosedTrades(t *testing.T) {
	input := performanceInput("1000", "1010", "1025")
	from := time.Date(2026, 11, 1, 15, 59, 0, 0, time.UTC).UnixMilli()
	shift := from - input.FromMS
	input.FromMS += shift
	input.ToMS += shift
	for i := range input.Samples {
		input.Samples[i].TimeMS += shift
		input.Samples[i].ObservedFromMS += shift
		input.Samples[i].ObservedToMS += shift
	}
	result := calculatePerformance(t, input)
	if len(result.Days) != 2 || result.Days[0].Date != "2026-11-01" || result.Days[1].Date != "2026-11-02" {
		t.Fatal(result.Days)
	}
	assertMetric(t, result.Days[0].NetProfit, "10")
	assertMetric(t, result.Days[1].NetProfit, "15")
	assertMetric(t, result.Days[0].Realized, "0")
}

func TestAnalyticsReductionPreservesMetricsAndEvidence(t *testing.T) {
	values := make([]string, 3001)
	for i := range values {
		values[i] = "1000"
	}
	values[1333] = "1200"
	values[1334] = "900"
	input := performanceInput(values...)
	a := calculatePerformance(t, input)
	b := calculatePerformance(t, input)
	assertMetric(t, a.Metrics.MaxDrawdownPct, "25")
	if !a.Downsampled || len(a.Points) > 2400 || a.EvidenceID != b.EvidenceID {
		t.Fatal("display sampling or evidence unstable", len(a.Points))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := analytics.Calculate(ctx, input, true); err == nil {
		t.Fatal("canceled analysis kept computing")
	}
}
