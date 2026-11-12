package accounts

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"portexa/internal/storage"
)

// Markers are annotations only. They never adjust an asset curve or imply that
// the absence of markers proves complete cash-flow coverage.
func (s *Service) trendFundEvents(ctx context.Context, accounts []storage.Account, input TrendRequest, view *TrendSeries) error {
	const eventLimit = 500
	remaining := eventLimit
	calendar := map[string][]timeRuleEntry{}
	for _, account := range accounts {
		if remaining <= 0 {
			view.EventsTruncated = true
			break
		}
		if view.Scope == "portfolio" && account.IncludedAtMS == nil {
			continue
		}
		entries, found := calendar[account.ServerID]
		if !found {
			var server storage.Server
			if err := s.Store.Read.WithContext(ctx).First(&server, "id=?", account.ServerID).Error; err != nil {
				return err
			}
			margin := int64(26 * time.Hour / time.Millisecond)
			var err error
			entries, _, err = historicalTimeEntries(ctx, s.Store.Read, server, max(0, input.FromMS-margin), input.ToMS+margin)
			if err != nil {
				return err
			}
			calendar[account.ServerID] = entries
		}
		var orders []storage.HistoryOrder
		if err := currentHistory(s.Store.Read.WithContext(ctx), account.ID).Where("o.command=6 AND o.event_time_wall>=? AND o.event_time_wall<?",
			max(0, input.FromMS/1000-12*3600), input.ToMS/1000+14*3600).Select("o.*").
			Order("o.event_time_wall,o.ticket").Limit(remaining + 1).Scan(&orders).Error; err != nil {
			return err
		}
		if len(orders) > remaining {
			view.EventsTruncated = true
			orders = orders[:remaining]
		}
		remaining -= len(orders)
		for _, order := range orders {
			entry, err := readFundEntry(s.Store.Read.WithContext(ctx), order)
			if err != nil {
				return err
			}
			if entry.Classification == nil || entry.NeedsReview ||
				entry.Classification.Category != "deposit" && entry.Classification.Category != "withdrawal" {
				continue
			}
			stamp, confirmed := trendEventUTC(order.EventTimeWall, entries)
			if !confirmed {
				if wallCouldAffect(order.EventTimeWall, eventWallRanges(entries, input.FromMS, input.ToMS)) {
					view.UnplacedEvents++
				}
				continue
			}
			if stamp < input.FromMS || stamp >= input.ToMS {
				continue
			}
			if view.Scope == "portfolio" && (stamp < *account.IncludedAtMS || account.RemovedAtMS != nil && stamp >= *account.RemovedAtMS) {
				continue
			}
			amount, currency := order.NetProfit, order.NativeCurrency
			if input.Unit == "USD" {
				var snapshot storage.Snapshot
				found := s.Store.Read.WithContext(ctx).Select("usd_scale").
					Where("account_id=? AND unit_revision=? AND native_currency=? AND usd_scale IS NOT NULL", account.ID, order.UnitRevision, currency).
					Limit(1).Find(&snapshot)
				if found.Error != nil {
					return found.Error
				}
				if snapshot.USDScale == nil {
					view.UnplacedEvents++
					continue
				}
				amount, currency = amount.Mul(*snapshot.USDScale), "USD"
			}
			if len(view.Events) >= eventLimit {
				view.EventsTruncated = true
				break
			}
			view.Events = append(view.Events, TrendEvent{TimeMS: stamp, Kind: entry.Classification.Category,
				AccountID: account.ID, Ticket: order.Ticket, Amount: &amount, Currency: currency})
		}
	}
	sort.SliceStable(view.Events, func(i, j int) bool { return view.Events[i].TimeMS < view.Events[j].TimeMS })
	return nil
}

func trendTimeEntries(rules []serverTimeRule, from, to int64) []timeRuleEntry {
	byStart := map[int64]timeRuleEntry{}
	for _, rule := range rules {
		var list []timeRuleEntry
		if json.Unmarshal([]byte(rule.RulesJSON), &list) == nil {
			for _, entry := range list {
				byStart[entry.FromUTCMS] = entry
			}
		}
	}
	entries := make([]timeRuleEntry, 0, len(byStart))
	for _, entry := range byStart {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FromUTCMS < entries[j].FromUTCMS })
	selected := []timeRuleEntry{}
	for i, entry := range entries {
		if i+1 < len(entries) && (entry.UntilUTCMS == nil || *entry.UntilUTCMS > entries[i+1].FromUTCMS) {
			until := entries[i+1].FromUTCMS
			entry.UntilUTCMS = &until
		}
		if entry.FromUTCMS < to && (entry.UntilUTCMS == nil || *entry.UntilUTCMS > from) {
			selected = append(selected, entry)
		}
	}
	return selected
}

// Try the actual offsets around the wall date, then require a unique inverse
// under a rule effective at that UTC instant. DST repeated/nonexistent times
// and expired observed offsets cannot become supposedly confirmed markers.
func trendEventUTC(wall uint32, entries []timeRuleEntry) (int64, bool) {
	candidates := map[int64]bool{}
	for _, entry := range entries {
		if entry.Kind != "iana" && entry.Kind != "broker_us_dst" && (entry.Kind != "observed_offset" || entry.UntilUTCMS == nil) {
			continue
		}
		offsets := map[int]bool{}
		for hours := -36; hours <= 36; hours += 6 {
			probe := time.Unix(int64(wall)+int64(hours)*3600, 0)
			zone, _, err := serverLocation(entry, probe)
			if err == nil {
				_, offset := probe.In(zone).Zone()
				offsets[offset] = true
			}
		}
		for offset := range offsets {
			instant := time.Unix(int64(wall)-int64(offset), 0)
			stamp := instant.UnixMilli()
			if stamp < entry.FromUTCMS || entry.UntilUTCMS != nil && stamp >= *entry.UntilUTCMS {
				continue
			}
			zone, _, err := serverLocation(entry, instant)
			if err == nil {
				_, actual := instant.In(zone).Zone()
				if actual == offset {
					candidates[stamp] = true
				}
			}
		}
	}
	if len(candidates) == 1 {
		for stamp := range candidates {
			return stamp, true
		}
	}
	return 0, false
}
