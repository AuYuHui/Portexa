package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestLivePositionsPublishedSnapshotAndFilters(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-Live")
	account := f.Account(t, server, endpoint, 641001, "USD")
	now := time.Now()
	f.Accounts.Now = func() time.Time { return now }
	s := apiServer(t, f)
	url := "/api/accounts/" + account.ID + "/positions"
	if res := request(s, "GET", url, "", nil, "", ""); res.Code != 401 {
		t.Fatalf("unauthenticated positions status: %d", res.Code)
	}
	cookie, _ := loginHTTP(t, s)
	get := func(query string) accounts.LivePositions {
		t.Helper()
		res := request(s, "GET", url+query, "", cookie, "", "")
		var view accounts.LivePositions
		if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &view) != nil {
			t.Fatalf("positions response: %d %s", res.Code, res.Body)
		}
		return view
	}
	unavailable := get("")
	if unavailable.ObservedToMS != nil || unavailable.TotalCount != nil || unavailable.FloatingNetProfit != nil || unavailable.FilteredNetProfit != nil {
		t.Fatal("missing snapshot incorrectly reported as an empty account")
	}
	job, err := f.Accounts.Claim(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := testutil.Result("USD")
	result.Trades = []mt4.Trade{
		{Ticket: 18, Symbol: "A", Command: 1, VolumeHundredths: 10, Profit: 12},
		{Ticket: 19, Symbol: "A", Command: 1, VolumeHundredths: 32, Profit: 1, Commission: -2.5},
		{Ticket: 20, Symbol: "A", Command: 0, VolumeHundredths: 11, Profit: -1.5000000001},
		{Ticket: 21, Symbol: "B", Command: 0, VolumeHundredths: 11},
		{Ticket: 22, Symbol: "PENDING", Command: 2, VolumeHundredths: 11},
	}
	snapshot, rows, err := syncer.Convert(job, result, now.UnixMilli()-15, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.Publish(t.Context(), job, snapshot, rows, now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	all := get("")
	if *all.TotalCount != 4 || len(all.Items) != 4 || len(all.Symbols) != 2 || all.Currency != "USD" ||
		*all.ObservedToMS != now.UnixMilli() || all.Stale || all.FloatingNetProfit.String() != "8.9999999999" ||
		!all.FloatingNetProfit.Equal(*all.FilteredNetProfit) || all.Items[0].Ticket != 20 || all.Items[1].Ticket != 19 {
		t.Fatalf("unexpected all positions: %+v", all)
	}
	loss := get("?loss_only=true")
	if len(loss.Items) != 2 || loss.FilteredNetProfit.String() != "-3.0000000001" {
		t.Fatalf("incorrect loss subtotal: %+v", loss)
	}
	sell := get("?symbol=A&side=sell&loss_only=true")
	if len(sell.Items) != 1 || sell.Items[0].Lots.String() != "0.32" || sell.FilteredNetProfit.String() != "-1.5" ||
		!sell.FloatingNetProfit.Equal(*all.FloatingNetProfit) || *sell.TotalCount != 4 {
		t.Fatalf("combined filter changed account totals: %+v", sell)
	}
	empty := get("?symbol=B&loss_only=true")
	if len(empty.Items) != 0 || empty.FilteredNetProfit == nil || !empty.FilteredNetProfit.IsZero() {
		t.Fatalf("expected known empty selection: %+v", empty)
	}
	for _, invalid := range []string{"?side=invalid", "?loss_only=1", "?side=buy&side=sell"} {
		if res := request(s, "GET", url+invalid, "", cookie, "", ""); res.Code != 400 {
			t.Errorf("invalid query accepted: %s %d", invalid, res.Code)
		}
	}
	now = now.Add(time.Hour)
	late := get("")
	if !late.Stale || *late.ObservedToMS != *all.ObservedToMS || len(late.Items) != 4 {
		t.Fatal("stale snapshot status or retained positions incorrect")
	}
}

func TestWatchingPositionsSchedulesEarlierSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(30 * time.Hour)
	f, a := dailyFixture(t, &now, "UTC")
	if err := f.Auth.Initialize(t.Context(), testutil.Password); err != nil {
		t.Fatal(err)
	}
	s := apiServer(t, f)
	cookie, csrf := loginHTTP(t, s)
	url := "/api/accounts/" + a.ID + "/positions"
	checkDue := func(at time.Time, want bool) {
		t.Helper()
		ids, err := f.Accounts.Due(t.Context(), at.UnixMilli(), 10)
		if err != nil || (len(ids) == 1 && ids[0] == a.ID) != want {
			t.Fatalf("due=%v want=%t error=%v", ids, want, err)
		}
	}
	observed := now
	now = observed.Add(5 * time.Second)
	earlier := observed.Add(10 * time.Second)
	for _, method := range []string{"GET", "POST"} {
		origin := "https://portexa.test"
		returned := request(s, method, url, "", cookie, "", origin)
		if method == "GET" && returned.Code != 200 || method == "POST" && returned.Code != 403 {
			t.Fatalf("%s returned %d", method, returned.Code)
		}
		checkDue(earlier, false)
	}
	history, err := f.Accounts.ClaimHistory(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(s, "POST", url, "", cookie, csrf, "https://portexa.test"); response.Code != 200 {
		t.Fatalf("watching during history returned %d", response.Code)
	}
	checkDue(earlier, false)
	if err := f.Accounts.FailHistory(t.Context(), history, accounts.Failure{Canceled: true, Code: "synthetic_done"}, false); err != nil {
		t.Fatal(err)
	}
	moveJob := func(column string) {
		t.Helper()
		err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
			return tx.Model(&a).Update(column, observed.Add(5*time.Minute).UnixMilli()).Error
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	moveJob("next_daily_sync_at_ms")
	if response := request(s, "POST", url, "", cookie, csrf, "https://portexa.test"); response.Code != 200 {
		t.Fatalf("watching with upcoming history returned %d", response.Code)
	}
	// The retry becomes due just as the faster snapshot would start.
	checkDue(earlier, false)
	moveJob("next_history_sync_at_ms")
	for i := 0; i < 2; i++ {
		response := request(s, "POST", url, "", cookie, csrf, "https://portexa.test")
		if response.Code != 200 {
			t.Fatalf("watching returned %d", response.Code)
		}
		checkDue(earlier.Add(-time.Millisecond), false)
		checkDue(earlier, true)
	}
	now = earlier
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := testutil.Result("USC")
	result.Trades[0].Ticket = 10002
	snapshot, rows, err := syncer.Convert(job, result, now.UnixMilli()-10, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.Publish(t.Context(), job, snapshot, rows, now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	response := request(s, "GET", url, "", cookie, "", "")
	var view accounts.LivePositions
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil ||
		view.ObservedToMS == nil || *view.ObservedToMS != now.UnixMilli() ||
		len(view.Items) != 1 || view.Items[0].Ticket != 10002 {
		t.Fatalf("new snapshot not reflected in positions: HTTP %d", response.Code)
	}
	checkDue(now.Add(10*time.Second), false)
}
