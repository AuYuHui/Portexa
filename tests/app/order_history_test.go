package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

func TestOrderHistoryAccountPageFiltersAndBeijingBoundary(t *testing.T) {
	f, account, now, start := analyticsFixture(t)
	midnight := start.Add(15 * time.Hour)
	*now = midnight.Add(-time.Minute)
	analyticsSnapshot(t, f, account, *now, 1000, 0)
	wall := uint32(midnight.Unix())
	trades := []mt4.Trade{
		{Ticket: 99, Command: 1, Symbol: "TESTUSD", OpenTime: wall - 3600, CloseTime: wall - 1, Profit: 800},
		{Ticket: 98, Command: 6, OpenTime: wall, Profit: 500, Comment: "Deposit"},
		{Ticket: 97, Command: 1, Symbol: "OTHER", OpenTime: wall - 3600, CloseTime: wall, Profit: -1},
		{Ticket: 96, Command: 0, Symbol: "TESTUSD", OpenTime: wall - 3600, CloseTime: wall, Profit: 2, Commission: -2},
	}
	for i := int32(1); i <= 101; i++ {
		trades = append(trades, mt4.Trade{Ticket: 100 + i, Command: 1, Symbol: "TESTUSD", OpenTime: wall - 3600, CloseTime: wall,
			OpenPrice: 1.25, ClosePrice: 1.28, Digits: 5, VolumeHundredths: 25, Profit: 0.03, Commission: -0.01})
	}
	*now = midnight.Add(2 * time.Minute)
	analyticsHistory(t, f, account, *now, trades)
	server := apiServer(t, f)
	url := "/api/accounts/" + account.ID + "/order-history"
	if got := request(server, "GET", url, "", nil, "", ""); got.Code != 401 {
		t.Fatal(got.Code)
	}
	cookie, _ := loginHTTP(t, server)
	read := func(q string) accounts.OrderHistoryPage {
		t.Helper()
		w := request(server, "GET", url+q, "", cookie, "", "")
		var page accounts.OrderHistoryPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatalf("bad response %d: %s", w.Code, w.Body)
		}
		return page
	}
	q := "?from=2026-11-02&to=2026-11-02&symbol=testusd&side=sell&result=profit"
	for _, number := range []int{1, 2, 3, 4} {
		page := read(fmt.Sprintf("%s&page=%d", q, number))
		actual := min(number, 3)
		if page.Total != 101 || page.PageSize != 50 || page.Page != actual || len(page.Items) != min(50, 101-50*(actual-1)) ||
			!page.Complete || page.NetProfit == nil || page.NetProfit.String() != "2.02" || page.Currency != "USD" {
			t.Fatalf("page %d: total=%d page=%d size=%d items=%d complete=%t net=%v known=%s", number, page.Total, page.Page, page.PageSize, len(page.Items), page.Complete, page.NetProfit, page.KnownNetProfit)
		}
		for j, item := range page.Items {
			if item.DealTicket != "" || item.OrderTicket != fmt.Sprint(201-(actual-1)*50-j) || item.Side != "sell" || item.Lots.String() != "0.25" ||
				item.CloseTimeMS == nil || *item.CloseTimeMS != midnight.UnixMilli() || item.CloseTimeWallMS != midnight.UnixMilli() || item.OpenPrice == nil || item.OpenPrice.String() != "1.25" {
				t.Fatal(item)
			}
		}
	}
	if page := read("?from=2026-11-02&to=2026-11-02&result=loss"); page.Total != 1 || page.Items[0].OrderTicket != "97" || page.NetProfit == nil || page.NetProfit.String() != "-1" {
		t.Fatal(page)
	}
	if page := read("?result=flat&ticket=96&side=buy"); page.Total != 1 || !page.KnownNetProfit.IsZero() {
		t.Fatal(page)
	}
	if page := read("?from=2026-11-02&to=2026-11-02&symbol=no-match"); page.Total != 0 || page.NetProfit == nil || !page.NetProfit.IsZero() {
		t.Fatal(page)
	}
	all := read("")
	// The recent server day has been collected but the older backfill has not.
	if all.Total != 104 || all.Complete || all.NetProfit != nil || all.KnownNetProfit.String() != "801.02" {
		t.Fatal(all.Total, all.Complete, all.NetProfit, all.KnownNetProfit)
	}
	for _, q := range []string{"?page=0", "?result=all", "?side=bad", "?page=2&page=1", "?size=100", "?ticket=-1", "?from=bad", "?from=2026-11-03&to=2026-11-02"} {
		if w := request(server, "GET", url+q, "", cookie, "", ""); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
}

