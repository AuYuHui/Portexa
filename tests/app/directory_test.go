package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"portexa/internal/discovery"
	"portexa/internal/httpapi"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

const directoryBody = `{"result":[{"company":"Synthetic Directory Broker","results":[{"name":"Synthetic-DirectoryLive","access":["203.0.113.9:443","203.0.113.10:443"],"is_demo":0}]}]}`

func TestAuthenticatedDirectoryRegistrationAndRevision(t *testing.T) {
	f := testutil.New(t, true)
	calls := 0
	catalog := discovery.New(discovery.Options{AllowTarget: f.Accounts.AllowTarget, Fetch: func(context.Context, string) ([]byte, error) { calls++; return []byte(directoryBody), nil }})
	s, err := httpapi.New(f.Auth, f.Accounts, noopScheduler{}, httpapi.Config{Origin: "https://portexa.test", Directory: catalog})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(s, "GET", "/api/server-directory?q=Synthetic", "", nil, "", ""); w.Code != 401 || calls != 0 {
		t.Fatal("anonymous directory access", w.Code, calls)
	}
	cookie, csrf := loginHTTP(t, s)
	w := request(s, "GET", "/api/server-directory?q=Synthetic", "", cookie, "", "")
	var list []discovery.Candidate
	if err = json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || len(list) != 1 || len(list[0].Endpoints) != 2 {
		t.Fatal("search response", w.Code, err)
	}
	id := list[0].ID
	if w = request(s, "POST", "/api/servers/discovered", fmt.Sprintf(`{"candidate_id":%q,"accept_risk":false}`, id), cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("risk acceptance bypassed", w.Code)
	}
	if w = request(s, "POST", "/api/servers/discovered", fmt.Sprintf(`{"candidate_id":%q,"accept_risk":true,"ip_address":"127.0.0.1"}`, id), cookie, csrf, "https://portexa.test"); w.Code != 400 {
		t.Fatal("client overrode address", w.Code)
	}
	if w = request(s, "POST", "/api/servers/discovered", `{"candidate_id":"forged","accept_risk":true}`, cookie, csrf, "https://portexa.test"); w.Code != 409 {
		t.Fatal("forged candidate accepted", w.Code)
	}
	w = request(s, "POST", "/api/servers/discovered", fmt.Sprintf(`{"candidate_id":%q,"accept_risk":true}`, id), cookie, csrf, "https://portexa.test")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var registered struct {
		Server   storage.Server   `json:"server"`
		Endpoint storage.Endpoint `json:"endpoint"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	var persisted storage.Endpoint
	if err = f.Store.Read.First(&persisted, "id = ?", registered.Endpoint.ID).Error; err != nil || !persisted.Approved() {
		t.Fatal("source/risk not saved", err)
	}
	var count int64
	f.Store.Read.Table("server_directory_evidence").Count(&count)
	if count != 1 {
		t.Fatal("source evidence not persisted")
	}
	a := f.Account(t, registered.Server, persisted, 7001, "USD")
	path := fmt.Sprintf("/api/servers/%s/discovered-endpoints", registered.Server.ID)
	w = request(s, "POST", path, fmt.Sprintf(`{"candidate_id":%q,"endpoint_index":1,"accept_risk":true}`, id), cookie, csrf, "https://portexa.test")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var updated storage.Account
	if err = f.Store.Read.First(&updated, "id = ?", a.ID).Error; err != nil || updated.SyncEnabled {
		t.Fatal("address revision did not pause account", err)
	}
	f.Store.Read.First(&persisted, "id = ?", persisted.ID)
	if persisted.State != "retired" {
		t.Fatal("old endpoint remained live")
	}
	otherServer, _ := f.Server(t, "Synthetic-Other")
	w = request(s, "POST", fmt.Sprintf("/api/servers/%s/discovered-endpoints", otherServer.ID), fmt.Sprintf(`{"candidate_id":%q,"accept_risk":true}`, id), cookie, csrf, "https://portexa.test")
	if w.Code != 400 {
		t.Fatal("cross-server directory revision accepted", w.Code)
	}
}
