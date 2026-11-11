package app_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func TestCollectionSettingsHTTP(t *testing.T) {
	f := testutil.New(t, true)
	s := apiServer(t, f)
	const path = "/api/settings/collection"
	if w := request(s, "GET", path, "", nil, "", ""); w.Code != 401 {
		t.Fatal("anonymous settings read", w.Code)
	}
	if w := request(s, "PUT", path, `{"snapshot_interval_seconds":30}`, nil, "", "https://portexa.test"); w.Code != 401 {
		t.Fatal("anonymous settings write", w.Code)
	}
	cookie, csrf := loginHTTP(t, s)
	read := func() storage.CollectionSettings {
		t.Helper()
		w := request(s, "GET", path, "", cookie, "", "")
		var result storage.CollectionSettings
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal("read collection settings", w.Code, w.Body)
		}
		return result
	}
	if read().SnapshotIntervalSeconds != 60 {
		t.Fatal("unexpected default cycle")
	}
	for _, credentials := range [][2]string{{"", "https://portexa.test"}, {csrf, "https://evil.test"}} {
		if w := request(s, "PUT", path, `{"snapshot_interval_seconds":30}`, cookie, credentials[0], credentials[1]); w.Code != 403 {
			t.Fatal("unprotected settings write", w.Code)
		}
	}
	for _, body := range []string{
		`{}`, `null`, `{"snapshot_interval_seconds":null}`, `{"snapshot_interval_seconds":0}`,
		`{"snapshot_interval_seconds":-30}`, `{"snapshot_interval_seconds":10}`, `{"snapshot_interval_seconds":31}`,
		`{"snapshot_interval_seconds":301}`, `{"snapshot_interval_seconds":30.5}`, `{"snapshot_interval_seconds":"30"}`,
		`{"snapshot_interval_seconds":30,"workers":100}`, `{"snapshot_interval_seconds":30} {}`,
	} {
		if w := request(s, "PUT", path, body, cookie, csrf, "https://portexa.test"); w.Code != 400 {
			t.Fatalf("invalid settings accepted: %s (%d)", body, w.Code)
		}
	}
	if read().SnapshotIntervalSeconds != 60 {
		t.Fatal("invalid update changed settings")
	}
	for _, seconds := range []int{30, 60, 120, 300} {
		body := fmt.Sprintf(`{"snapshot_interval_seconds":%d}`, seconds)
		if w := request(s, "PUT", path, body, cookie, csrf, "https://portexa.test"); w.Code != 200 {
			t.Fatal("save settings", w.Code, w.Body)
		}
		if settings := read(); settings.SnapshotIntervalSeconds != seconds || settings.UpdatedAtMS == 0 {
			t.Fatal("saved settings unavailable", settings)
		}
		w := request(s, "GET", "/api/overview", "", cookie, "", "")
		var overview accounts.Overview
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &overview) != nil || overview.CollectionSettings.SnapshotIntervalSeconds != seconds {
			t.Fatal("overview shows the wrong cycle", w.Code, w.Body)
		}
	}
}

func TestCollectionChangePreservesAccountIntentAndBackoff(t *testing.T) {
	f := testutil.New(t, false)
	now := time.Now().Truncate(time.Millisecond)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(t, "Synthetic-Collection")
	states := []string{"scheduled", "manual", "backoff", "paused", "removed", "first"}
	before := map[string]storage.Account{}
	for i, state := range states {
		a := f.Account(t, server, endpoint, uint32(8900+i), "USD")
		if state != "first" {
			job, err := f.Accounts.Claim(t.Context(), a.ID)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, positions, err := syncer.Convert(job, testutil.Result("USD"), now.UnixMilli(), now.UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Accounts.Publish(t.Context(), job, snapshot, positions, now.Add(time.Hour).UnixMilli()); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
			updates := map[string]any{}
			switch state {
			case "manual":
				updates["next_sync_at_ms"] = now.UnixMilli()
			case "backoff":
				updates["failure_count"] = 2
			case "paused":
				updates["sync_enabled"] = false
			case "removed":
				updates["removed_at_ms"] = now.UnixMilli()
			}
			return tx.Model(&a).Updates(updates).Error
		}); err != nil {
			t.Fatal(err)
		}
		if err := f.Store.Read.First(&a, "id = ?", a.ID).Error; err != nil {
			t.Fatal(err)
		}
		before[state] = a
	}
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	for state, original := range before {
		var current storage.Account
		if err := f.Store.Read.First(&current, "id = ?", original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if state == "scheduled" {
			gap := time.UnixMilli(current.NextSyncAtMS).Sub(now)
			if gap < accounts.DailyAdmissionLead+2*time.Second || gap > 30*time.Second {
				t.Fatal("new cycle does not reserve a history window", gap)
			}
		} else if current.NextSyncAtMS != original.NextSyncAtMS {
			t.Fatal("cycle change overwrote account intent", state)
		}
		if current.SyncEnabled != original.SyncEnabled || current.ConfigVersion != original.ConfigVersion || current.FailureCount != original.FailureCount {
			t.Fatal("cycle change mutated account authorization or backoff", state)
		}
	}
	var runs int64
	if err := f.Store.Read.Model(&storage.SyncRun{}).Count(&runs).Error; err != nil || runs != 5 {
		t.Fatal("settings update created collection work", runs, err)
	}
	var scheduled storage.Account
	if err := f.Store.Read.First(&scheduled, "id = ?", before["scheduled"].ID).Error; err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	var unchanged storage.Account
	if err := f.Store.Read.First(&unchanged, "id = ?", scheduled.ID).Error; err != nil || unchanged.NextSyncAtMS != scheduled.NextSyncAtMS {
		t.Fatal("saving unchanged settings postponed work", err)
	}
}

func TestCollectionSettingsSurviveReopenAndBackupRestore(t *testing.T) {
	f := testutil.New(t, true)
	server, endpoint := f.Server(t, "Synthetic-Collection-Restore")
	a := f.Account(t, server, endpoint, 8990, "USD")
	if _, err := f.Accounts.UpdateCollectionSettings(t.Context(), accounts.CollectionInput{SnapshotIntervalSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err := f.Store.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	service := accounts.New(reopened, f.Vault, nil)
	settings, err := service.CollectionSettings(t.Context())
	if err != nil || settings.SnapshotIntervalSeconds != 120 {
		t.Fatal("reopen lost collection settings", settings, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.sqlite")
	if err := storage.RestoreNew(t.Context(), backup, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	service = accounts.New(restored, f.Vault, nil)
	settings, err = service.CollectionSettings(t.Context())
	if err != nil || settings.SnapshotIntervalSeconds != 120 {
		t.Fatal("restore lost collection settings", settings, err)
	}
	var account storage.Account
	if err := restored.Read.First(&account, "id = ?", a.ID).Error; err != nil || account.SyncEnabled {
		t.Fatal("restoring collection settings enabled collection", err)
	}
}
