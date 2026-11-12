package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func trendSample(t testing.TB, f testutil.Fixture, account storage.Account, stamp time.Time, balance float64) storage.Snapshot {
	t.Helper()
	if err := f.Accounts.RequestSync(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	currency := "USD"
	if job.Account.Currency != nil {
		currency = *job.Account.Currency
	}
	result := testutil.Result(currency)
	result.Account.Balance, result.Account.Equity, result.Account.Credit = balance, balance+5.25, 2
	snapshot, positions, err := syncer.Convert(job, result, stamp.UnixMilli()-500, stamp.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.PublishScheduled(t.Context(), job, snapshot, positions); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func trendRange(from, to time.Time, unit string) accounts.TrendRequest {
	return accounts.TrendRequest{FromMS: from.UnixMilli(), ToMS: to.UnixMilli(), Unit: unit}
}

func TestAssetTrendNativeUnitsRangeContinuityAndLegacySamples(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	start := now
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Trend-Account")
	a := f.Account(t, server, endpoint, 9101, "USC")
	first := trendSample(t, f, a, now, 100000.01)
	now = start.Add(time.Minute)
	trendSample(t, f, a, now, 100100.02)
	now = start.Add(6 * time.Minute)
	trendSample(t, f, a, now, 100200.03)
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	now = start.Add(11 * time.Minute)
	trendSample(t, f, a, now, 100300.04)
	input := trendRange(start, now.Add(time.Second), "native")
	view, err := f.Accounts.AccountTrend(t.Context(), a.ID, input)
	if err != nil || len(view.Points) != 4 || view.ValidPoints != 4 || view.Points[0].Currency != "USC" ||
		view.Points[0].Balance.String() != "100000.01" || view.Points[0].Credit.String() != "2" {
		t.Fatal("native snapshot data changed", view, err)
	}
	if view.Points[0].Segment != view.Points[1].Segment || view.Points[1].Segment == view.Points[2].Segment ||
		view.Points[2].Segment != view.Points[3].Segment {
		t.Fatal("gap or 300-second cadence misrepresented", view.Points)
	}
	input.Unit = "USD"
	view, err = f.Accounts.AccountTrend(t.Context(), a.ID, input)
	if err != nil || view.Points[0].Balance.String() != "1000.0001" || view.Points[0].Credit.String() != "0.02" {
		t.Fatal("USC conversion lost precision", view, err)
	}
	input = trendRange(start, start.Add(time.Minute), "native")
	view, err = f.Accounts.AccountTrend(t.Context(), a.ID, input)
	if err != nil || len(view.Points) != 1 || view.Points[0].SnapshotID != first.ID {
		t.Fatal("range is not half-open", view, err)
	}
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&storage.Snapshot{}).Where("id=?", first.ID).Update("collection_interval_seconds", nil).Error
	}); err != nil {
		t.Fatal(err)
	}
	input.ToMS = now.Add(time.Second).UnixMilli()
	view, err = f.Accounts.AccountTrend(t.Context(), a.ID, input)
	if err != nil || view.UnknownCadence != 1 || view.Points[0].Segment == view.Points[1].Segment {
		t.Fatal("legacy cadence was invented", view, err)
	}
	currency := "USD"
	now = start.Add(12 * time.Minute)
	if err := f.Accounts.UpdateAccount(t.Context(), a.ID, accounts.AccountPatch{ConfirmedCurrency: &currency}); err != nil {
		t.Fatal(err)
	}
	trendSample(t, f, a, now, 1234.56)
	input.ToMS = now.Add(time.Second).UnixMilli()
	view, err = f.Accounts.AccountTrend(t.Context(), a.ID, input)
	if err != nil || len(view.Currencies) != 2 || view.Points[3].Segment == view.Points[4].Segment ||
		view.Points[4].Currency != "USD" || view.Points[4].Balance.String() != "1234.56" {
		t.Fatal("unit revisions joined together", view, err)
	}
}

