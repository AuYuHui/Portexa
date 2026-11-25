package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"portexa/internal/accounts"
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
