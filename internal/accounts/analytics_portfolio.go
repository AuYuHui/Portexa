package accounts

import (
	"context"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
	"portexa/internal/analytics"
	"portexa/internal/storage"
)

type analyticsPortfolio struct{ input analytics.Input }

func newAnalyticsPortfolio(from, to int64) *analyticsPortfolio {
	count := int((to-from)/analytics.Minute) + 1
	p := &analyticsPortfolio{input: analytics.Input{FromMS: from, ToMS: to, Currency: "USD", Label: "portfolio",
		Samples: make([]analytics.Sample, count), Coverage: make([]bool, count-1), Events: []analytics.Event{}, LedgerReasons: []string{}}}
	for i := range p.input.Samples {
		p.input.Samples[i].TimeMS = from + int64(i)*analytics.Minute
	}
	for i := range p.input.Coverage {
		p.input.Coverage[i] = true
	}
	return p
}

func analyticsMember(a storage.Account, at int64) bool {
	return a.IncludedAtMS != nil && at >= *a.IncludedAtMS && (a.RemovedAtMS == nil || at < *a.RemovedAtMS)
}

func (s *Service) addAnalyticsPortfolio(ctx context.Context, p *analyticsPortfolio, source *analyticsSource) error {
	a, input := source.account, &p.input
	input.Fingerprint += source.account.ID + ":" + source.input.Fingerprint + "|"
	input.RawSamples += source.input.RawSamples
	active := false
	for i, sample := range source.input.Samples {
		if !analyticsMember(a, sample.TimeMS) {
			continue
		}
		active = true
		slot := &input.Samples[i]
		slot.Expected++
		if sample.Reason != "" {
			slot.Reason = sample.Reason
			continue
		}
		slot.Included++
		if slot.ObservedFromMS == 0 || sample.ObservedFromMS < slot.ObservedFromMS {
			slot.ObservedFromMS = sample.ObservedFromMS
		}
		slot.ObservedToMS = max(slot.ObservedToMS, sample.ObservedToMS)
		slot.Balance = slot.Balance.Add(sample.Balance)
		slot.Credit = slot.Credit.Add(sample.Credit)
		slot.Equity = slot.Equity.Add(sample.Equity)
		slot.Floating = slot.Floating.Add(sample.Floating)
		slot.Tolerance = slot.Tolerance.Add(sample.Tolerance)
	}
	for i := range input.Coverage {
		from, to := input.FromMS+int64(i)*analytics.Minute, input.FromMS+int64(i+1)*analytics.Minute
		if a.IncludedAtMS != nil && *a.IncludedAtMS < to && (a.RemovedAtMS == nil || *a.RemovedAtMS > from) {
			input.Coverage[i] = input.Coverage[i] && source.input.Coverage[i]
		}
	}
	if !active {
		return nil
	}
	input.LedgerReasons = append(input.LedgerReasons, source.input.LedgerReasons...)
	input.UnresolvedTimes += source.input.UnresolvedTimes
	for _, event := range source.input.Events {
		if !analyticsMember(a, event.TimeMS) {
			continue
		}
		input.Events = append(input.Events, event)
		if event.TimeMS >= input.FromMS && event.TimeMS < input.ToMS && (event.Kind == "unclassified" || event.Kind == "adjustment") {
			input.Unclassified++
		}
	}
	if a.IncludedAtMS != nil && *a.IncludedAtMS > input.FromMS && *a.IncludedAtMS < input.ToMS {
		var first storage.Snapshot
		found := s.Store.Read.WithContext(ctx).Where("account_id=? AND observed_to_ms=?", a.ID, *a.IncludedAtMS).Order("id").Limit(1).Find(&first)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 || first.USDScale == nil || first.UnitRevision != a.UnitRevision {
			input.CapitalReasons = append(input.CapitalReasons, "membership_unresolved")
		} else {
			balance, credit := first.Balance.Mul(*first.USDScale), first.Credit.Mul(*first.USDScale)
			input.Events = append(input.Events, analytics.Event{TimeMS: *a.IncludedAtMS, AccountID: a.ID, Kind: "membership_in",
				Amount: first.Equity.Sub(first.Credit).Mul(*first.USDScale), SourceRunID: first.SyncRunID,
				BalanceAmount: &balance, CreditAmount: &credit, FloatingCapital: first.FloatingNetProfit.Mul(*first.USDScale)})
		}
	}
	if a.RemovedAtMS != nil && *a.RemovedAtMS >= input.FromMS && *a.RemovedAtMS < input.ToMS {
		input.CapitalReasons = append(input.CapitalReasons, "membership_unresolved")
	}
	return nil
}

