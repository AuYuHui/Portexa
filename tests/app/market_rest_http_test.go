package app_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"portexa/internal/marketrest"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func TestMarketRestSettingsAuthorizationValidationAndVersions(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-Rest-Settings")
	a := f.Account(t, server, endpoint, 9872, "USD")
	s := apiServer(t, f)
	cookie, csrf := loginHTTP(t, s)
	read := func() storage.CollectionSettings {
		t.Helper()
		w := request(s, "GET", "/api/settings/collection", "", cookie, "", "")
		var result storage.CollectionSettings
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return result
	}
	initial := read()
	if initial.MarketRestTemplate != marketrest.Template || initial.MarketRestPolicyRevision <= 0 {
		t.Fatal("no built-in default", initial)
	}
	for _, body := range []string{`{"snapshot_interval_seconds":60,"market_rest_template":"guess"}`, `{"snapshot_interval_seconds":60,"market_rest_excluded_account_ids":["missing"]}`, fmt.Sprintf(`{"snapshot_interval_seconds":60,"market_rest_excluded_account_ids":[%q,%q]}`, a.ID, a.ID)} {
		if w := request(s, "PUT", "/api/settings/collection", body, cookie, csrf, "https://portexa.test"); w.Code != 400 {
			t.Fatal("invalid policy accepted", w.Code)
		}
	}
	if read().MarketRestPolicyRevision != initial.MarketRestPolicyRevision {
		t.Fatal("invalid save changed policy")
	}
	body := fmt.Sprintf(`{"snapshot_interval_seconds":60,"market_rest_template":"off","market_rest_excluded_account_ids":[%q]}`, a.ID)
	if w := request(s, "PUT", "/api/settings/collection", body, cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w := request(s, "PUT", "/api/settings/collection", body, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated := read()
	if updated.MarketRestTemplate != "off" || updated.MarketRestPolicyRevision <= initial.MarketRestPolicyRevision || len(updated.MarketRestExcludedAccountIDs) != 1 || updated.MarketRestExcludedAccountIDs[0] != a.ID {
		t.Fatal("policy did not persist", updated)
	}
	if w := request(s, "PUT", "/api/settings/collection", `{"snapshot_interval_seconds":120}`, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if latest := read(); latest.MarketRestPolicyRevision != updated.MarketRestPolicyRevision || latest.MarketRestTemplate != "off" {
		t.Fatal("legacy writer silently reset policy", latest)
	}
	var old storage.MarketRestPolicy
	if err := f.Store.Read.First(&old, "revision=?", initial.MarketRestPolicyRevision).Error; err != nil || old.Template != marketrest.Template {
		t.Fatal("historical policy was overwritten", old, err)
	}
}
