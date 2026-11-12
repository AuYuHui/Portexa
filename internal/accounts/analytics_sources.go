package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/analytics"
	"portexa/internal/storage"
)

type analyticsCoverage struct {
	storage.HistoryCoverage
	SourceThroughMS int64
}

type analyticsSource struct {
	account    storage.Account
	server     storage.Server
	entries    []timeRuleEntry
	coverage   []analyticsCoverage
	input      analytics.Input
	cutoff     int64
	hash       hash.Hash
	readOrders int
}

func (s *Service) analyticsSource(ctx context.Context, account storage.Account, from, to int64, unit string) (*analyticsSource, error) {
	source := &analyticsSource{account: account, cutoff: to, hash: sha256.New()}
	source.input = analytics.Input{AccountID: account.ID, Label: account.Label, Currency: "USD", FromMS: from, ToMS: to,
		Events: []analytics.Event{}, LedgerReasons: []string{}}
	if unit == "native" && account.Currency != nil {
		source.input.Currency = *account.Currency
	}
	scaleKey := "unconfirmed"
	if account.USDScale != nil {
		scaleKey = account.USDScale.String()
	}
	fmt.Fprintf(source.hash, "account:%s:%d:%s:%s|", account.ID, account.UnitRevision, source.input.Currency, scaleKey)
	if account.Currency == nil || account.UnitVerifiedAtMS == nil || unit == "USD" && account.USDScale == nil {
		source.input.LedgerReasons = append(source.input.LedgerReasons, "unit_unconfirmed")
	}
	if err := s.Store.Read.WithContext(ctx).First(&source.server, "id=?", account.ServerID).Error; err != nil {
		return nil, err
	}
	var rules []serverTimeRule
	if err := s.Store.Read.WithContext(ctx).Where("server_id=? AND verified_at_ms IS NOT NULL", account.ServerID).
		Order("revision").Find(&rules).Error; err != nil {
		return nil, err
	}
	source.entries = trendTimeEntries(rules, 0, 2147483647_000)
	if len(source.entries) == 0 {
		source.input.LedgerReasons = append(source.input.LedgerReasons, "time_unresolved")
	}
	for _, rule := range rules {
		fmt.Fprintf(source.hash, "rule:%s:%s|", rule.ID, rule.RulesJSON)
	}
	query := s.Store.Read.WithContext(ctx).Table("history_coverage c").
		Joins("JOIN history_runs r ON r.coverage_id=c.id AND r.account_id=c.account_id AND r.state='succeeded'").
		Where("c.account_id=? AND c.unit_revision=?", account.ID, account.UnitRevision).
		Select("c.*,MAX(r.started_at_ms) AS source_through_ms").Group("c.id").Order("c.start_wall,c.end_wall")
	if err := query.Scan(&source.coverage).Error; err != nil {
		return nil, err
	}
	for _, c := range source.coverage {
		fmt.Fprintf(source.hash, "coverage:%s:%d:%d:%d|", c.ID, c.StartWall, c.EndWall, c.SourceThroughMS)
	}
	if wall, ok := source.wall(to - 1); ok {
		through := int64(0)
		for _, c := range source.coverage {
			if c.StartWall <= wall && c.EndWall > wall {
				through = max(through, c.SourceThroughMS)
			}
		}
		if through > 0 {
			source.cutoff = min(to, through/analytics.Minute*analytics.Minute)
		}
	}
	var last storage.Snapshot
	latest := s.Store.Read.WithContext(ctx).Select("observed_to_ms").Where("account_id=? AND observed_to_ms<=?", account.ID, source.cutoff).
		Order("observed_to_ms DESC,id DESC").Limit(1).Find(&last)
	if latest.Error != nil {
		return nil, latest.Error
	}
	if latest.RowsAffected > 0 {
		source.cutoff = min(source.cutoff, (last.ObservedToMS+analytics.Minute-1)/analytics.Minute*analytics.Minute)
	}
	if err := s.analyticsEvents(ctx, source, from-analytics.MaxAge-1000, to, unit); err != nil {
		return nil, err
	}
	return source, nil
}

