package accounts

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

// A measured offset is a bounded historical fact, never an annual calendar.
// Rebuild its validity from immutable accepted observations rather than today's
// mutable calibration status. Expiry, conflicts and configuration changes must
// not turn an old open-ended ruleset into unlimited historical evidence.
func historicalTimeEntries(ctx context.Context, tx *gorm.DB, server storage.Server, from, to int64) ([]timeRuleEntry, string, error) {
	var rules []serverTimeRule
	if err := tx.WithContext(ctx).Where("server_id=? AND verified_at_ms IS NOT NULL", server.ID).
		Order("revision").Find(&rules).Error; err != nil {
		return nil, "", err
	}
	entries := trendTimeEntries(rules, from, to)
	measured := false
	for _, entry := range entries {
		measured = measured || entry.Kind == "observed_offset"
	}
	var windows []timeRuleEntry
	if measured {
		var observations []storage.ClockObservation
		if err := tx.WithContext(ctx).Table("trade_clock_observations AS o").Select("o.*").
			Joins("JOIN accounts AS a ON a.id=o.account_id AND a.server_id=o.server_id AND a.config_version=o.config_version AND a.pinned_endpoint_id=o.endpoint_id").
			Joins("JOIN server_endpoints AS e ON e.id=o.endpoint_id AND e.state='reviewed'").
			Where("o.server_id=? AND o.state='accepted' AND a.removed_at_ms IS NULL AND o.window_to_ms>? AND o.checked_at_ms<?",
				server.ID, from-clockEvidenceTTL.Milliseconds(), to).
			Order("o.checked_at_ms,o.window_to_ms,o.id").Limit(50_001).Find(&observations).Error; err != nil {
			return nil, "", err
		}
		if len(observations) > 50_000 {
			return nil, "", &InputError{"时间校准依据过多，请缩短查询范围"}
		}
		var err error
		windows, err = measuredTimeWindows(ctx, server, observations)
		if err != nil {
			return nil, "", err
		}
	}
	bounded := []timeRuleEntry{}
	for _, entry := range entries {
		entry.FromUTCMS = max(from, entry.FromUTCMS)
		until := to
		if entry.UntilUTCMS != nil {
			until = min(until, *entry.UntilUTCMS)
		}
		entry.UntilUTCMS = &until
		if entry.Kind != "observed_offset" {
			bounded = append(bounded, entry)
			continue
		}
		for _, window := range windows {
			if window.OffsetMinutes != entry.OffsetMinutes || entry.PolicyID != "trade-events-v1" {
				continue
			}
			a, b := max(entry.FromUTCMS, window.FromUTCMS), min(until, *window.UntilUTCMS)
			if a < b {
				copy := entry
				copy.FromUTCMS, copy.UntilUTCMS = a, &b
				bounded = append(bounded, copy)
			}
		}
	}
	sort.Slice(bounded, func(i, j int) bool { return bounded[i].FromUTCMS < bounded[j].FromUTCMS })
	evidence, err := json.Marshal(bounded)
	return bounded, string(evidence), err
}

type clockEvidenceEvent struct {
	at     int64
	index  int
	remove bool
}

