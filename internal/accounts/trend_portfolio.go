package accounts

import (
	"context"
	"sort"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
)

type portfolioSlot struct {
	point                   TrendPoint
	balance, equity, credit decimal.Decimal
	floating                decimal.Decimal
}

// Portfolio points are complete valuations of their historical membership,
// never the sum of whatever accounts happen to have responded. Financial
// returns and capital-flow reconciliation are deliberately separate reports.
func (s *Service) PortfolioTrend(ctx context.Context, input TrendRequest) (TrendSeries, error) {
	view, err := s.validateTrend(input)
	view.Scope, view.Unit = "portfolio", "USD"
	view.NativeCurrency, view.Currencies = "USD", []string{"USD"}
	view.CanConvertUSD = true
	view.AlignmentMS, view.MaxSampleAgeMS = trendAlignmentMS, trendMaxAgeMS
	if err != nil {
		return view, err
	}
	if input.Unit != "USD" {
		return view, &InputError{"组合走势仅支持USD"}
	}
	var accounts []storage.Account
	if err = s.Store.Read.WithContext(ctx).Select("id,server_id,currency,usd_scale,unit_revision,unit_verified_at_ms,included_at_ms,removed_at_ms,created_at_ms").
		Where("created_at_ms<?", input.ToMS).Order("id").Find(&accounts).Error; err != nil {
		return view, err
	}
	start := (input.FromMS + trendAlignmentMS - 1) / trendAlignmentMS * trendAlignmentMS
	slots := make([]portfolioSlot, max(0, int((input.ToMS-start+trendAlignmentMS-1)/trendAlignmentMS)))
	for i := range slots {
		slots[i].point = TrendPoint{TimeMS: start + int64(i)*trendAlignmentMS, Currency: "USD", CadenceKnown: true}
	}
	boundaries := []int64{}
	for _, account := range accounts {
		if account.IncludedAtMS == nil {
			if account.USDScale != nil {
				view.PendingAccounts++
			} else {
				view.ExcludedAccounts++
			}
			continue
		}
		boundaries = append(boundaries, *account.IncludedAtMS)
		if account.RemovedAtMS != nil {
			boundaries = append(boundaries, *account.RemovedAtMS)
		}
		var changes []storage.AccountUnitChange
		if err := s.Store.Read.WithContext(ctx).Where("account_id=? AND effective_at_ms<?", account.ID, input.ToMS).
			Order("effective_at_ms,unit_revision").Find(&changes).Error; err != nil {
			return view, err
		}
		for _, change := range changes {
			boundaries = append(boundaries, change.EffectiveAtMS)
		}
		if err := s.addPortfolioAccount(ctx, account, input, slots, changes); err != nil {
			return view, err
		}
		for _, event := range []struct {
			at   *int64
			kind string
		}{{account.IncludedAtMS, "account_included"}, {account.RemovedAtMS, "account_removed"}} {
			if event.at != nil && *event.at >= input.FromMS && *event.at < input.ToMS {
				view.Events = append(view.Events, TrendEvent{TimeMS: *event.at, Kind: event.kind, AccountID: account.ID, Currency: "USD"})
			}
		}
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })
	reducer := newTrendReducer(input.FromMS, input.ToMS)
	segment, boundaryIndex := int64(0), 0
	previousValid := false
	for i := range slots {
		slot := &slots[i]
		p := slot.point
		changed := false
		for boundaryIndex < len(boundaries) && boundaries[boundaryIndex] <= p.TimeMS {
			changed = true
			boundaryIndex++
		}
		valid := p.ExpectedAccounts > 0 && p.ExpectedAccounts == p.IncludedAccounts
		if valid && p.ObservedToMS-p.ObservedFromMS > trendMaxAgeMS {
			p.Reason, valid = "sampling_skew", false
		}
		if valid {
			p.Balance, p.Equity, p.Credit, p.FloatingNetProfit = &slot.balance, &slot.equity, &slot.credit, &slot.floating
			view.LastObservedToMS = max(view.LastObservedToMS, p.ObservedToMS)
		} else if p.ExpectedAccounts == 0 {
			p.Reason = "no_members"
		}
		if changed || !valid || !previousValid {
			segment++
		}
		p.Segment = segment
		reducer.add(p)
		previousValid = valid
	}
	reducer.finish(&view)
	view.Stale = view.LastObservedToMS == 0 || min(view.ToMS, view.AsOfMS)-view.LastObservedToMS > trendMaxAgeMS
	if err := s.trendFundEvents(ctx, accounts, input, &view); err != nil {
		return view, err
	}
	sort.SliceStable(view.Events, func(i, j int) bool { return view.Events[i].TimeMS < view.Events[j].TimeMS })
	return view, nil
}

func (s *Service) addPortfolioAccount(ctx context.Context, account storage.Account, input TrendRequest, slots []portfolioSlot, changes []storage.AccountUnitChange) error {
	index, changeIndex := 0, -1
	var last *storage.Snapshot
	fill := func(until int64) {
		for index < len(slots) && slots[index].point.TimeMS < until {
			slot := &slots[index]
			p := &slot.point
			index++
			for changeIndex+1 < len(changes) && changes[changeIndex+1].EffectiveAtMS <= p.TimeMS {
				changeIndex++
			}
			if p.TimeMS < *account.IncludedAtMS || account.RemovedAtMS != nil && p.TimeMS >= *account.RemovedAtMS {
				continue
			}
			p.ExpectedAccounts++
			if last == nil || p.TimeMS-last.ObservedToMS > trendMaxAgeMS || last.ObservedToMS-last.ObservedFromMS > trendMaxAgeMS {
				p.Reason = "missing_samples"
				continue
			}
			if last.USDScale == nil || last.NativeCurrency != "USD" && last.NativeCurrency != "USC" {
				p.Reason = "unit_unconfirmed"
				continue
			}
			if changeIndex >= 0 && changes[changeIndex].UnitRevision != last.UnitRevision {
				p.Reason = "unit_changed"
				continue
			}
			if p.IncludedAccounts == 0 || last.ObservedFromMS < p.ObservedFromMS {
				p.ObservedFromMS = last.ObservedFromMS
			}
			p.ObservedToMS = max(p.ObservedToMS, last.ObservedToMS)
			p.IncludedAccounts++
			scale := *last.USDScale
			slot.balance = slot.balance.Add(last.Balance.Mul(scale))
			slot.equity = slot.equity.Add(last.Equity.Mul(scale))
			slot.credit = slot.credit.Add(last.Credit.Mul(scale))
			slot.floating = slot.floating.Add(last.FloatingNetProfit.Mul(scale))
		}
	}
	err := s.visitPortfolioSnapshots(ctx, account.ID, max(0, input.FromMS-trendMaxAgeMS), input.ToMS, func(snapshot storage.Snapshot) error {
		fill(snapshot.ObservedToMS)
		last = &snapshot
		return nil
	})
	if err != nil {
		return err
	}
	fill(input.ToMS)
	return ctx.Err()
}