func TestAssetTrendPortfolioMembershipMissingDataAndPause(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	start := now
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Trend-Portfolio")
	usd := f.Account(t, server, endpoint, 9201, "USD")
	usc := f.Account(t, server, endpoint, 9202, "USC")
	f.Account(t, server, endpoint, 9203, "EUR")
	f.Account(t, server, endpoint, 9204, "USD") // No first snapshot or effective inclusion.
	now = start.Add(5 * time.Second)
	trendSample(t, f, usd, now, 1000)
	now = start.Add(20 * time.Second)
	trendSample(t, f, usc, now, 100000)
	now = start.Add(65 * time.Second)
	trendSample(t, f, usd, now, 1001)
	now = start.Add(80 * time.Second)
	trendSample(t, f, usc, now, 100100)
	now = start.Add(125 * time.Second)
	newcomer := f.Account(t, server, endpoint, 9205, "USD")
	trendSample(t, f, usd, now, 1002)
	now = start.Add(135 * time.Second)
	trendSample(t, f, usc, now, 100200)
	now = start.Add(140 * time.Second)
	trendSample(t, f, newcomer, now, 300)
	paused := false
	if err := f.Accounts.UpdateAccount(t.Context(), usd.ID, accounts.AccountPatch{SyncEnabled: &paused}); err != nil {
		t.Fatal(err)
	}
	now = start.Add(5 * time.Minute)
	view, err := f.Accounts.PortfolioTrend(t.Context(), trendRange(start, now, "USD"))
	if err != nil || view.PendingAccounts != 1 || view.ExcludedAccounts != 1 {
		t.Fatal("scope unavailable", view, err)
	}
	if view.IncludedFromMS != start.Add(5*time.Second).UnixMilli() {
		t.Fatal("collection start did not use actual membership evidence", view.IncludedFromMS)
	}
	byTime := map[int64]accounts.TrendPoint{}
	for _, point := range view.Points {
		byTime[point.TimeMS] = point
	}
	first, second, third, missing := byTime[start.Add(time.Minute).UnixMilli()], byTime[start.Add(2*time.Minute).UnixMilli()],
		byTime[start.Add(3*time.Minute).UnixMilli()], byTime[start.Add(4*time.Minute).UnixMilli()]
	if first.Balance == nil || first.Balance.String() != "2000" || first.ExpectedAccounts != 2 || second.ExpectedAccounts != 2 {
		t.Fatal("future membership rewrote the past", first, second)
	}
	if third.Balance == nil || third.Balance.String() != "2304" || third.ExpectedAccounts != 3 || third.Segment == second.Segment {
		t.Fatal("inclusion joined as trading performance or pause removed a member", third, second)
	}
	if missing.Equity != nil || missing.ExpectedAccounts != 3 || missing.Reason != "missing_samples" {
		t.Fatal("missing member became zero or stale value", missing)
	}
}

func TestAssetTrendPortfolioRejectsSkewAndChangedUnit(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	start := now
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Trend-Skew")
	a := f.Account(t, server, endpoint, 9301, "USD")
	b := f.Account(t, server, endpoint, 9302, "USD")
	now = start.Add(time.Minute)
	first := trendSample(t, f, a, now, 1000)
	now = start.Add(90 * time.Second)
	trendSample(t, f, b, now, 1000)
	if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		return tx.Model(&first).Update("observed_from_ms", start.Add(-time.Millisecond).UnixMilli()).Error
	}); err != nil {
		t.Fatal(err)
	}
	now = start.Add(121 * time.Second)
	input := trendRange(start.Add(120*time.Second), now, "USD")
	view, err := f.Accounts.PortfolioTrend(t.Context(), input)
	if err != nil || len(view.Points) != 1 || view.Points[0].Equity != nil || view.Points[0].Reason != "sampling_skew" {
		t.Fatal("combined sample span ignored", view, err)
	}
	now = start.Add(100 * time.Second)
	confirmed := false
	if err := f.Accounts.UpdateAccount(t.Context(), b.ID, accounts.AccountPatch{ConfirmUnit: &confirmed}); err != nil {
		t.Fatal(err)
	}
	now = start.Add(121 * time.Second)
	view, err = f.Accounts.PortfolioTrend(t.Context(), input)
	if err != nil || view.Points[0].Equity != nil || view.Points[0].Reason != "unit_changed" {
		t.Fatal("old confirmation survived unit change", view, err)
	}
}

