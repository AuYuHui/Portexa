package marketrest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const LearningVersion = "weekend-learning-v1"

type Observation struct {
	ID                           int64
	Stream, Snapshot, Previous   string
	From, To, PreviousTo, MaxGap int64
	Healthy, Comparable, Changed bool
}

type Weekend struct {
	Key         string
	End         int64
	Close, Open int
	Evidence    []int64
}

type Learned struct {
	Close, Open, CloseMin, CloseMax, OpenMin, OpenMax int
	Through, Expires                                  int64
	Keys                                              []string
	Digest                                            string
}

// LearningBounds uses civil New York dates, including DST transition weekends.
func LearningBounds(at time.Time) (time.Time, time.Time) {
	local := at.In(newYork)
	days := (int(local.Weekday()) - int(time.Friday) + 7) % 7
	f := time.Date(local.Year(), local.Month(), local.Day()-days, 0, 0, 0, 0, newYork)
	return f.Add(16 * time.Hour), time.Date(f.Year(), f.Month(), f.Day()+2, 20, 0, 0, 0, newYork)
}

func LearnedWindow(at time.Time, closeMinute, openMinute int) (time.Time, time.Time) {
	start, _ := LearningBounds(at)
	f := start.In(newYork)
	return time.Date(f.Year(), f.Month(), f.Day()+closeMinute/1440, closeMinute%1440/60, closeMinute%60, 0, 0, newYork),
		time.Date(f.Year(), f.Month(), f.Day()+openMinute/1440, openMinute%1440/60, openMinute%60, 0, 0, newYork)
}
func civilMinute(ms int64, friday time.Time) int {
	t := time.UnixMilli(ms).In(newYork)
	// Calendar midnight UTC gives a civil day count unaffected by DST.
	a := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(friday.Year(), friday.Month(), friday.Day(), 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b)/(24*time.Hour))*1440 + t.Hour()*60 + t.Minute()
}

// CompleteWeekends never joins different account/config/unit streams. Other
// healthy streams may only contradict, never fill missing observations.
func CompleteWeekends(rows []Observation, now time.Time) []Weekend {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].To == rows[j].To {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].To < rows[j].To
	})
	out := []Weekend{}
	for offset := 8; offset >= 0; offset-- {
		from, to := LearningBounds(now.AddDate(0, 0, -7*offset))
		if to.After(now) || to.Before(now.AddDate(0, 0, -56)) {
			continue
		}
		streams := map[string][]Observation{}
		for _, r := range rows {
			if r.To >= from.UnixMilli() && r.PreviousTo <= to.UnixMilli() {
				streams[r.Stream] = append(streams[r.Stream], r)
			}
		}
		candidates := []Weekend{}
		keys := make([]string, 0, len(streams))
		for key := range streams {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			stream := streams[key]
			if w, ok := completeStream(stream, from, to); ok {
				candidates = append(candidates, w)
			}
		}
		if len(candidates) == 0 {
			continue
		}

		w := candidates[0]
		closeMin, closeMax, openMin, openMax := w.Close, w.Close, w.Open, w.Open
		for _, c := range candidates[1:] {
			closeMin = min(closeMin, c.Close)
			closeMax = max(closeMax, c.Close)
			openMin = min(openMin, c.Open)
			openMax = max(openMax, c.Open)
			w.Close = max(w.Close, c.Close)
			w.Open = min(w.Open, c.Open)
			w.Evidence = append(w.Evidence, c.Evidence...)
		}
		sort.Slice(w.Evidence, func(i, j int) bool { return w.Evidence[i] < w.Evidence[j] })
		valid := closeMax-closeMin <= 15 && openMax-openMin <= 15
		a, b := LearnedWindow(from, w.Close, w.Open)
		for _, r := range rows {
			if !r.Comparable || !r.Changed {
				continue
			}
			if r.PreviousTo < b.UnixMilli() && r.To > a.UnixMilli() {
				// Boundary changes themselves define the uncertainty interval.
				if r.PreviousTo >= a.UnixMilli() && r.To <= b.UnixMilli() {
					valid = false
				}
			}
		}
		if valid {
			out = append(out, w)
		}
	}
	return out
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func completeStream(rows []Observation, from, to time.Time) (Weekend, bool) {
	w := Weekend{Key: from.Format("2006-01-02"), End: to.UnixMilli()}
	if len(rows) < 6 {
		return w, false
	}
	cursor := from.UnixMilli()
	last := ""
	changes := []int{}
	for i, r := range rows {
		if r.To < cursor {
			continue
		}
		if !r.Healthy || !r.Comparable || r.PreviousTo > cursor || r.To-r.PreviousTo > r.MaxGap || r.PreviousTo >= r.To || (last != "" && r.Previous != last) {
			return w, false
		}
		cursor = r.To
		last = r.Snapshot
		w.Evidence = append(w.Evidence, r.ID)
		if r.Changed {
			changes = append(changes, i)
		}
		if cursor >= to.UnixMilli() {
			break
		}
	}
	if cursor < to.UnixMilli() {
		return w, false
	}
	best := int64(0)
	left, right := -1, -1
	for i := 1; i < len(changes); i++ {
		a, b := rows[changes[i-1]], rows[changes[i]]
		gap := b.PreviousTo - a.To
		if gap > best {
			best = gap
			left = changes[i-1]
			right = changes[i]
		}
	}
	if best < int64(6*time.Hour/time.Millisecond) || left < 0 {
		return w, false
	}
	before, after := 0, 0
	for _, i := range changes {
		if rows[i].To <= rows[left].To && rows[i].To >= rows[left].To-int64(time.Hour/time.Millisecond) {
			before++
		}
		if rows[i].PreviousTo >= rows[right].PreviousTo && rows[i].To <= rows[right].To+int64(time.Hour/time.Millisecond) {
			after++
		}
	}
	if before < 2 || after < 2 {
		return w, false
	}
	// Round close up and open down: the unsampled part never becomes evidence.
	w.Close = civilMinute(rows[left].To+59_999, from)
	w.Open = civilMinute(rows[right].PreviousTo, from)
	return w, w.Open > w.Close
}