func TestOrderHistoryWithoutCollectedEvidence(t *testing.T) {
	f, a, _, _ := analyticsFixture(t)
	page, err := f.Accounts.OrderHistory(t.Context(), a.ID, accounts.OrderHistoryFilter{Page: 1})
	if err != nil || page.Total != 0 || page.NetProfit != nil || page.Complete || page.Items == nil || page.AsOfMS != nil {
		t.Fatal(page, err)
	}
}

func TestOrderHistoryMissingMiddleDayAndFuture(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	for i, elapsed := range []time.Duration{20 * time.Hour, 60 * time.Hour} {
		at := start.Add(elapsed)
		*now = at
		analyticsSnapshot(t, f, a, *now, 1000, 0)
		*now = at.Add(2 * time.Minute)
		wall := uint32(at.Unix())
		analyticsHistory(t, f, a, *now, []mt4.Trade{{Ticket: int32(101 + i), Command: 1, Symbol: "TESTUSD", OpenTime: wall - 60, CloseTime: wall, Profit: 2}})
	}
	read := func(from, to string) accounts.OrderHistoryPage {
		t.Helper()
		result, err := f.Accounts.OrderHistory(t.Context(), a.ID, accounts.OrderHistoryFilter{Page: 1, From: from, To: to})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	gap := read("2026-11-02", "2026-11-03")
	if gap.Complete || gap.NetProfit != nil || gap.Total != 2 || gap.KnownNetProfit.String() != "4" {
		t.Fatal(gap)
	}
	future := read("2026-11-06", "2026-11-07")
	if future.Complete || future.NetProfit != nil || future.Total != 0 {
		t.Fatal(future)
	}
}

func TestOrderHistoryAmbiguousServerClockKeepsRawRows(t *testing.T) {
	f, a, now, start := analyticsFixture(t)
	wallBase := start.Add(11 * time.Hour) // At 12:00 UTC the server moves from UTC to UTC-2.
	*now = wallBase.Add(4 * time.Hour)
	analyticsSnapshot(t, f, a, *now, 1000, 0)
	*now = now.Add(time.Minute)
	trades := []mt4.Trade{}
	for i, d := range []time.Duration{-150 * time.Minute, -90 * time.Minute, time.Hour} {
		wall := uint32(wallBase.Add(d).Unix())
		trades = append(trades, mt4.Trade{Ticket: int32(100 + i), Command: 0, Symbol: "TESTUSD", OpenTime: wall - 60, CloseTime: wall, Profit: 5})
	}
	analyticsHistory(t, f, a, *now, trades)
	var server storage.Server
	if err := f.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		t.Fatal(err)
	}
	*now = wallBase
	if _, err := f.Accounts.ConfirmServerTime(t.Context(), a.ServerID, accounts.ServerTimeInput{PreviousRulesetID: *server.ActiveTimeRulesetID, TimeZone: "Etc/GMT+2", Confirm: true, EvidenceRef: "synthetic-clock-change"}); err != nil {
		t.Fatal(err)
	}
	*now = wallBase.Add(4*time.Hour + time.Minute)
	all, err := f.Accounts.OrderHistory(t.Context(), a.ID, accounts.OrderHistoryFilter{Page: 1})
	if err != nil || len(all.Items) != 3 {
		t.Fatal(all, err)
	}
	if all.Items[0].OrderTicket != "102" || all.Items[0].CloseTimeMS == nil || *all.Items[0].CloseTimeMS != wallBase.Add(3*time.Hour).UnixMilli() ||
		all.Items[1].OrderTicket != "100" || all.Items[2].OrderTicket != "101" || all.Items[2].CloseTimeMS != nil {
		t.Fatal(all.Items)
	}
	day, err := f.Accounts.OrderHistory(t.Context(), a.ID, accounts.OrderHistoryFilter{Page: 1, From: "2026-11-01", To: "2026-11-01"})
	if err != nil || day.Total != 2 || day.Complete || day.NetProfit != nil || day.KnownNetProfit.String() != "10" {
		t.Fatal(day, err)
	}
}