func TestAssetTrendHTTPBoundaryAndExactDecimalJSON(t *testing.T) {
	f := testutil.New(t, true)
	now := time.Now()
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Trend-HTTP")
	a := f.Account(t, server, endpoint, 9401, "USD")
	trendSample(t, f, a, now, 1234.56)
	handler := apiServer(t, f)
	paths := []string{"/api/accounts/" + a.ID + "/equity-history", "/api/portfolio/equity-history"}
	for _, path := range paths {
		if w := request(handler, "GET", path, "", nil, "", ""); w.Code != 401 {
			t.Fatal("anonymous financial history", w.Code)
		}
	}
	cookie, _ := loginHTTP(t, handler)
	query := fmt.Sprintf("?from_ms=%d&to_ms=%d", now.Add(-time.Hour).UnixMilli(), now.Add(time.Second).UnixMilli())
	for _, suffix := range []string{"", "?from_ms=x&to_ms=1", query + "&sort=balance", query + "&unit=USD&unit=native", query + "&unit=EUR"} {
		if w := request(handler, "GET", paths[0]+suffix, "", cookie, "", ""); w.Code != 400 {
			t.Fatal("invalid query accepted", suffix, w.Code)
		}
	}
	w := request(handler, "GET", paths[0]+query, "", cookie, "", "")
	var view struct {
		Points []struct{ Balance json.RawMessage }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || w.Code != 200 || len(view.Points) != 1 ||
		string(view.Points[0].Balance) != `"1234.56"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("amount or financial cache boundary changed", w.Code, w.Body, err)
	}
	if strings.Contains(w.Body.String(), testutil.InvestorPassword) {
		t.Fatal("credentials in history")
	}
	if w := request(handler, "GET", "/api/accounts/absent/equity-history"+query, "", cookie, "", ""); w.Code != 404 {
		t.Fatal("missing account status", w.Code)
	}
	for _, input := range []accounts.TrendRequest{
		{FromMS: 1, ToMS: 0, Unit: "native"},
		{FromMS: 0, ToMS: accounts.TrendMaxRange.Milliseconds() + 1, Unit: "native"},
		{FromMS: now.UnixMilli(), ToMS: now.Add(time.Hour).UnixMilli(), Unit: "native"},
	} {
		if _, err := f.Accounts.AccountTrend(t.Context(), a.ID, input); err == nil {
			t.Fatal("invalid service range accepted", input)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.Accounts.AccountTrend(ctx, a.ID, trendRange(now.Add(-time.Hour), now, "native")); !errors.Is(err, context.Canceled) {
		t.Fatal("request cancellation ignored", err)
	}
}

// Seed immutable synthetic source rows in bounded transactions. This helper
// also exercises the actual schema, indexes and source-run foreign keys.
func seedTrendSamples(t testing.TB, f testutil.Fixture, a storage.Account, from time.Time, count int) {
	t.Helper()
	cycle := 30
	scale := decimal.NewFromInt(1)
	for start := 0; start < count; start += 100 {
		var runs []storage.SyncRun
		var snapshots []storage.Snapshot
		for i := start; i < min(start+100, count); i++ {
			stamp := from.Add(time.Duration(i) * 30 * time.Second).UnixMilli()
			run := storage.SyncRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID,
				CredentialVersion: a.CredentialVersion, ConfigVersion: a.ConfigVersion, Kind: "snapshot", State: "succeeded", Attempt: 1}
			snapshot := storage.Snapshot{ID: storage.NewID(), AccountID: a.ID, SyncRunID: run.ID, ObservedFromMS: stamp - 500, ObservedToMS: stamp,
				NativeCurrency: "USD", UnitRevision: a.UnitRevision, USDScale: &scale, CollectionIntervalSeconds: &cycle,
				Balance: decimal.NewFromInt(1000 + int64(i%50)), Equity: decimal.NewFromInt(1000 + int64(i%80))}
			if i == 1234 {
				snapshot.Equity = decimal.RequireFromString("999999.123456789")
			}
			if i == 2222 {
				snapshot.Balance = decimal.RequireFromString("-888888.987654321")
			}
			runs, snapshots = append(runs, run), append(snapshots, snapshot)
		}
		if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
			if err := tx.Create(&runs).Error; err != nil {
				return err
			}
			return tx.Create(&snapshots).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAssetTrendDownsamplingPreservesExtremaAndBoundedOutput(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	now := from
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Trend-Sampling")
	a := f.Account(t, server, endpoint, 9501, "USD")
	seedTrendSamples(t, f, a, from, 5000)
	now = from.Add(5000 * 30 * time.Second)
	view, err := f.Accounts.AccountTrend(t.Context(), a.ID, trendRange(from, now, "native"))
	if err != nil || view.TotalPoints != 5000 || !view.Downsampled || len(view.Points) > 2400 {
		t.Fatal("unbounded or incomplete sampling", len(view.Points), view.TotalPoints, err)
	}
	foundEquity, foundBalance := false, false
	for _, point := range view.Points {
		foundEquity = foundEquity || point.Equity.String() == "999999.123456789"
		foundBalance = foundBalance || point.Balance.String() == "-888888.987654321"
	}
	if !foundEquity || !foundBalance || view.Points[0].TimeMS != from.UnixMilli() ||
		view.Points[len(view.Points)-1].TimeMS != now.Add(-30*time.Second).UnixMilli() {
		t.Fatal("sampling discarded an extremum or endpoint")
	}
}
