// Package marketrest defines the bounded, versioned weekend sampling template.
// It is a collection policy, not a broker session discovery API.
package marketrest

import (
	"strings"
	"time"
	_ "time/tzdata"
)

const Template = "fx_metals_weekend_v1"
const Off = "off"
const Interval = 5 * time.Minute
const HistoryInterval = 15 * time.Minute
const Grace = 90 * time.Second

var newYork, _ = time.LoadLocation("America/New_York")
var beijing = time.FixedZone("Asia/Shanghai", 8*3600)

// Window deliberately stays inside the usual FX/metals weekend closure. It
// handles New York's DST in calendar dates, rather than adding a fixed 48h.
func Window(at time.Time) (from, to time.Time, closed bool) {
	local := at.In(newYork)
	days := (int(local.Weekday()) - int(time.Friday) + 7) % 7
	friday := local.AddDate(0, 0, -days)
	from = time.Date(friday.Year(), friday.Month(), friday.Day(), 18, 0, 0, 0, newYork)
	sunday := from.AddDate(0, 0, 2)
	to = time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 16, 0, 0, 0, newYork)
	return from, to, !at.Before(from) && at.Before(to)
}

func Reduced(at time.Time) bool {
	from, to, closed := Window(at)
	if !closed || at.Before(from.Add(5*time.Minute)) || !at.Before(to.Add(-5*time.Minute)) {
		return false
	}
	local := at.In(beijing)
	minutes := local.Hour()*60 + local.Minute()
	return minutes >= 2 && minutes < 24*60-2
}

// NextGuard is the first upcoming high-frequency boundary. The scheduler must
// wake before it even if the next regular five-minute phase would be later.
func NextGuard(at time.Time) time.Time {
	_, end, _ := Window(at)
	local := at.In(beijing)
	guard := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, beijing).Add(-2 * time.Minute)
	if !guard.After(at) {
		guard = guard.AddDate(0, 0, 1)
	}
	if end.Add(-5 * time.Minute).Before(guard) {
		guard = end.Add(-5 * time.Minute)
	}
	return guard
}

// SupportedSymbol accepts a narrow set of FX/metals with an optional broker
// suffix. Unknown symbols, including crypto, never activate this template.
func SupportedSymbol(symbol string) bool {
	base := strings.ToUpper(strings.TrimSpace(symbol))
	if i := strings.IndexAny(base, "._-/"); i >= 0 {
		base = base[:i]
	}
	if len(base) != 6 {
		return false
	}
	currencies := map[string]bool{"USD": true, "EUR": true, "GBP": true, "JPY": true, "CHF": true,
		"CAD": true, "AUD": true, "NZD": true, "CNH": true, "HKD": true, "SGD": true,
		"NOK": true, "SEK": true, "DKK": true, "ZAR": true, "MXN": true, "TRY": true,
		"PLN": true, "CZK": true, "HUF": true, "THB": true}
	return (currencies[base[:3]] || base[:3] == "XAU" || base[:3] == "XAG") && currencies[base[3:]]
}
