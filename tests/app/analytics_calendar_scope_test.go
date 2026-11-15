package app_test

import (
	"crypto/sha256"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/storage"
	"slices"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/analytics"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestAnalyticsCalendarScopesUnresolvedTimesToEachDay(t *testing.T) {
	start := time.Date(2026, 11, 11, 16, 0, 0, 0, time.UTC) // Nov 12 in Beijing.
	now := start
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Daily-Scope")
	if _, err := f.Accounts.ConfirmServerTime(t.Context(), server.ID, accounts.ServerTimeInput{TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-daily-calendar"}); err != nil {
		t.Fatal(err)
	}
	a := f.Account(t, server, endpoint, 9861, "USD")
	for i, balance := range []float64{1000, 1000, 1005} {
		now = start.Add(time.Duration(i) * 24 * time.Hour)
		analyticsSnapshot(t, f, a, now, balance, 0)
	}
	now = now.Add(time.Minute)
	trades := []mt4.Trade{
		{Ticket: 78001, Command: 0, OpenTime: uint32(start.Add(-2 * time.Hour).Unix()), CloseTime: uint32(start.Add(-time.Hour).Unix()), Profit: 7},
		{Ticket: 78002, Command: 0, OpenTime: uint32(start.Add(25 * time.Hour).Unix()), CloseTime: uint32(start.Add(26 * time.Hour).Unix()), Profit: 5},
	}
	if err := f.Store.Read.First(&server, "id=?", server.ID).Error; err != nil {
		t.Fatal(err)
	}
	coverage := storage.HistoryCoverage{ID: storage.NewID(), AccountID: a.ID, UnitRevision: a.UnitRevision, TimeRulesetID: *server.ActiveTimeRulesetID, StartWall: uint32(start.Add(-48 * time.Hour).Unix()), EndWall: uint32(now.Add(24 * time.Hour).Unix()), State: "complete", CheckedAtMS: new(now.UnixMilli())}
	run := storage.HistoryRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID, CoverageID: coverage.ID, ConfigVersion: a.ConfigVersion, UnitRevision: a.UnitRevision, NativeCurrency: "USD", State: "succeeded", StartedAtMS: now.UnixMilli(), FinishedAtMS: new(now.UnixMilli())}
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		if err := tx.Create(&coverage).Error; err != nil {
			return err
		}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		for _, trade := range trades {
			hash := sha256.Sum256([]byte("synthetic-calendar-trade"))
			order := storage.HistoryOrder{AccountID: a.ID, Ticket: trade.Ticket, Revision: 1, SourceRunID: run.ID, SourceHash: hash[:], UnitRevision: a.UnitRevision, NativeCurrency: "USD", Command: 0, OpenTimeWall: trade.OpenTime, CloseTimeWall: trade.CloseTime, EventTimeWall: trade.CloseTime, Profit: decimal.NewFromFloat(trade.Profit), NetProfit: decimal.NewFromFloat(trade.Profit)}
			if err := tx.Create(&order).Error; err != nil {
				return err
			}
			if err := tx.Create(&storage.HistoryOrderHead{AccountID: a.ID, Ticket: trade.Ticket, Revision: 1}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{a.ID, ""} {
		req := accounts.AnalyticsRequest{AccountID: account, Unit: "USD", FromMS: start.Add(-24 * time.Hour).UnixMilli(), ToMS: start.Add(48 * time.Hour).UnixMilli()}
		wide, err := f.Accounts.Analytics(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if wide.Result.Quality.UnresolvedTimes == 0 {
			t.Fatal("fixture did not exercise unresolved history")
		}
		req.FromMS = start.Add(24 * time.Hour).UnixMilli()
		narrow, err := f.Accounts.Analytics(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		assertMetric(t, narrow.Result.Metrics.Realized, "5")
		found := false
		for _, day := range wide.Result.Days {
			if day.Date == "2026-11-13" {
				found = true
				assertMetric(t, day.Realized, "5")
				assertMetric(t, day.NetProfit, "5")
				if day.Trades == nil || *day.Trades != 1 {
					t.Fatal("wrong daily trade count", day.Trades)
				}
			}
			if account != "" && day.Date == "2026-11-11" && day.Realized.Value != nil {
				t.Fatal("unresolved day was treated as verified", day)
			}
		}
		if !found {
			t.Fatal("expected calendar date missing")
		}
	}
	// A missing historical inclusion valuation is an issue on the inclusion
	// day, not a reason to hide independently valued later days.
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&a).Update("included_at_ms", start.Add(-time.Minute).UnixMilli()).Error
	}); err != nil {
		t.Fatal(err)
	}
	wide, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{Unit: "USD", FromMS: start.Add(-24 * time.Hour).UnixMilli(), ToMS: start.Add(48 * time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(wide.Result.Metrics.NetProfit.Reasons, "membership_unresolved") {
		t.Fatal("fixture lost inclusion failure")
	}
	for _, day := range wide.Result.Days {
		if day.Date == "2026-11-13" {
			assertMetric(t, day.NetProfit, "5")
		}
	}
}

func TestAnalyticsCalendarRetainsBoundaryEventsAndCountsOnlyDailyTrades(t *testing.T) {
	input := performanceInput("1000", "1000", "1000")
	from := time.Date(2026, 11, 12, 15, 59, 0, 0, time.UTC).UnixMilli()
	shift := from - input.FromMS
	input.FromMS += shift
	input.ToMS += shift
	for i := range input.Samples {
		input.Samples[i].TimeMS += shift
		input.Samples[i].ObservedFromMS += shift
		input.Samples[i].ObservedToMS += shift
	}
	midnight := from + analytics.Minute
	input.Events = []analytics.Event{{TimeMS: midnight - 250, Kind: "trade"}, {TimeMS: input.ToMS, Kind: "trade"}}
	result := calculatePerformance(t, input)
	day := result.Days[1]
	assertMetric(t, day.Realized, "0")
	if day.Trades == nil || *day.Trades != 0 {
		t.Fatal("events outside day were counted", day.Trades)
	}
	if day.NetProfit.Value != nil || !slices.Contains(day.NetProfit.Reasons, "boundary_event") || !slices.Contains(day.NetProfit.Reasons, "missing_start") || !slices.Contains(day.NetProfit.Reasons, "missing_end") {
		t.Fatal("daily slice discarded boundary evidence", day)
	}
}

func TestAnalyticsCalendarKeepsDatedReasonsOnAffectedDays(t *testing.T) {
	for _, capital := range []bool{false, true} {
		input := performanceInput("1000", "1000", "1000")
		from := time.Date(2026, 11, 12, 15, 59, 0, 0, time.UTC).UnixMilli()
		shift := from - input.FromMS
		input.FromMS += shift
		input.ToMS += shift
		for i := range input.Samples {
			input.Samples[i].TimeMS += shift
			input.Samples[i].ObservedFromMS += shift
			input.Samples[i].ObservedToMS += shift
		}
		input.ScopedReasons = []analytics.ScopedReason{{FromMS: from + 1000, ToMS: from + 1001, Reason: "synthetic_dated_issue", Capital: capital}}
		result := calculatePerformance(t, input)
		if result.Metrics.NetProfit.Value != nil || result.Days[0].NetProfit.Value != nil {
			t.Fatal("affected interval was incorrectly accepted", capital, result)
		}
		assertMetric(t, result.Days[1].NetProfit, "0")
		assertMetric(t, result.Days[1].Realized, "0")
		if !capital && result.Days[0].Realized.Value != nil {
			t.Fatal("affected trade ledger accepted")
		}
	}
}
