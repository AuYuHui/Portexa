package accounts

import (
	"time"

	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

// A 24-hour active lifetime can span one weekend of at most 47 hours. Read a
// bounded superset, then apply the same per-observation expiry in both readers.
const clockEvidenceLookback = clockEvidenceTTL + 48*time.Hour

func clockEvidenceExpiry(server storage.Server, observation storage.ClockObservation) int64 {
	observed := time.UnixMilli(observation.WindowToMS)
	until := observed.Add(clockEvidenceTTL)
	if marketrest.SupportedSymbol(observation.Symbol) {
		until = clockAfterActiveTime(observed, clockEvidenceTTL)
	}
	// The pause is not an annual broker clock rule. A known DST change still
	// terminates the old offset, including when that change occurs during rest.
	if !sameOfficialClockPeriod(server, observed, until.Add(-time.Millisecond)) {
		lo, hi := observed.UnixMilli(), until.UnixMilli()
		for lo+1 < hi {
			mid := lo + (hi-lo)/2
			if sameOfficialClockPeriod(server, observed, time.UnixMilli(mid)) {
				lo = mid
			} else {
				hi = mid
			}
		}
		return hi
	}
	return until.UnixMilli()
}

func clockAfterActiveTime(at time.Time, remaining time.Duration) time.Time {
	for remaining > 0 {
		from, to, closed := marketrest.Window(at)
		if closed {
			at = to
			continue
		}
		if !from.After(at) {
			from, to, _ = marketrest.Window(at.AddDate(0, 0, 7))
		}
		active := from.Sub(at)
		if remaining <= active {
			return at.Add(remaining)
		}
		remaining -= active
		at = to
	}
	return at
}

// If the first evidence after a closure contradicts the carried offset, the
// change may have happened during rest. Preserve earlier confirmed history,
// but do not pretend the old offset was verified across that unobserved gap.
func clockConflictBoundary(at int64, observations []storage.ClockObservation, active map[int]bool) int64 {
	from, to, _ := marketrest.Window(time.UnixMilli(at))
	if from.UnixMilli() > at {
		return at
	}
	latest, eligible := int64(0), false
	for index := range active {
		o := observations[index]
		latest = max(latest, o.WindowToMS)
		eligible = eligible || marketrest.SupportedSymbol(o.Symbol)
	}
	if eligible && latest < to.UnixMilli() {
		return min(at, max(from.UnixMilli(), latest))
	}
	return at
}
