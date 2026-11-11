package app_test

import (
	"testing"

	"portexa/tests/testutil"
)

func TestFundingHTTPAuthClassificationConflictAndPagination(t *testing.T) {
	f, a, _, _ := fundingFixture(t)
	if err := f.Auth.Initialize(t.Context(), testutil.Password); err != nil {
		t.Fatal(err)
	}
	s := apiServer(t, f)
	base := "/api/accounts/" + a.ID
	for _, path := range []string{base + "/funds", base + "/history/sync", base + "/funds/6/classification"} {
		method := "GET"
		if path == base+"/history/sync" {
			method = "POST"
		}
		if path == base+"/funds/6/classification" {
			method = "PUT"
		}
		if w := request(s, method, path, "", nil, "", "https://portexa.test"); w.Code != 401 {
			t.Fatal("unprotected funds route", path, w.Code)
		}
	}
	cookie, csrf := loginHTTP(t, s)
	body := `{"trade_revision":1,"classification_revision":0,"category":"deposit","reason":"合成到账记录核对"}`
	if w := request(s, "PUT", base+"/funds/6/classification", body, cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w := request(s, "PUT", base+"/funds/6/classification", body, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "PUT", base+"/funds/6/classification", body, cookie, csrf, "https://portexa.test"); w.Code != 409 {
		t.Fatal("stale classification overwrite accepted", w.Code, w.Body)
	}
	if w := request(s, "GET", base+"/funds?limit=2", "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=oops", "cursor=oops", "unclassified=oops", "sort=profit"} {
		if w := request(s, "GET", base+"/funds?"+query, "", cookie, "", ""); w.Code != 400 {
			t.Fatal("invalid paging accepted", query, w.Code, w.Body)
		}
	}
	if w := request(s, "PUT", base+"/funds/8/classification", body, cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("credit counted as deposit", w.Code, w.Body)
	}
	if w := request(s, "PUT", base+"/funds/2147483648/classification", body, cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("invalid ticket accepted", w.Code)
	}
	if w := request(s, "POST", base+"/history/sync", "", cookie, csrf, "https://portexa.test"); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
}
