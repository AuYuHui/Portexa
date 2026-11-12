// Package analytics calculates reproducible, cash-flow-adjusted performance.
// It consumes evidence, never contacts a broker or reads a database.
package analytics

import "github.com/shopspring/decimal"

const Version = "analytics-v1-md24-minute"
const Minute int64 = 60_000
const MaxAge int64 = 90_000
const MaxRange int64 = 90 * 24 * 60 * Minute

var Tolerance = decimal.RequireFromString("0.00000001")

type Sample struct {
	TimeMS         int64
	ObservedFromMS int64
	ObservedToMS   int64
	ID             string
	Balance        decimal.Decimal
	Credit         decimal.Decimal
	Equity         decimal.Decimal
	Floating       decimal.Decimal
	Tolerance      decimal.Decimal
	Reason         string
	Expected       int
	Included       int
}

type Event struct {
	TimeMS          int64            `json:"time_ms"`
	AccountID       string           `json:"account_id"`
	Ticket          int32            `json:"ticket"`
	Revision        int64            `json:"revision"`
	Kind            string           `json:"kind"`
	Amount          decimal.Decimal  `json:"amount"`
	Symbol          string           `json:"symbol,omitempty"`
	Classification  string           `json:"classification"`
	SourceRunID     string           `json:"source_run_id"`
	BalanceAmount   *decimal.Decimal `json:"-"`
	CreditAmount    *decimal.Decimal `json:"-"`
	FloatingCapital decimal.Decimal  `json:"-"`
}

func (e Event) Capital() bool {
	return e.Kind == "deposit" || e.Kind == "withdrawal" || e.Kind == "capital_adjustment" || e.Kind == "transfer_in" ||
		e.Kind == "transfer_out" || e.Kind == "membership_in" || e.Kind == "membership_out"
}

type Input struct {
	AccountID       string
	Label           string
	Currency        string
	FromMS          int64
	ToMS            int64
	Samples         []Sample
	Events          []Event // Sorted by UTC, account and ticket; current revisions only.
	Coverage        []bool  // One entry per interval between minute samples.
	LedgerReasons   []string
	CapitalReasons  []string
	Fingerprint     string
	RawSamples      int
	Unclassified    int
	UnresolvedTimes int
}

type Metric struct {
	Value   *decimal.Decimal `json:"value"`
	Reasons []string         `json:"reasons"`
}

type Metrics struct {
	NetProfit          Metric `json:"net_profit"`
	ReturnPct          Metric `json:"return_pct"`
	Realized           Metric `json:"realized"`
	MaxDrawdownPct     Metric `json:"max_drawdown_pct"`
	CurrentDrawdownPct Metric `json:"current_drawdown_pct"`
	FloatingChange     Metric `json:"floating_change"`
	OtherIncome        Metric `json:"other_income"`
	Costs              Metric `json:"costs"`
	CapitalNet         Metric `json:"capital_net"`
	Deposits           Metric `json:"deposits"`
	Withdrawals        Metric `json:"withdrawals"`
}

type Quality struct {
	Reasons                 []string         `json:"reasons"`
	HistoryComplete         bool             `json:"history_complete"`
	ExpectedPoints          int              `json:"expected_points"`
	ValidPoints             int              `json:"valid_points"`
	ValidIntervals          int              `json:"valid_intervals"`
	RawSamples              int              `json:"raw_samples"`
	Unclassified            int              `json:"unclassified"`
	UnresolvedTimes         int              `json:"unresolved_times"`
	BalanceResidual         *decimal.Decimal `json:"balance_residual"`
	CreditResidual          *decimal.Decimal `json:"credit_residual"`
	ReconciliationTolerance *decimal.Decimal `json:"reconciliation_tolerance"`
	Reconciliation          string           `json:"reconciliation"`
	ObservedFromMS          int64            `json:"observed_from_ms"`
	ObservedToMS            int64            `json:"observed_to_ms"`
	FirstSampleID           string           `json:"first_sample_id"`
	LastSampleID            string           `json:"last_sample_id"`
}

type Point struct {
	TimeMS            int64            `json:"time_ms"`
	Segment           int              `json:"segment"`
	NetProfit         *decimal.Decimal `json:"net_profit"`
	ReturnPct         *decimal.Decimal `json:"return_pct"`
	DrawdownPct       *decimal.Decimal `json:"drawdown_pct"`
	StatisticalEquity *decimal.Decimal `json:"statistical_equity"`
	Reason            string           `json:"reason"`
}

type Day struct {
	Date           string `json:"date"`
	FromMS         int64  `json:"from_ms"`
	ToMS           int64  `json:"to_ms"`
	Partial        bool   `json:"partial"`
	NetProfit      Metric `json:"net_profit"`
	Realized       Metric `json:"realized"`
	FloatingChange Metric `json:"floating_change"`
	OtherIncome    Metric `json:"other_income"`
	Costs          Metric `json:"costs"`
	Trades         *int   `json:"trades"`
}

type Month struct {
	Month     string `json:"month"`
	NetProfit Metric `json:"net_profit"`
	Partial   bool   `json:"partial"`
}

type Drawdown struct {
	PeakMS      int64  `json:"peak_ms"`
	TroughMS    int64  `json:"trough_ms"`
	RecoveredMS *int64 `json:"recovered_ms"`
}

type Result struct {
	AccountID   string   `json:"account_id"`
	Label       string   `json:"label"`
	Currency    string   `json:"currency"`
	FromMS      int64    `json:"from_ms"`
	ToMS        int64    `json:"to_ms"`
	Metrics     Metrics  `json:"metrics"`
	Quality     Quality  `json:"quality"`
	Drawdown    Drawdown `json:"drawdown"`
	Points      []Point  `json:"points"`
	Days        []Day    `json:"days"`
	Months      []Month  `json:"months"`
	EvidenceID  string   `json:"evidence_id"`
	Downsampled bool     `json:"downsampled"`
}

func ptr(v decimal.Decimal) *decimal.Decimal { return &v }
func known(v decimal.Decimal) Metric         { return Metric{Value: ptr(v), Reasons: []string{}} }
func unavailable(reasons ...string) Metric   { return Metric{Reasons: unique(reasons)} }
func unique(values []string) []string {
	out := []string{}
	for _, value := range values {
		if value == "" {
			continue
		}
		found := false
		for _, previous := range out {
			if previous == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}
