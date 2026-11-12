package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func analyticsSnapshot(t *testing.T, f testutil.Fixture, a storage.Account, at time.Time, balance, floating float64) {
	t.Helper()
	if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: *a.Currency, Balance: balance, Equity: balance + floating}}
	if floating != 0 {
		result.Trades = []mt4.Trade{{Ticket: 100000 + int32(a.Login), Command: 0, Symbol: "EURUSD", VolumeHundredths: 10, Profit: floating}}
	}
	sample, positions, err := syncer.Convert(job, result, at.UnixMilli()-250, at.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.Publish(t.Context(), job, sample, positions, at.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func analyticsHistory(t *testing.T, f testutil.Fixture, a storage.Account, now time.Time, trades []mt4.Trade) {
	t.Helper()
	if err := f.Accounts.RequestHistory(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1; i++ {
		job, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		publishHistory(t, f, job, trades, now)
	}
}

func analyticsFixture(t *testing.T) (testutil.Fixture, storage.Account, *time.Time, time.Time) {
	t.Helper()
	start := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	now := start.Add(-24 * time.Hour)
	f := testutil.New(t, true)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Analytics")
	if _, err := f.Accounts.ConfirmServerTime(t.Context(), server.ID, accounts.ServerTimeInput{TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-calendar"}); err != nil {
		t.Fatal(err)
	}
	a := f.Account(t, server, endpoint, 9801, "USD")
	return f, a, &now, start
}

func TestAnalyticsSourceCoverageCutoffRevisionsAndEvidence(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	for i := 0; i <= 3; i++ {
		*now = start.Add(time.Duration(i) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 1000, float64(i)*10)
	}
	*now = start.Add(4 * time.Minute)
	analyticsHistory(t, f, a, *now, nil)
	input := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: now.UnixMilli()}
	view, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "30")
	assertMetric(t, view.Result.Metrics.ReturnPct, "3")
	if view.Result.ToMS != start.Add(3*time.Minute).UnixMilli() || !view.Result.Quality.HistoryComplete {
		t.Fatal("did not use common cutoff", view.Result)
	}
	again, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil || again.Result.EvidenceID != view.Result.EvidenceID {
		t.Fatal("identical evidence changed", err)
	}
	portfolio, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{Unit: "USD", FromMS: input.FromMS, ToMS: input.ToMS})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, portfolio.Result.Metrics.NetProfit, "30")
	// A source debit which is absent from the balance is never accepted as profit.
	trade := mt4.Trade{Ticket: 88001, Command: 6, OpenTime: uint32(start.Add(30 * time.Second).Unix()), Profit: 5, Comment: "Unknown"}
	*now = now.Add(time.Minute)
	analyticsHistory(t, f, a, *now, []mt4.Trade{trade})
	view, err = f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Result.Metrics.NetProfit.Value != nil || view.Result.Quality.Unclassified != 1 {
		t.Fatal("unknown funding accepted", view)
	}
	assertMetric(t, view.Result.Metrics.Realized, "0")
	classify(t, f, a.ID, 88001, 1, 0, "rebate")
	view, err = f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Result.Metrics.NetProfit.Value != nil || view.Result.Quality.Reconciliation != "failed" {
		t.Fatal("classification bypassed reconciliation", view.Result)
	}
	if view.Result.EvidenceID == again.Result.EvidenceID || view.SourceRevision == again.SourceRevision {
		t.Fatal("revision evidence unchanged")
	}
}

func TestAnalyticsUSCConversionAndFundingClassification(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	currency := "USC"
	if err := f.Accounts.UpdateAccount(t.Context(), a.ID, accounts.AccountPatch{ConfirmedCurrency: &currency}); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&a, "id=?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	*now = start
	analyticsSnapshot(t, f, a, *now, 100000, 0)
	*now = start.Add(time.Minute)
	analyticsSnapshot(t, f, a, *now, 150000, 3000)
	*now = start.Add(2 * time.Minute)
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 88002, Command: 6, OpenTime: uint32(start.Add(30 * time.Second).Unix()), Profit: 50000, Comment: "Deposit"}})
	input := accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(time.Minute).UnixMilli()}
	view, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "30")
	assertMetric(t, view.Result.Metrics.ReturnPct, "2.4")
	input.Unit = "native"
	native, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, native.Result.Metrics.NetProfit, "3000")
	assertMetric(t, native.Result.Metrics.ReturnPct, "2.4")
	if native.Result.Currency != "USC" {
		t.Fatal(native.Result.Currency)
	}
}