func (source *analyticsSource) wall(stamp int64) (uint32, bool) {
	for _, entry := range source.entries {
		if stamp < entry.FromUTCMS || entry.UntilUTCMS != nil && stamp >= *entry.UntilUTCMS {
			continue
		}
		instant := time.UnixMilli(stamp)
		zone, _, err := serverLocation(entry, instant)
		if err != nil || entry.Kind != "iana" && entry.Kind != "broker_us_dst" {
			return 0, false
		}
		_, offset := instant.In(zone).Zone()
		wall := instant.Unix() + int64(offset)
		if wall < 0 || wall > 2147483647 {
			return 0, false
		}
		return uint32(wall), true
	}
	return 0, false
}

func (source *analyticsSource) covered(from, to int64) bool {
	a, okA := source.wall(from)
	b, okB := source.wall(to - 1)
	// A discontinuous or repeated server minute needs its own verified window.
	if !okA || !okB || int64(b)-int64(a) != (to-1)/1000-from/1000 {
		return false
	}
	end, through := uint64(b)+1, uint64(a)
	for _, c := range source.coverage {
		if c.SourceThroughMS < to {
			continue
		}
		if uint64(c.StartWall) <= through && uint64(c.EndWall) > through {
			through = uint64(c.EndWall)
		}
		if through >= end {
			return true
		}
	}
	return false
}

type analyticsOrder struct {
	storage.HistoryOrder
	ManualCategory      *string
	ManualBasedRevision *int64
	ManualRevision      *int64
}

func classificationKey(order analyticsOrder) (string, string) {
	if order.ManualCategory != nil && order.ManualBasedRevision != nil && *order.ManualBasedRevision == order.Revision {
		return *order.ManualCategory, fmt.Sprintf("manual:%d", *order.ManualRevision)
	}
	category, _, version := currentAutomaticFundResult(order.HistoryOrder)
	return category, "automatic:" + version
}

func (s *Service) analyticsEvents(ctx context.Context, source *analyticsSource, from, to int64, unit string) error {
	const batchSize = 1000
	var stamp uint32
	var ticket int32
	for count := 0; ; {
		var orders []analyticsOrder
		query := withClassification(currentHistory(s.Store.Read.WithContext(ctx), source.account.ID)).
			Where("o.command IN (0,1,6,7) AND o.event_time_wall>=? AND o.event_time_wall<?",
				max(0, from/1000-24*3600), to/1000+24*3600).
			Select("o.*,c.category AS manual_category,c.based_trade_revision AS manual_based_revision,c.revision AS manual_revision")
		if ticket != 0 {
			query = query.Where("(o.event_time_wall,o.ticket)>(?,?)", stamp, ticket)
		}
		if err := query.Order("o.event_time_wall,o.ticket").Limit(batchSize).Scan(&orders).Error; err != nil {
			return err
		}
		for _, order := range orders {
			count++
			source.readOrders = count
			if count > 200_000 {
				return &InputError{"分析区间交易过多，请缩短时间范围"}
			}
			category, classification := classificationKey(order)
			fmt.Fprintf(source.hash, "order:%d:%d:%x:%s:%s|", order.Ticket, order.Revision, order.SourceHash, category, classification)
			instant, ok := trendEventUTC(order.EventTimeWall, source.entries)
			if !ok {
				source.input.UnresolvedTimes++
				continue
			}
			if instant < from || instant >= to {
				continue
			}
			if order.UnitRevision != source.account.UnitRevision || source.account.Currency == nil || order.NativeCurrency != *source.account.Currency {
				source.input.LedgerReasons = append(source.input.LedgerReasons, "unit_changed")
				continue
			}
			amount := order.NetProfit
			if unit == "USD" {
				if source.account.USDScale == nil {
					continue
				}
				amount = amount.Mul(*source.account.USDScale)
			}
			kind, key := "trade", ""
			if order.Command == 7 {
				kind = "credit"
			}
			if order.Command == 6 {
				kind, key = classificationKey(order)
				if kind == "" {
					kind = "unclassified"
				}
				if (kind == "unclassified" || kind == "adjustment") && instant >= source.input.FromMS {
					source.input.Unclassified++
				}
			}
			fmt.Fprintf(source.hash, "effective:%d:%d:%s:%s|", order.Ticket, instant, kind, key)
			source.input.Events = append(source.input.Events, analytics.Event{TimeMS: instant, AccountID: source.account.ID,
				Ticket: order.Ticket, Revision: order.Revision, Kind: kind, Amount: amount, Symbol: order.Symbol,
				Classification: key, SourceRunID: order.SourceRunID})
		}
		if len(orders) < batchSize {
			break
		}
		last := orders[len(orders)-1]
		stamp, ticket = last.EventTimeWall, last.Ticket
	}
	sort.Slice(source.input.Events, func(i, j int) bool {
		a, b := source.input.Events[i], source.input.Events[j]
		if a.TimeMS != b.TimeMS {
			return a.TimeMS < b.TimeMS
		}
		return a.Ticket < b.Ticket
	})
	return nil
}

