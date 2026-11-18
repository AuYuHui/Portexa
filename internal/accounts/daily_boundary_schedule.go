package accounts

import "time"

var beijingDay = time.FixedZone("Asia/Shanghai", 8*60*60)

// Daily analytics uses Beijing midnight, independently of the broker's clock.
// Keep this burst short and retain the existing 30-second minimum cadence so
// history/calibration still have their full admission budget between snapshots.
func dailyBoundaryWindow(at time.Time) (time.Time, time.Time) {
	local := at.In(beijingDay)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, beijingDay)
	if !at.Before(midnight.Add(2 * time.Minute)) {
		midnight = midnight.AddDate(0, 0, 1)
	}
	return midnight.Add(-2 * time.Minute), midnight.Add(2 * time.Minute)
}

func dailyBoundaryInterval(at time.Time, interval time.Duration) time.Duration {
	start, end := dailyBoundaryWindow(at)
	if !at.Before(start) && at.Before(end) {
		return min(interval, 30*time.Second)
	}
	return interval
}

func nextDailyBoundarySnapshot(at, regular time.Time) time.Time {
	start, end := dailyBoundaryWindow(at)
	if at.Before(start) {
		if start.Before(regular) {
			return start
		}
		return regular
	}
	if !at.Before(end) {
		return regular
	}
	// Target :27/:57, allowing a typical short Fetch to finish just before
	// the minute boundary. Slow Fetches keep the 27-second history reservation;
	// this improves boundary evidence without promising an exact midnight quote.
	const cycle, phase = int64(30_000), int64(27_000)
	next := ((at.UnixMilli()-phase)/cycle+1)*cycle + phase
	earliest := at.Add(DailyAdmissionLead + 2*time.Second).UnixMilli()
	burst := time.UnixMilli(max(next, earliest))
	return burst
}