func TestAnalyticsTransfersRequireReviewAndCarryKnownTransit(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	var server storage.Server
	var endpoint storage.Endpoint
	if err := f.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Read.First(&endpoint, "id=?", a.PinnedEndpointID).Error; err != nil {
		t.Fatal(err)
	}
	b := f.Account(t, server, endpoint, 9802, "USC")
	for i := 0; i <= 3; i++ {
		*now = start.Add(time.Duration(i) * time.Minute)
		balanceA, balanceB := 1000.0, 100000.0
		if i >= 1 {
			balanceA = 900
		}
		if i >= 3 {
			balanceB = 110000
		}
		analyticsSnapshot(t, f, a, *now, balanceA, 0)
		analyticsSnapshot(t, f, b, *now, balanceB, 0)
	}
	*now = start.Add(4 * time.Minute)
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 88003, Command: 6, OpenTime: uint32(start.Add(30 * time.Second).Unix()), Profit: -100, Comment: "Unknown"}})
	analyticsHistory(t, f, b, *now, []mt4.Trade{{Ticket: 88004, Command: 6, OpenTime: uint32(start.Add(150 * time.Second).Unix()), Profit: 10000, Comment: "Unknown"}})
	classify(t, f, a.ID, 88003, 1, 0, "transfer_out")
	classify(t, f, b.ID, 88004, 1, 0, "transfer_in")
	input := accounts.AnalyticsRequest{Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(3 * time.Minute).UnixMilli()}
	before, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if before.Result.Metrics.NetProfit.Value != nil || len(before.Transfers) != 2 {
		t.Fatal("unreviewed transfers silently netted", before.Result)
	}
	assertMetric(t, before.Result.Metrics.Realized, "0")
	match, err := f.Accounts.MatchAnalyticsTransfer(t.Context(), accounts.TransferInput{OutAccountID: a.ID, OutTicket: 88003, OutRevision: 1, InAccountID: b.ID, InTicket: 88004, InRevision: 1, Reason: "合成跨账户到账核对"})
	if err != nil {
		t.Fatal(err)
	}
	if !match.AmountUSD.Equal(decimal.NewFromInt(100)) {
		t.Fatal(match)
	}
	after, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, after.Result.Metrics.NetProfit, "0")
	assertMetric(t, after.Result.Metrics.ReturnPct, "0")
	assertMetric(t, after.Result.Metrics.MaxDrawdownPct, "0")
	if len(after.Transfers) != 0 {
		t.Fatal(after.Transfers)
	}
	for _, point := range after.Result.Points {
		if point.StatisticalEquity != nil && !point.StatisticalEquity.Equal(decimal.NewFromInt(2000)) {
			t.Fatal("in-transit money disappeared", point)
		}
	}
	// Moving the beginning into the transfer's transit interval preserves capital.
	input.FromMS = start.Add(time.Minute).UnixMilli()
	inTransit, err := f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, inTransit.Result.Metrics.NetProfit, "0")
	assertMetric(t, inTransit.Result.Metrics.ReturnPct, "0")
	classify(t, f, a.ID, 88003, 1, 1, "fee")
	after, err = f.Accounts.Analytics(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if after.Result.Metrics.NetProfit.Value != nil {
		t.Fatal("obsolete transfer evidence retained")
	}
}

func TestAnalyticsEmptyPortfolioAndInteriorGapsDoNotBecomeZero(t *testing.T) {
	start := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return start.Add(time.Hour) }
	empty, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{Unit: "USD", FromMS: start.UnixMilli(), ToMS: start.Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Result.Metrics.Realized.Value != nil || empty.Result.Metrics.NetProfit.Value != nil {
		t.Fatal("empty membership became known zero", empty.Result)
	}
	f, a, now, start := analyticsFixture(t)
	for _, minute := range []int{0, 1, 5} {
		*now = start.Add(time.Duration(minute) * time.Minute)
		analyticsSnapshot(t, f, a, *now, 1000, float64(minute)*10)
	}
	*now = start.Add(7 * time.Minute)
	analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: 88009, Command: 6, OpenTime: uint32(start.Add(6 * time.Minute).Unix()), Profit: 50, Comment: "Unknown"}})
	view, err := f.Accounts.Analytics(t.Context(), accounts.AnalyticsRequest{AccountID: a.ID, Unit: "USD", FromMS: start.UnixMilli(), ToMS: now.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	assertMetric(t, view.Result.Metrics.NetProfit, "50")
	if view.Result.Metrics.MaxDrawdownPct.Value != nil || view.Result.Quality.Unclassified != 0 || view.Result.Metrics.ReturnPct.Value != nil {
		t.Fatal("gap or post-cutoff record changed valid endpoint profit", view.Result)
	}
}

func TestAnalyticsHTTPAuthorizationValidationActivityAndReadFencing(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	*now = start
	analyticsSnapshot(t, f, a, *now, 1000, 0)
	*now = start.Add(time.Minute)
	analyticsSnapshot(t, f, a, *now, 1000, 10)
	*now = start.Add(2 * time.Minute)
	analyticsHistory(t, f, a, *now, nil)
	s := apiServer(t, f)
	base := fmt.Sprintf("/api/analytics?from_ms=%d&to_ms=%d&account_id=%s", start.UnixMilli(), start.Add(time.Minute).UnixMilli(), a.ID)
	if w := request(s, "GET", base, "", nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cookie, csrf := loginHTTP(t, s)
	if w := request(s, "GET", base, "", cookie, "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	for _, suffix := range []string{"&unit=EUR", "&from_ms=1", "&unknown=1", "&to_ms=oops"} {
		if w := request(s, "GET", base+suffix, "", cookie, "", ""); w.Code != 400 {
			t.Fatal(suffix, w.Code, w.Body)
		}
	}
	if w := request(s, "POST", "/api/analytics/transfers", `{}`, cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("transfer mutation lacks CSRF", w.Code)
	}
	if w := request(s, "POST", "/api/analytics/transfers", `{}`, cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	activity := fmt.Sprintf("/api/analytics/activity?from_ms=%d&to_ms=%d&account_id=%s", start.UnixMilli(), start.Add(time.Minute).UnixMilli(), a.ID)
	if w := request(s, "GET", activity, "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	// Mutate source evidence between bounded read pages. A mixed report is rejected.
	changed := false
	callback := "synthetic_analytics_revision"
	if err := f.Store.Read.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if !changed && tx.Statement.Table == "accounts" {
			changed = true
			_ = f.Store.Write(t.Context(), storage.High, func(write *gorm.DB) error {
				return write.Exec("UPDATE analytics_revision SET revision=revision+1 WHERE id=1").Error
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.Store.Read.Callback().Query().Remove(callback)
	if w := request(s, "GET", base, "", cookie, "", ""); w.Code != 409 {
		t.Fatal("mixed revision report accepted", w.Code, w.Body)
	}
}
