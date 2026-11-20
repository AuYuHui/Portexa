package app_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/httpapi"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestDeleteAccountStopsCollectionAndAllowsReconnection(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-Deletion")
	account := f.Account(t, server, endpoint, 7201, "USD")
	other := f.Account(t, server, endpoint, 7202, "USD")
	before := trendSample(t, f, account, time.Now(), 1000)
	if err := f.Accounts.RequestSync(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	excluded := []string{account.ID, other.ID}
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{
		SnapshotIntervalSeconds: 60, MarketRestExcludedAccountIDs: &excluded,
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 1)
	engine := syncer.New(f.Accounts, func(ctx context.Context, config mt4.Config, _ mt4.Options) (mt4.Result, error) {
		if uint64(config.Login) != account.Login {
			return testutil.Result("USD"), nil
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return mt4.Result{}, ctx.Err()
	}, syncer.Options{})
	const origin = "https://portexa.test"
	s, err := httpapi.New(f.Auth, f.Accounts, engine, httpapi.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	startEngine(t, engine)
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("snapshot fetch did not start")
	}

	path := "/api/accounts/" + account.ID
	cookie, csrf := loginHTTP(t, s)
	if w := request(s, "DELETE", path, "", nil, "", origin); w.Code != http.StatusUnauthorized {
		t.Fatal("deletion without a session", w.Code)
	}
	if w := request(s, "DELETE", path, "", cookie, "", origin); w.Code != http.StatusForbidden {
		t.Fatal("deletion without CSRF", w.Code)
	}
	if w := request(s, "DELETE", path, "", cookie, csrf, origin); w.Code != http.StatusNoContent {
		t.Fatal(w.Code, w.Body)
	}
	eventually(t, func() bool {
		var count int64
		return f.Store.Read.Model(&storage.SyncRun{}).Where("account_id = ? AND state = 'canceled'", account.ID).
			Count(&count).Error == nil && count == 1
	})

	var removed storage.Account
	if err := f.Store.Read.First(&removed, "id = ?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	if removed.SyncEnabled || removed.RemovedAtMS == nil || len(removed.CredentialEnvelope) != 0 ||
		removed.CredentialKeyID != "" || removed.ConfigVersion != account.ConfigVersion+1 ||
		removed.CurrentSnapshotID == nil || *removed.CurrentSnapshotID != before.ID {
		t.Fatal("incorrect removal or historical snapshot lost")
	}
	overview, err := f.Accounts.Overview(t.Context())
	if err != nil || len(overview.Accounts) != 1 || overview.Accounts[0].ID != other.ID {
		t.Fatal("deleted account still visible or another account affected", err)
	}
	settings, err := f.Accounts.CollectionSettings(t.Context())
	if err != nil || len(settings.MarketRestExcludedAccountIDs) != 1 || settings.MarketRestExcludedAccountIDs[0] != other.ID {
		t.Fatal("incorrect market-rest exceptions", err)
	}
	for _, suffix := range []string{"", "/snapshots", "/funds", "/deals"} {
		if w := request(s, "GET", path+suffix, "", cookie, "", ""); w.Code != http.StatusNotFound {
			t.Fatal(suffix, w.Code, w.Body)
		}
	}
	if w := request(s, "PATCH", path, `{"config_version":2,"sync_enabled":true}`, cookie, csrf, origin); w.Code != http.StatusNotFound {
		t.Fatal("deleted account could be enabled", w.Code, w.Body)
	}
	if w := request(s, "DELETE", path, "", cookie, csrf, origin); w.Code != http.StatusNotFound {
		t.Fatal("repeat deletion", w.Code, w.Body)
	}
	if _, err := f.Accounts.Claim(t.Context(), account.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("deleted account could be claimed", err)
	}

	reconnected := f.Account(t, server, endpoint, uint32(account.Login), "USD")
	if reconnected.ID == account.ID || reconnected.IncludedAtMS != nil || reconnected.CurrentSnapshotID != nil {
		t.Fatal("reconnected account inherited previous membership")
	}
}