func measuredTimeWindows(ctx context.Context, server storage.Server, observations []storage.ClockObservation) ([]timeRuleEntry, error) {
	events := []clockEvidenceEvent{}
	for i, o := range observations {
		if o.OffsetMinutes == nil || o.CheckedAtMS == nil || *o.CheckedAtMS < o.WindowToMS ||
			!sameOfficialClockPeriod(server, time.UnixMilli(o.WindowFromMS), time.UnixMilli(o.WindowToMS)) {
			continue
		}
		until := o.WindowToMS + clockEvidenceTTL.Milliseconds()
		// A known DST transition terminates the measurement even without a
		// subsequent trade. Each evidence lifetime is at most 24 hours.
		if !sameOfficialClockPeriod(server, time.UnixMilli(o.WindowToMS), time.UnixMilli(until-1)) {
			lo, hi := o.WindowToMS, until
			for lo+1 < hi {
				mid := lo + (hi-lo)/2
				if sameOfficialClockPeriod(server, time.UnixMilli(o.WindowToMS), time.UnixMilli(mid)) {
					lo = mid
				} else {
					hi = mid
				}
			}
			until = hi
		}
		if *o.CheckedAtMS < until {
			events = append(events, clockEvidenceEvent{at: *o.CheckedAtMS, index: i}, clockEvidenceEvent{at: until, index: i, remove: true})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].at != events[j].at {
			return events[i].at < events[j].at
		}
		return events[i].remove && !events[j].remove
	})
	active := map[int]bool{}
	tickets, windows := map[int32]int{}, map[string]int{}
	result := []timeRuleEntry{}
	offset, opened, start := 0, false, int64(0)
	closeWindow := func(at int64) {
		if opened && at > start {
			end := at
			result = append(result, timeRuleEntry{FromUTCMS: start, UntilUTCMS: &end,
				Kind: "observed_offset", PolicyID: "trade-events-v1", OffsetMinutes: offset})
		}
		opened = false
	}
	for i := 0; i < len(events); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		at := events[i].at
		for i < len(events) && events[i].at == at {
			e := events[i]
			o := observations[e.index]
			key := o.AccountID + ":" + o.AfterSnapshotID
			if e.remove {
				if active[e.index] {
					delete(active, e.index)
					tickets[o.Ticket]--
					windows[key]--
					if tickets[o.Ticket] == 0 {
						delete(tickets, o.Ticket)
					}
					if windows[key] == 0 {
						delete(windows, key)
					}
				}
			} else {
				if (len(active) > 0 || opened) && offset != *o.OffsetMinutes {
					closeWindow(min(at, o.WindowFromMS))
					active, tickets, windows = map[int]bool{}, map[int32]int{}, map[string]int{}
				}
				offset = *o.OffsetMinutes
				active[e.index] = true
				tickets[o.Ticket]++
				windows[key]++
			}
			i++
		}
		qualified := len(tickets) >= ClockRequiredEvents && len(windows) >= 2
		if opened && !qualified {
			closeWindow(at)
		}
		if !opened && qualified {
			start, opened = at, true
		}
	}
	return result, nil
}

// Conservative raw wall-clock ranges for the requested UTC interval. Unknown
// calendar gaps retain the full supported offset envelope; known portions use
// their actual offsets. Unresolvable candidates outside all these ranges cannot
// block a different, fully calibrated interval just because SQL read padding.
func eventWallRanges(entries []timeRuleEntry, from, to int64) [][2]int64 {
	ranges := [][2]int64{}
	cursor := from
	for _, entry := range entries {
		a, b := max(from, entry.FromUTCMS), to
		if entry.UntilUTCMS != nil {
			b = min(b, *entry.UntilUTCMS)
		}
		if a >= b {
			continue
		}
		if a > cursor {
			ranges = append(ranges, [2]int64{cursor - 12*int64(time.Hour/time.Millisecond), a + 14*int64(time.Hour/time.Millisecond)})
		}
		lo, hi := 14*3600, -12*3600
		for at := a; ; at = min(at+int64(6*time.Hour/time.Millisecond), b-1) {
			zone, _, err := serverLocation(entry, time.UnixMilli(at))
			if err == nil {
				_, seconds := time.UnixMilli(at).In(zone).Zone()
				lo, hi = min(lo, seconds), max(hi, seconds)
			}
			if at == b-1 {
				break
			}
		}
		if lo <= hi {
			ranges = append(ranges, [2]int64{a + int64(lo)*1000, b + int64(hi)*1000})
		}
		cursor = max(cursor, b)
	}
	if cursor < to {
		ranges = append(ranges, [2]int64{cursor - 12*int64(time.Hour/time.Millisecond), to + 14*int64(time.Hour/time.Millisecond)})
	}
	return ranges
}

func wallCouldAffect(wall uint32, ranges [][2]int64) bool {
	stamp := int64(wall) * 1000
	for _, r := range ranges {
		if stamp+999 >= r[0] && stamp < r[1] {
			return true
		}
	}
	return false
}
