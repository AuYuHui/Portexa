package accounts

import (
	"github.com/shopspring/decimal"
	"portexa/internal/analytics"
	"portexa/internal/storage"
)

func (source *analyticsSource) valuation(row *storage.Snapshot, at int64, unit string) analytics.Sample {
	p := analytics.Sample{TimeMS: at, Reason: "missing_samples", Expected: 1, Tolerance: analytics.Tolerance}
	if row == nil {
		return p
	}
	p.ID, p.ObservedFromMS, p.ObservedToMS = row.ID, row.ObservedFromMS, row.ObservedToMS
	if at-row.ObservedToMS > analytics.MaxAge || row.ObservedToMS-at > analytics.MaxAge || row.ObservedToMS-row.ObservedFromMS > analytics.MaxAge {
		return p
	}
	p.Reason = ""
	p.UnknownCadence = row.CollectionIntervalSeconds == nil
	if row.UnitRevision != source.account.UnitRevision || source.account.Currency == nil || row.NativeCurrency != *source.account.Currency {
		p.Reason = "unit_changed"
	}
	scale := decimal.NewFromInt(1)
	if unit == "USD" {
		if row.USDScale == nil {
			p.Reason = "unit_unconfirmed"
		} else {
			scale = *row.USDScale
		}
	}
	p.Balance, p.Credit, p.Equity, p.Floating = row.Balance.Mul(scale), row.Credit.Mul(scale), row.Equity.Mul(scale), row.FloatingNetProfit.Mul(scale)
	p.Tolerance = analytics.Tolerance.Mul(scale)
	p.Windows = []analytics.ValuationWindow{{AccountID: source.account.ID, FromMS: row.ObservedFromMS, ToMS: row.ObservedToMS}}
	if p.Reason == "" {
		p.Included = 1
	}
	return p
}

func (source *analyticsSource) laterValuationUsable(p analytics.Sample) bool {
	if analytics.SampleReason(p, source.input.Events) != "" {
		return false
	}
	from, to := min(p.TimeMS, p.ObservedFromMS-999), p.ObservedToMS+1
	if !source.covered(from, to) || analyticsMember(source.account, p.TimeMS) != analyticsMember(source.account, p.ObservedToMS) {
		return false
	}
	for _, issue := range source.input.ScopedReasons {
		if issue.FromMS < to && issue.ToMS > from {
			return false
		}
	}
	ranges := eventWallRanges(source.entries, from, to)
	for _, wall := range source.unresolvedWalls {
		if wallCouldAffect(wall, ranges) {
			return false
		}
	}
	return true
}