func (s *Service) analyticsSamples(ctx context.Context, source *analyticsSource, from, to int64, unit string) error {
	input := &source.input
	input.FromMS, input.ToMS = from, to
	input.Unclassified = 0
	for _, event := range input.Events {
		if event.TimeMS >= from && event.TimeMS < to && (event.Kind == "unclassified" || event.Kind == "adjustment") {
			input.Unclassified++
		}
	}
	count := max(0, int((to-from)/analytics.Minute)+1)
	input.Samples, input.Coverage = make([]analytics.Sample, count), make([]bool, max(0, count-1))
	index := 0
	var previous *storage.Snapshot
	fill := func(until int64) {
		for index < count && from+int64(index)*analytics.Minute < until {
			at := from + int64(index)*analytics.Minute
			p := analytics.Sample{TimeMS: at, Reason: "missing_samples", Expected: 1, Tolerance: analytics.Tolerance}
			if previous != nil {
				p.ID, p.ObservedFromMS, p.ObservedToMS = previous.ID, previous.ObservedFromMS, previous.ObservedToMS
				if at-previous.ObservedToMS <= analytics.MaxAge && previous.ObservedToMS-previous.ObservedFromMS <= analytics.MaxAge {
					p.Reason = ""
					if previous.CollectionIntervalSeconds == nil {
						p.Reason = "unknown_cadence"
					}
					if previous.UnitRevision != source.account.UnitRevision || source.account.Currency == nil || previous.NativeCurrency != *source.account.Currency {
						p.Reason = "unit_changed"
					}
					scale := decimal.NewFromInt(1)
					if unit == "USD" {
						if previous.USDScale == nil {
							p.Reason = "unit_unconfirmed"
						} else {
							scale = *previous.USDScale
						}
					}
					p.Balance, p.Credit, p.Equity, p.Floating = previous.Balance.Mul(scale), previous.Credit.Mul(scale), previous.Equity.Mul(scale), previous.FloatingNetProfit.Mul(scale)
					p.Tolerance = analytics.Tolerance.Mul(scale)
					if p.Reason == "" {
						p.Included = 1
					}
				}
			}
			input.Samples[index] = p
			if index < len(input.Coverage) {
				input.Coverage[index] = source.covered(at, at+analytics.Minute)
			}
			index++
		}
	}
	err := s.visitTrendSnapshots(ctx, source.account.ID, max(0, from-analytics.MaxAge), to+1, func(row storage.Snapshot) error {
		fill(row.ObservedToMS)
		input.RawSamples++
		fmt.Fprintf(source.hash, "sample:%s:%d:%d:%s:%s:%s:%s:%d|", row.ID, row.ObservedFromMS, row.ObservedToMS,
			row.Balance, row.Credit, row.Equity, row.FloatingNetProfit, row.UnitRevision)
		previous = &row
		return nil
	})
	if err != nil {
		return err
	}
	fill(to + 1)
	input.Fingerprint = hex.EncodeToString(source.hash.Sum(nil))
	return nil
}

func analyticsRevision(tx *gorm.DB) (int64, error) {
	var revision int64
	err := tx.Table("analytics_revision").Where("id=1").Pluck("revision", &revision).Error
	return revision, err
}