func (s *Service) finishAnalyticsPortfolio(ctx context.Context, p *analyticsPortfolio, sources []*analyticsSource, view *AnalyticsReport) error {
	input := &p.input
	byID := map[string]storage.Account{}
	for _, source := range sources {
		byID[source.account.ID] = source.account
	}
	// A transfer initiated before the selected range may still be in transit.
	// Do not erase it merely because the outgoing event predates the chart.
	seen := map[string]bool{}
	for _, event := range input.Events {
		seen[transferKey(event)] = true
	}
	for _, source := range sources {
		if source.account.USDScale == nil || source.account.IncludedAtMS == nil {
			continue
		}
		var prior []analyticsOrder
		query := withClassification(currentHistory(s.Store.Read.WithContext(ctx), source.account.ID)).
			Where("o.command=6 AND o.event_time_wall<?", input.FromMS/1000+24*3600).
			Where(`(CASE WHEN c.based_trade_revision=o.revision THEN c.category ELSE o.automatic_category END)='transfer_out'
				OR (c.based_trade_revision IS NOT o.revision AND o.automatic_rule_version<>?)`, FundingRuleVersion).
			Select("o.*,c.category AS manual_category,c.based_trade_revision AS manual_based_revision,c.revision AS manual_revision").
			Order("o.event_time_wall,o.ticket").Limit(10_001)
		if err := query.Scan(&prior).Error; err != nil {
			return err
		}
		if len(prior) > 10_000 {
			return &InputError{"待核对历史转账过多，请先查看单账户分析"}
		}
		for _, order := range prior {
			kind, key := classificationKey(order)
			if kind != "transfer_out" {
				continue
			}
			stamp, ok := trendEventUTC(order.EventTimeWall, source.entries)
			if !ok {
				input.CapitalReasons = append(input.CapitalReasons, "transfer_unresolved")
				continue
			}
			if stamp >= input.FromMS || !analyticsMember(source.account, stamp) {
				continue
			}
			event := analytics.Event{TimeMS: stamp, AccountID: source.account.ID, Ticket: order.Ticket, Revision: order.Revision, Kind: kind,
				Amount: order.NetProfit.Mul(*source.account.USDScale), Classification: key, SourceRunID: order.SourceRunID}
			if !seen[transferKey(event)] {
				input.Events = append(input.Events, event)
				seen[transferKey(event)] = true
			}
			input.Fingerprint += fmt.Sprintf("prior-transfer:%s:%s|", transferKey(event), key)
		}
	}
	var matches []storage.TransferMatch
	if err := s.Store.Read.WithContext(ctx).Where("out_ms<?", input.ToMS).Order("id").Limit(10_001).Find(&matches).Error; err != nil {
		return err
	}
	if len(matches) > 10_000 {
		return &InputError{"历史转账配对过多，请先查看单账户分析"}
	}
	matched := map[string]bool{}
	transit := make([]decimal.Decimal, len(input.Samples)+1)
	for _, match := range matches {
		out, err := s.transferEndpoint(ctx, s.Store.Read, match.OutAccountID, match.OutTicket, match.OutRevision)
		if err != nil {
			return err
		}
		in, err := s.transferEndpoint(ctx, s.Store.Read, match.InAccountID, match.InTicket, match.InRevision)
		if err != nil {
			return err
		}
		if out == nil || in == nil || out.key != match.OutClassification || in.key != match.InClassification ||
			out.ruleset != match.OutRuleset || in.ruleset != match.InRuleset || out.event.TimeMS != match.OutMS || in.event.TimeMS != match.InMS {
			continue
		}
		outAccount, outOK := byID[match.OutAccountID]
		inAccount, inOK := byID[match.InAccountID]
		if !outOK || !inOK || !analyticsMember(outAccount, match.OutMS) || !analyticsMember(inAccount, match.InMS) {
			continue
		}
		matched[transferKey(out.event)], matched[transferKey(in.event)] = true, true
		input.Fingerprint += "match:" + match.ID + "|"
		start := max(0, int((match.OutMS-input.FromMS+analytics.Minute-1)/analytics.Minute))
		end := max(0, int((match.InMS-input.FromMS+analytics.Minute-1)/analytics.Minute))
		start, end = min(start, len(input.Samples)), min(end, len(input.Samples))
		transit[start] = transit[start].Add(match.AmountUSD)
		transit[end] = transit[end].Sub(match.AmountUSD)
	}
	events := []analytics.Event{}
	for _, event := range input.Events {
		if event.Kind == "transfer_in" || event.Kind == "transfer_out" {
			if matched[transferKey(event)] {
				continue
			}
			input.CapitalReasons = append(input.CapitalReasons, "transfer_unresolved")
			if len(view.Transfers) < 100 {
				view.Transfers = append(view.Transfers, event)
			}
		}
		events = append(events, event)
	}
	input.Events = events
	amount := decimal.Zero
	hasMembers := false
	for i := range input.Samples {
		point := &input.Samples[i]
		amount = amount.Add(transit[i])
		point.Balance = point.Balance.Add(amount)
		point.Equity = point.Equity.Add(amount)
		if point.Expected == 0 {
			point.Reason = "no_members"
		} else if point.Expected != point.Included {
			point.Reason = "missing_samples"
		} else if point.ObservedToMS-point.ObservedFromMS > analytics.MaxAge {
			point.Reason = "sampling_skew"
		}
		if point.Expected > 0 {
			hasMembers = true
		}
	}
	if !hasMembers {
		input.LedgerReasons = append(input.LedgerReasons, "no_members")
		for i := range input.Coverage {
			input.Coverage[i] = false
		}
	}
	sort.Slice(input.Events, func(i, j int) bool {
		a, b := input.Events[i], input.Events[j]
		if a.TimeMS != b.TimeMS {
			return a.TimeMS < b.TimeMS
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		return a.Ticket < b.Ticket
	})
	return nil
}

func transferKey(event analytics.Event) string {
	return fmt.Sprintf("%s:%d:%d", event.AccountID, event.Ticket, event.Revision)
}
