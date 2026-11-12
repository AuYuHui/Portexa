package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

type totals struct {
	balance, credit, capital, realized, income, costs, deposits, withdrawals, floatingCapital decimal.Decimal
	unknown                                                                                   bool
	trades                                                                                    int
}

func eventsBetween(events []Event, from, to int64) []Event {
	a := sort.Search(len(events), func(i int) bool { return events[i].TimeMS >= from })
	b := sort.Search(len(events), func(i int) bool { return events[i].TimeMS >= to })
	return events[a:b]
}

func totalEvents(events []Event) totals {
	var sum totals
	for _, event := range events {
		if event.Kind == "credit" {
			sum.credit = sum.credit.Add(event.Amount)
			continue
		}
		balanceAmount := event.Amount
		if event.BalanceAmount != nil {
			balanceAmount = *event.BalanceAmount
		}
		sum.balance = sum.balance.Add(balanceAmount)
		if event.CreditAmount != nil {
			sum.credit = sum.credit.Add(*event.CreditAmount)
		}
		sum.floatingCapital = sum.floatingCapital.Add(event.FloatingCapital)
		if event.Capital() {
			sum.capital = sum.capital.Add(event.Amount)
		}
		switch event.Kind {
		case "trade":
			sum.realized = sum.realized.Add(event.Amount)
			sum.trades++
		case "rebate", "profit_share_income":
			sum.income = sum.income.Add(event.Amount)
		case "fee", "profit_share_expense":
			sum.costs = sum.costs.Add(event.Amount)
		case "deposit":
			sum.deposits = sum.deposits.Add(event.Amount)
		case "withdrawal":
			sum.withdrawals = sum.withdrawals.Add(event.Amount.Abs())
		case "unclassified", "adjustment":
			sum.unknown = true
		}
	}
	return sum
}

func equity(sample Sample) decimal.Decimal  { return sample.Equity.Sub(sample.Credit) }
func tolerance(a, b Sample) decimal.Decimal { return decimal.Max(a.Tolerance, b.Tolerance) }

func reconcile(a, b Sample, sum totals) (decimal.Decimal, decimal.Decimal, bool) {
	balance := b.Balance.Sub(a.Balance).Sub(sum.balance)
	credit := b.Credit.Sub(a.Credit).Sub(sum.credit)
	epsilon := tolerance(a, b)
	return balance, credit, balance.Abs().LessThanOrEqual(epsilon) && credit.Abs().LessThanOrEqual(epsilon)
}

// Sample validity includes the complete uncertainty window, up to its minute
// label. A second-resolution event at a boundary cannot be assigned by guess.
func sampleReason(sample Sample, events []Event) string {
	if sample.Reason != "" {
		return sample.Reason
	}
	if sample.ObservedToMS == 0 {
		return "missing_samples"
	}
	if sample.Equity.Sub(sample.Credit).Sub(sample.Balance).Sub(sample.Floating).Abs().GreaterThan(sample.Tolerance) {
		return "structural_mismatch"
	}
	if len(eventsBetween(events, sample.ObservedFromMS-999, sample.TimeMS+1)) > 0 {
		return "boundary_event"
	}
	return ""
}