func Train(weeks []Weekend) (Learned, bool) {
	m := Learned{}
	if len(weeks) < 3 {
		return m, false
	}
	weeks = weeks[:3]
	m.CloseMin, m.CloseMax = weeks[0].Close, weeks[0].Close
	m.OpenMin, m.OpenMax = weeks[0].Open, weeks[0].Open
	for _, w := range weeks {
		m.CloseMin = min(m.CloseMin, w.Close)
		m.CloseMax = max(m.CloseMax, w.Close)
		m.OpenMin = min(m.OpenMin, w.Open)
		m.OpenMax = max(m.OpenMax, w.Open)
		m.Keys = append(m.Keys, w.Key)
	}
	if m.CloseMax-m.CloseMin > 15 || m.OpenMax-m.OpenMin > 15 {
		return m, false
	}
	m.Close = max(17*60, m.CloseMax+15)
	m.Open = min(2*1440+17*60, m.OpenMin-15)
	m.Through = weeks[2].End
	m.Expires = weeks[0].End + int64(56*24*time.Hour/time.Millisecond)
	raw, _ := json.Marshal(weeks)
	sum := sha256.Sum256(raw)
	m.Digest = hex.EncodeToString(sum[:])
	return m, m.Open-m.Close >= 360
}
func Validate(m Learned, w Weekend, rows []Observation) bool {
	if w.End <= m.Through || abs(w.Close-m.CloseMin) > 15 || abs(w.Close-m.CloseMax) > 15 || abs(w.Open-m.OpenMin) > 15 || abs(w.Open-m.OpenMax) > 15 {
		return false
	}
	date, err := time.ParseInLocation("2006-01-02", w.Key, newYork)
	if err != nil {
		return false
	}
	from, to := LearnedWindow(date, m.Close, m.Open)
	for _, r := range rows {
		if r.Comparable && r.Changed && r.PreviousTo < to.UnixMilli() && r.To > from.UnixMilli() {
			return false
		}
	}
	return true
}
func StreamKey(account string, config, unit int64) string {
	return fmt.Sprintf("%s:%d:%d", account, config, unit)
}
