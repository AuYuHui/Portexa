package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"portexa/internal/accounts"
	"portexa/mt4"
	"portexa/tests/testutil"
)

type restEvidenceReads struct {
	logger.Interface
	queries int
	rows    int64
}

func (log *restEvidenceReads) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, rows := query()
	if strings.Contains(sql, "o.command IN (0,1,6,7)") {
		log.queries++
		log.rows += rows
	}
}

func TestAssetTrendRestEvidenceSkipsUnrelatedWeekdayTrades(t *testing.T) {
	f, a, now, start := restFixture(t)
	var trades []mt4.Trade
	for i := 0; i < 12; i++ {
		wall := uint32(start.Add(-48*time.Hour + time.Duration(i)*time.Minute).Unix())
		trades = append(trades, mt4.Trade{Ticket: int32(91000 + i), Command: 0, Symbol: "XAUUSD.c", OpenTime: wall - 60, CloseTime: wall})
	}
	// The chart spans two weekends. Each proof must load its own evidence,
	// without decoding unrelated weekday orders or reusing the prior window.
	for _, week := range []time.Duration{0, 7 * 24 * time.Hour} {
		for _, offset := range []time.Duration{0, time.Minute, 6 * time.Minute} {
			*now = start.Add(week + offset)
			restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
		}
		*now = now.Add(time.Second)
		analyticsHistory(t, f, a, *now, trades)
		*now = now.Add(time.Second)
		backfill, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
		if err != nil {
			t.Fatal(err)
		}
		publishHistory(t, f, backfill, trades, *now)
	}
	var storedOrders int64
	if err := f.Store.Read.Table("history_order_heads").Where("account_id=?", a.ID).Count(&storedOrders).Error; err != nil || storedOrders != int64(len(trades)) {
		t.Fatal("fixture did not publish the unrelated historical trades", storedOrders, err)
	}
	reads := &restEvidenceReads{Interface: logger.Default.LogMode(logger.Silent)}
	f.Store.Read = f.Store.Read.Session(&gorm.Session{Logger: reads})
	view, err := f.Accounts.PortfolioTrend(t.Context(), trendRange(start.Add(-7*24*time.Hour), now.Add(time.Minute), "USD"))
	if err != nil || view.RestCarriedPoints != 6 {
		t.Fatal("separate weekend proofs changed", view.RestCarriedPoints, err)
	}
	if reads.rows != 0 {
		t.Fatal("rest proof decoded unrelated weekday trades", reads.rows)
	}
	if reads.queries < 2 {
		t.Fatal("test did not exercise separate evidence windows", reads.queries)
	}
}

func TestAssetTrendResolvedCalendarKeepsPerEventDST(t *testing.T) {
	for _, mode := range []string{"iana", "broker"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
			if mode == "iana" {
				if _, err := f.Accounts.ConfirmServerTime(t.Context(), server.ID, accounts.ServerTimeInput{
					PreviousRulesetID: *server.ActiveTimeRulesetID, Mode: "manual", TimeZone: "America/New_York",
				}); err != nil {
					t.Fatal(err)
				}
			}
			a := f.Account(t, server, endpoint, 9891, "USD")
			now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			trendSample(t, f, a, now, 1000)
			analyticsHistory(t, f, a, now, nil)
			now = now.Add(time.Second)
			before := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
			after := before.Add(5 * time.Hour)
			offsets := []time.Duration{3 * time.Hour, 2 * time.Hour}
			if mode == "iana" {
				offsets = []time.Duration{-4 * time.Hour, -5 * time.Hour}
			}
			analyticsHistory(t, f, a, now, []mt4.Trade{
				{Ticket: 92001, Command: 6, OpenTime: uint32(before.Add(offsets[0]).Unix()), Profit: 1, Comment: "Deposit"},
				{Ticket: 92002, Command: 6, OpenTime: uint32(after.Add(offsets[1]).Unix()), Profit: 2, Comment: "Deposit"},
			})
			view, err := f.Accounts.AccountTrend(t.Context(), a.ID, trendRange(before.Add(-4*time.Hour), now, "native"))
			if err != nil || len(view.Events) != 2 || view.Events[0].TimeMS != before.UnixMilli() || view.Events[1].TimeMS != after.UnixMilli() {
				t.Fatal("one query froze the offset across the DST transition", view.Events, err)
			}
		})
	}
}