// Calculate uses all aligned source samples. Downsampling is a final display
// operation; financial metrics and daily values never use a reduced curve.
func Calculate(ctx context.Context, input Input, details bool) (Result, error) {
	result := Result{AccountID: input.AccountID, Label: input.Label, Currency: input.Currency,
		FromMS: input.FromMS, ToMS: input.ToMS, Points: []Point{}, Days: []Day{}, Months: []Month{}}
	q := Quality{Reasons: []string{}, ExpectedPoints: len(input.Samples), RawSamples: input.RawSamples,
		Unclassified: input.Unclassified, UnresolvedTimes: input.UnresolvedTimes, Reconciliation: "unavailable"}
	ledgerReasons := append([]string{}, input.LedgerReasons...)
	historyComplete := len(input.Coverage) == len(input.Samples)-1 && len(input.Samples) >= 2
	for _, covered := range input.Coverage {
		if !covered {
			historyComplete = false
		}
	}
	q.HistoryComplete = historyComplete
	if !historyComplete {
		ledgerReasons = append(ledgerReasons, "history_incomplete")
	}
	if input.UnresolvedTimes > 0 {
		ledgerReasons = append(ledgerReasons, "time_unresolved")
	}
	sum := totalEvents(eventsBetween(input.Events, input.FromMS, input.ToMS))
	tradeReasons := unique(ledgerReasons)
	profitReasons := append([]string{}, tradeReasons...)
	profitReasons = append(profitReasons, input.CapitalReasons...)
	if sum.unknown || input.Unclassified > 0 {
		profitReasons = append(profitReasons, "unclassified_funds")
	}
	m := Metrics{Realized: known(sum.realized), OtherIncome: known(sum.income), Costs: known(sum.costs),
		CapitalNet: known(sum.capital), Deposits: known(sum.deposits), Withdrawals: known(sum.withdrawals)}
	if len(tradeReasons) > 0 {
		m.Realized, m.OtherIncome, m.Costs = unavailable(tradeReasons...), unavailable(tradeReasons...), unavailable(tradeReasons...)
	}
	if sum.unknown || input.Unclassified > 0 {
		m.OtherIncome, m.Costs = unavailable("unclassified_funds"), unavailable("unclassified_funds")
	}
	if len(profitReasons) > 0 {
		m.CapitalNet, m.Deposits, m.Withdrawals = unavailable(profitReasons...), unavailable(profitReasons...), unavailable(profitReasons...)
	}
	reasons := make([]string, len(input.Samples))
	for i, sample := range input.Samples {
		reasons[i] = sampleReason(sample, input.Events)
		if reasons[i] == "" {
			q.ValidPoints++
			if sample.UnknownCadence {
				q.UnknownCadence++
			}
			if q.ObservedFromMS == 0 {
				q.ObservedFromMS = sample.ObservedFromMS
			}
			q.ObservedToMS = sample.ObservedToMS
		}
	}
	if len(input.Samples) < 2 {
		profitReasons = append(profitReasons, "insufficient_samples")
	} else {
		a, b := input.Samples[0], input.Samples[len(input.Samples)-1]
		q.FirstSampleID, q.LastSampleID = a.ID, b.ID
		if reasons[0] != "" {
			profitReasons = append(profitReasons, "missing_start", reasons[0])
		}
		if reasons[len(reasons)-1] != "" {
			profitReasons = append(profitReasons, "missing_end", reasons[len(reasons)-1])
		}
		if reasons[0] == "" && reasons[len(reasons)-1] == "" {
			m.FloatingChange = known(b.Floating.Sub(a.Floating).Sub(sum.floatingCapital))
			balance, credit, ok := reconcile(a, b, sum)
			q.BalanceResidual, q.CreditResidual, q.ReconciliationTolerance = ptr(balance), ptr(credit), ptr(tolerance(a, b))
			if len(tradeReasons) == 0 {
				q.Reconciliation = "passed"
				if !ok {
					q.Reconciliation = "failed"
					profitReasons = append(profitReasons, "reconciliation_mismatch")
				}
			}
			if len(profitReasons) == 0 {
				m.NetProfit = known(equity(b).Sub(equity(a)).Sub(sum.capital))
			}
		} else {
			m.FloatingChange = unavailable("missing_endpoints")
		}
	}
	if m.FloatingChange.Value == nil && len(m.FloatingChange.Reasons) == 0 {
		m.FloatingChange = unavailable("insufficient_samples")
	}
	if m.NetProfit.Value == nil {
		m.NetProfit = unavailable(profitReasons...)
	}
	pathReasons := append([]string{}, profitReasons...)
	nav, peak, cumulative, maximum := decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.Zero, decimal.Zero
	segment, peakMS := 0, input.FromMS
	var drawdown decimal.Decimal
	for i, sample := range input.Samples {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return result, err
			}
		}
		p := Point{TimeMS: sample.TimeMS, Reason: reasons[i]}
		if p.Reason == "" {
			p.StatisticalEquity = ptr(equity(sample))
		}
		valid := i > 0 && reasons[i] == "" && reasons[i-1] == "" && i-1 < len(input.Coverage) && input.Coverage[i-1]
		if valid && len(tradeReasons) == 0 && len(input.CapitalReasons) == 0 {
			a := input.Samples[i-1]
			events := eventsBetween(input.Events, a.TimeMS, sample.TimeMS)
			period := totalEvents(events)
			_, _, ok := reconcile(a, sample, period)
			if period.unknown {
				valid = false
				p.Reason = "unclassified_funds"
			} else if !ok {
				valid = false
				p.Reason = "reconciliation_mismatch"
			}
			if valid {
				gain := equity(sample).Sub(equity(a)).Sub(period.capital)
				denominator := equity(a)
				for _, event := range events {
					if event.Capital() {
						weight := decimal.NewFromInt(sample.TimeMS-event.TimeMS).DivRound(decimal.NewFromInt(sample.TimeMS-a.TimeMS), 24)
						denominator = denominator.Add(event.Amount.Mul(weight))
					}
				}
				if denominator.Sign() <= 0 || equity(a).Sign() <= 0 {
					valid = false
					p.Reason = "nonpositive_capital"
				} else {
					r := gain.DivRound(denominator, 24)
					if r.LessThanOrEqual(decimal.NewFromInt(-1)) {
						valid = false
						p.Reason = "return_chain_broken"
					} else {
						nav = nav.Mul(decimal.NewFromInt(1).Add(r)).Round(24)
						cumulative = cumulative.Add(gain)
						if nav.GreaterThanOrEqual(peak) {
							if result.Drawdown.TroughMS != 0 && result.Drawdown.RecoveredMS == nil && nav.GreaterThanOrEqual(peak) {
								stamp := sample.TimeMS
								result.Drawdown.RecoveredMS = &stamp
							}
							peak, peakMS = nav, sample.TimeMS
						}
						drawdown = decimal.NewFromInt(1).Sub(nav.DivRound(peak, 24)).Mul(decimal.NewFromInt(100))
						if drawdown.GreaterThan(maximum) {
							maximum = drawdown
							result.Drawdown = Drawdown{PeakMS: peakMS, TroughMS: sample.TimeMS}
						}
						p.NetProfit, p.ReturnPct, p.DrawdownPct = ptr(cumulative), ptr(nav.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100))), ptr(drawdown)
						q.ValidIntervals++
					}
				}
			}
		} else if i > 0 {
			valid = false
		}
		if i > 0 && !valid {
			pathReasons = append(pathReasons, "sampling_gap", p.Reason)
			if p.Reason == "" {
				p.Reason = "sampling_gap"
			}
		}
		if !valid {
			segment++
			nav, peak, cumulative = decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.Zero
			peakMS = sample.TimeMS
			if reasons[i] == "" && len(tradeReasons) == 0 && len(profitReasons) == 0 {
				p.NetProfit, p.ReturnPct, p.DrawdownPct = ptr(decimal.Zero), ptr(decimal.Zero), ptr(decimal.Zero)
			}
		}
		p.Segment = segment
		if details {
			result.Points = append(result.Points, p)
		}
	}
	pathReasons = unique(pathReasons)
	if len(pathReasons) == 0 && len(input.Samples) >= 2 && q.ValidIntervals == len(input.Samples)-1 {
		m.ReturnPct, m.MaxDrawdownPct, m.CurrentDrawdownPct = known(nav.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100))), known(maximum), known(drawdown)
	} else {
		m.ReturnPct, m.MaxDrawdownPct, m.CurrentDrawdownPct = unavailable(pathReasons...), unavailable(pathReasons...), unavailable(pathReasons...)
		result.Drawdown = Drawdown{}
	}
	q.Reasons = unique(append(profitReasons, pathReasons...))
	result.Metrics, result.Quality = m, q
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d", Version, input.Fingerprint, input.FromMS, input.ToMS)))
	result.EvidenceID = hex.EncodeToString(fingerprint[:])
	if details {
		result.Points, result.Downsampled = reducePoints(result.Points)
		var err error
		result.Days, err = calendar(ctx, input)
		if err != nil {
			return result, err
		}
		result.Months = monthly(result.Days)
	}
	return result, nil
}
