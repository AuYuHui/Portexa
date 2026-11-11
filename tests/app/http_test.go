package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"portexa/internal/accounts"
	"portexa/internal/httpapi"
	"portexa/tests/testutil"
)

type noopScheduler struct{}

func (noopScheduler) Wake()         {}
func (noopScheduler) Cancel(string) {}

func apiServer(t *testing.T, f testutil.Fixture) *httpapi.Server {
	t.Helper()
	s, err := httpapi.New(f.Auth, f.Accounts, noopScheduler{}, httpapi.Config{Origin: "https://portexa.test"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func request(handler http.Handler, method, path, body string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://portexa.test"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:45678"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func loginHTTP(t *testing.T, s http.Handler) (*http.Cookie, string) {
	t.Helper()
	w := request(s, "POST", "/api/auth/login", `{"password":"`+testutil.Password+`"}`, nil, "", "https://portexa.test")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var body struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.CSRF == "" {
		t.Fatal("missing csrf", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	return cookies[0], body.CSRF
}

func TestHTTPAuthenticationBoundary(t *testing.T) {
	f := testutil.New(t, true)
	s := apiServer(t, f)
	for _, path := range []string{"/api/overview", "/api/accounts", "/api/servers", "/api/accounts/none", "/api/accounts/none/snapshots", "/api/unknown"} {
		if w := request(s, "GET", path, "", nil, "", ""); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	if w := request(s, "POST", "/api/auth/login", `{"password":"`+testutil.Password+`"}`, nil, "", ""); w.Code != 403 {
		t.Fatal("missing origin accepted")
	}
	if w := request(s, "POST", "/api/auth/login", `{"password":"`+testutil.Password+`"}`, nil, "", "https://evil.test"); w.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
	cookie, csrf := loginHTTP(t, s)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Name != "__Host-portexa" || cookie.Path != "/" {
		t.Fatal("unsafe cookie", cookie.Name)
	}
	if w := request(s, "GET", "/api/overview", "", cookie, "", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	missing := request(s, "GET", "/api/overview", "", cookie, "", "")
	var payload struct {
		Total struct {
			Equity json.RawMessage `json:"equity"`
		} `json:"usd_total"`
	}
	if err := json.Unmarshal(missing.Body.Bytes(), &payload); err != nil || string(payload.Total.Equity) != "null" {
		t.Fatal("unknown equity serialized as zero", missing.Body)
	}
	if w := request(s, "POST", "/api/accounts", "{}", cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w := request(s, "POST", "/api/accounts", `{"username":"ignored"}`, cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("unknown field accepted")
	}
	if w := request(s, "PUT", "/api/auth/password", `{"old_password":"wrong","new_password":"new-synthetic-login-password"}`, cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("wrong old password status", w.Code)
	}
	if w := request(s, "GET", "/api/overview", "", cookie, "", ""); w.Code != 200 {
		t.Fatal("wrong old password revoked current session")
	}
	if w := request(s, "POST", "/api/auth/logout", "", cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(s, "GET", "/api/overview", "", cookie, "", ""); w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
}

func TestHTTPRedactionAndSortWhitelist(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-API")
	a := f.Account(t, server, endpoint, 1003, "USC")
	s := apiServer(t, f)
	cookie, csrf := loginHTTP(t, s)
	w := request(s, "GET", "/api/accounts/"+a.ID, "", cookie, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, secret := range []string{testutil.InvestorPassword, "credential_envelope", "credential_key_id", "password_hash", "token_sha256"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret exposed", secret)
		}
	}
	for _, path := range []string{"/api/accounts/" + a.ID + "?sort=profit", "/api/accounts/" + a.ID + "/snapshots?sort=equity", "/api/accounts?sort=balance"} {
		if w := request(s, "GET", path, "", cookie, "", ""); w.Code != 400 {
			t.Fatal("unsupported sort accepted", path, w.Code)
		}
	}
	_, pending, err := f.Accounts.CreateServer(context.Background(), accounts.ServerInput{BrokerName: "Synthetic", ServerName: "Pending", IPAddress: "203.0.113.10", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(accounts.AccountInput{ServerID: pending.ServerID, EndpointID: pending.ID, Login: 1004, Label: "合成账户", Password: testutil.InvestorPassword, SyncEnabled: true})
	if w := request(s, "POST", "/api/accounts", string(body), cookie, csrf, "https://portexa.test"); w.Code != 409 {
		t.Fatal("unreviewed endpoint accepted", w.Code, w.Body)
	}
}

func TestLoginRateLimitAndProductionOrigin(t *testing.T) {
	f := testutil.New(t, true)
	s := apiServer(t, f)
	for i := 0; i < 5; i++ {
		request(s, "POST", "/api/auth/login", `{"password":"wrong"}`, nil, "", "https://portexa.test")
	}
	if w := request(s, "POST", "/api/auth/login", `{"password":"`+testutil.Password+`"}`, nil, "", "https://portexa.test"); w.Code != 429 {
		t.Fatal("no login limit", w.Code)
	}
	for _, config := range []httpapi.Config{{Origin: "http://portexa.test"}, {Origin: "http://0.0.0.0:8080", DevelopmentHTTP: true}, {Origin: "https://portexa.test/path"}} {
		if _, err := httpapi.New(f.Auth, f.Accounts, noopScheduler{}, config); err == nil {
			t.Fatal("unsafe origin accepted", config.Origin)
		}
	}
}
