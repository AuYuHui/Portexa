package accounts

import (
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

var orderHistoryBeijing = time.FixedZone("Asia/Shanghai", 8*3600)

type historyPeriod struct {
	from, to       int64
	hasFrom, hasTo bool
}

func (p historyPeriod) dated() bool { return p.hasFrom || p.hasTo }

func orderHistoryPeriod(f OrderHistoryFilter, now time.Time) (historyPeriod, error) {
	invalid := &InputError{"订单历史筛选或页码无效"}
	p := historyPeriod{to: now.UnixMilli() + 1, hasFrom: f.From != "", hasTo: f.To != ""}
	if f.Page < 1 || f.Ticket < 0 || !utf8.ValidString(f.Symbol) || len(f.Symbol) > 128 ||
		(f.Side != "" && f.Side != "buy" && f.Side != "sell") ||
		(f.Result != "" && f.Result != "profit" && f.Result != "loss" && f.Result != "flat") {
		return p, invalid
	}
	if p.hasFrom {
		d, err := time.ParseInLocation(time.DateOnly, f.From, orderHistoryBeijing)
		if err != nil || d.UnixMilli() < 0 {
			return p, invalid
		}
		p.from = d.UnixMilli()
	}
	if p.hasTo {
		d, err := time.ParseInLocation(time.DateOnly, f.To, orderHistoryBeijing)
		if err != nil || d.AddDate(0, 0, 1).UnixMilli() <= 0 {
			return p, invalid
		}
		p.to = d.AddDate(0, 0, 1).UnixMilli()
	}
	if p.to <= p.from {
		return p, invalid
	}
	return p, nil
}

// Bound calibration lookups even when browsing years of orders. A block
// includes adjacent days to resolve raw wall clocks around either edge.
type orderHistoryClock struct {
	tx     *gorm.DB
	server storage.Server
	blocks map[int64][]timeRuleEntry
}

func (clock *orderHistoryClock) entries(stamp int64) ([]timeRuleEntry, error) {
	const day = int64(24 * time.Hour / time.Millisecond)
	const week = 7 * day
	block := stamp / week * week
	if entries, ok := clock.blocks[block]; ok {
		return entries, nil
	}
	entries, _, err := historicalTimeEntries(clock.tx.Statement.Context, clock.tx, clock.server, max(0, block-3*day), block+week+3*day)
	if err == nil {
		clock.blocks[block] = entries
	}
	return entries, err
}

func (clock *orderHistoryClock) resolve(o storage.HistoryOrder) (int64, bool, error) {
	entries, err := clock.entries(orderWallMS(o))
	if err != nil {
		return 0, false, err
	}
	stamp, ok := historyOrderUTC(o, entries)
	return stamp, ok, nil
}

// Even if a clock rule changes at midnight, every possible server wall
// timestamp for this Beijing interval lies within its two boundary envelopes.
func (p historyPeriod) wallBounds(clock *orderHistoryClock) ([2]int64, error) {
	const twoDays = int64(48 * time.Hour / time.Millisecond)
	bounds := [2]int64{0, 0}
	if p.hasFrom {
		entries, err := clock.entries(p.from)
		if err != nil {
			return bounds, err
		}
		bounds[0] = p.from + 14*time.Hour.Milliseconds()
		for _, r := range eventWallRanges(entries, p.from, min(p.from+twoDays, p.to)) {
			bounds[0] = min(bounds[0], r[0])
		}
	}
	entries, err := clock.entries(p.to - 1)
	if err != nil {
		return bounds, err
	}
	for _, r := range eventWallRanges(entries, max(p.from, p.to-twoDays), p.to) {
		bounds[1] = max(bounds[1], r[1])
	}
	return bounds, nil
}

func orderHistoryCoverage(clock *orderHistoryClock, a storage.Account, period historyPeriod, now time.Time) (bool, *int64, error) {
	var windows []analyticsCoverage
	q := clock.tx.Table("history_coverage c").
		Joins("JOIN history_runs r ON r.coverage_id=c.id AND r.account_id=c.account_id AND r.state='succeeded'").
		Where("c.account_id=? AND c.unit_revision=? AND c.checked_at_ms IS NOT NULL", a.ID, a.UnitRevision)
	if clock.server.Protocol == "mt5" {
		q = q.Where("r.completion_reason=?", mt5HistoryComplete)
	}
	if err := q.Select("c.*,MAX(r.started_at_ms) AS source_through_ms").Group("c.id").Order("c.start_wall").Scan(&windows).Error; err != nil {
		return false, nil, err
	}
	var latest int64
	for _, win := range windows {
		latest = max(latest, win.SourceThroughMS)
	}
	if latest <= 0 {
		return false, nil, nil
	}
	if period.hasTo && period.to > now.In(orderHistoryBeijing).AddDate(0, 0, 1).UnixMilli() {
		return false, &latest, nil
	}
	if period.to > now.UnixMilli() {
		// Today's ledger is complete only through the latest history read.
		// A past selected day must still be covered through its full end.
		period.to = min(latest, now.UnixMilli()) / 1000 * 1000
	}
	if period.to <= period.from {
		return false, &latest, nil
	}
	bounds, err := period.wallBounds(clock)
	if err != nil {
		return false, nil, err
	}
	through, required := max(int64(0), bounds[0]/1000), (bounds[1]+999)/1000
	for _, win := range windows {
		if int64(win.StartWall) > through {
			break
		}
		end := int64(win.EndWall)
		if win.CheckedThroughWall != nil {
			end = min(end, int64(*win.CheckedThroughWall))
		}
		if end <= through {
			continue
		}
		// A historic request cannot certify future rows in a broker's
		// wall-clock window, even when it requested the whole current day.
		earliestWall := win.SourceThroughMS/1000 - 12*3600
		if end > earliestWall {
			entries, err := clock.entries(win.SourceThroughMS)
			if err != nil {
				return false, nil, err
			}
			if wall, ok := serverWallTime(entries, win.SourceThroughMS); ok {
				end = min(end, int64(wall))
			} else {
				end = min(end, earliestWall)
			}
		}
		through = max(through, end)
		if through >= required {
			return true, &latest, nil
		}
	}
	return false, &latest, nil
}
