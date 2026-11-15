package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func TestRestLearningSettingsAndStatusAuthorization(t *testing.T) {
	f, a, now, start := restFixture(t)
	*now = start
	restSnapshot(t, f, a, *now, "XAUUSD.c", 2)
	if err := f.Auth.Initialize(t.Context(), testutil.Password); err != nil {
		t.Fatal(err)
	}
	s := apiServer(t, f)
	cookie, csrf := loginHTTP(t, s)
	if w := request(s, "GET", "/api/market-rest/learning", "", nil, "", ""); w.Code != 401 {
		t.Fatal("unprotected learning status", w.Code)
	}
	w := request(s, "GET", "/api/market-rest/learning", "", cookie, "", "")
	var groups []accounts.RestLearningView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &groups) != nil || len(groups) != 1 || groups[0].ValidWeekends != 0 || groups[0].Active != nil {
		t.Fatal("invented initial learning", w.Code, w.Body.String())
	}
	before, err := f.Accounts.CollectionSettings(t.Context())
	if err != nil || !before.MarketRestLearningEnabled {
		t.Fatal("missing learning default", err)
	}
	*now = now.Add(time.Minute)
	body := `{"snapshot_interval_seconds":60,"market_rest_learning_enabled":false}`
	if w = request(s, "PUT", "/api/settings/collection", body, cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("missing CSRF", w.Code)
	}
	if w = request(s, "PUT", "/api/settings/collection", body, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	off, err := f.Accounts.CollectionSettings(t.Context())
	if err != nil || off.MarketRestLearningEnabled || off.MarketRestPolicyRevision <= before.MarketRestPolicyRevision {
		t.Fatal("learning not versioned", err)
	}
	restSnapshot(t, f, a, *now, "XAUUSD.c", 2)
	var count int64
	f.Store.Read.Model(&storage.MarketActivityObservation{}).Count(&count)
	if count != 1 {
		t.Fatal("collected while learning disabled", count)
	}
	w = request(s, "GET", "/api/market-rest/learning", "", cookie, "", "")
	if json.Unmarshal(w.Body.Bytes(), &groups) != nil || len(groups) != 1 || groups[0].State != "off" {
		t.Fatal("off status missing")
	}
	// Legacy saves preserve the explicit learning switch.
	if w = request(s, "PUT", "/api/settings/collection", `{"snapshot_interval_seconds":120}`, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	legacy, _ := f.Accounts.CollectionSettings(t.Context())
	if legacy.MarketRestLearningEnabled {
		t.Fatal("legacy save re-enabled learning")
	}
	*now = now.Add(time.Minute)
	on := true
	resumed, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 60, MarketRestLearningEnabled: &on})
	if err != nil || resumed.MarketRestLearningEpochMS <= before.MarketRestLearningEpochMS {
		t.Fatal("re-enable reused old learning cycle", err)
	}
	// Historical policy remains immutable and old groups aren't adopted anew.
	var old storage.MarketRestPolicy
	f.Store.Read.First(&old, "revision=?", before.MarketRestPolicyRevision)
	if !old.LearningEnabled {
		t.Fatal("rewrote historical switch")
	}
	current, err := f.Accounts.RestLearning(t.Context())
	if err != nil || len(current) != 0 {
		t.Fatal("invented new epoch evidence", err)
	}
}
