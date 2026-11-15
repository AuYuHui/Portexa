package app_test

import (
	"fmt"
	"testing"
	"time"

	"portexa/internal/marketrest"
)

func learningRows(t *testing.T) ([]marketrest.Observation, time.Time) {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	rows := []marketrest.Observation{}
	id := int64(0)
	for week := 0; week < 4; week++ {
		from, to := marketrest.LearningBounds(time.Date(2027, 2, 13+7*week, 12, 0, 0, 0, ny))
		close, open := marketrest.LearnedWindow(from, 17*60, 2*1440+17*60)
		previous := ""
		for at := from; !at.After(to); at = at.Add(5 * time.Minute) {
			id++
			snapshot := fmt.Sprint(id)
			rows = append(rows, marketrest.Observation{ID: id, Stream: "account:1:1", Snapshot: snapshot, Previous: previous, From: at.Add(-time.Second).UnixMilli(), To: at.UnixMilli(), PreviousTo: at.Add(-5 * time.Minute).UnixMilli(), MaxGap: 390000, Healthy: true, Comparable: true, Changed: !at.After(close) || !at.Before(open)})
			previous = snapshot
		}
	}
	return rows, time.Date(2027, 3, 8, 12, 0, 0, 0, ny)
}
func TestRestLearningIndependentHoldoutAndDST(t *testing.T) {
	rows, now := learningRows(t)
	weeks := marketrest.CompleteWeekends(rows, now)
	if len(weeks) != 4 {
		t.Fatalf("complete weekends %d", len(weeks))
	}
	model, ok := marketrest.Train(weeks)
	if !ok || !marketrest.Validate(model, weeks[3], rows) {
		t.Fatal("four-week validation", model, weeks)
	}
	if model.Close < 17*60 || model.Open > 2*1440+17*60 || model.Through >= weeks[3].End {
		t.Fatal("unbounded or holdout leakage", model)
	}
	// Holdout changes reject the prediction without changing training parameters.
	bad := append([]marketrest.Observation(nil), rows...)
	for i := range bad {
		from, to := marketrest.LearnedWindow(time.UnixMilli(bad[i].To), model.Close, model.Open)
		if bad[i].To > model.Through && bad[i].To > from.UnixMilli() && bad[i].To < to.UnixMilli() {
			bad[i].Changed = true
			break
		}
	}
	if marketrest.Validate(model, weeks[3], bad) {
		t.Fatal("ignored independent counterevidence")
	}
	again, _ := marketrest.Train(weeks)
	if again.Digest != model.Digest || again.Close != model.Close {
		t.Fatal("holdout tuned training")
	}
}
func TestRestLearningRejectsGapsEmptyPositionsAndUnhealthyStreams(t *testing.T) {
	for _, scenario := range []string{"gap", "empty", "failure", "config", "cross_account", "frozen", "uncertain"} {
		t.Run(scenario, func(t *testing.T) {
			rows, now := learningRows(t)
			for i := range rows {
				switch scenario {
				case "gap":
					if i%300 == 100 {
						rows[i].PreviousTo = rows[i].To - 900000
					}
				case "empty":
					rows[i].Comparable = false
				case "failure":
					if i%300 == 100 {
						rows[i].Healthy = false
					}
				case "config":
					if i%300 >= 150 {
						rows[i].Stream = "account:2:1"
					}
				case "cross_account":
					if i%300 >= 150 {
						rows[i].Stream = "another:1:1"
					}
				case "frozen":
					rows[i].Changed = false
				case "uncertain":
					rows[i].MaxGap = 1000
				}
			}
			if got := marketrest.CompleteWeekends(rows, now); len(got) != 0 {
				t.Fatalf("accepted %s: %d", scenario, len(got))
			}
		})
	}
}
func TestRestLearningRejectsBoundaryDriftAndExpires(t *testing.T) {
	rows, now := learningRows(t)
	weeks := marketrest.CompleteWeekends(rows, now)
	weeks[1].Close += 16
	if _, ok := marketrest.Train(weeks); ok {
		t.Fatal("accepted drift")
	}
	weeks[1].Close -= 16
	m, ok := marketrest.Train(weeks)
	if !ok || m.Expires != weeks[0].End+56*24*time.Hour.Milliseconds() {
		t.Fatal("expiry")
	}
	if got := marketrest.CompleteWeekends(rows, now.AddDate(0, 0, 60)); len(got) != 0 {
		t.Fatal("reused stale observations")
	}
}

func TestRestLearningSharedEvidenceIsDeterministicAndSpreadIsGlobal(t *testing.T) {
	base, now := learningRows(t)
	makeStream := func(name string, offset int64, closeMinute int) []marketrest.Observation {
		out := append([]marketrest.Observation(nil), base...)
		for i := range out {
			r := &out[i]
			r.ID += offset
			r.Stream = name
			r.Snapshot = name + r.Snapshot
			if r.Previous != "" {
				r.Previous = name + r.Previous
			}
			close, open := marketrest.LearnedWindow(time.UnixMilli(r.To), closeMinute, 2*1440+17*60)
			at := time.UnixMilli(r.To)
			r.Changed = !at.After(close) || !at.Before(open)
		}
		return out
	}
	rows := append(append([]marketrest.Observation(nil), base...), makeStream("second", 100000, 17*60+15)...)
	digest := ""
	for range 12 {
		weeks := marketrest.CompleteWeekends(rows, now)
		if len(weeks) != 4 {
			t.Fatal("lost healthy shared evidence")
		}
		model, ok := marketrest.Train(weeks)
		if !ok {
			t.Fatal("bounded shared windows rejected")
		}
		if digest != "" && digest != model.Digest {
			t.Fatal("same evidence created different model versions")
		}
		digest = model.Digest
	}
	rows = append(rows, makeStream("third", 200000, 17*60+30)...)
	if got := marketrest.CompleteWeekends(rows, now); len(got) != 0 {
		t.Fatal("accepted total boundary spread over 15 minutes")
	}
	weeks := marketrest.CompleteWeekends(base, now)
	model, _ := marketrest.Train(weeks)
	// A real comparable price difference remains counterevidence when the
	// observation has a collection gap; it cannot establish quiet coverage.
	extra := base[len(base)-100]
	from, to := marketrest.LearnedWindow(time.UnixMilli(extra.To), model.Close, model.Open)
	extra.To = from.Add(time.Hour).UnixMilli()
	extra.PreviousTo = extra.To - time.Minute.Milliseconds()
	extra.From = extra.To - 1000
	extra.Healthy = false
	extra.Comparable = true
	extra.Changed = true
	if extra.To >= to.UnixMilli() {
		t.Fatal("bad synthetic interval")
	}
	if marketrest.Validate(model, weeks[3], append(base, extra)) {
		t.Fatal("ignored reliable quote conflict after gap")
	}
}
