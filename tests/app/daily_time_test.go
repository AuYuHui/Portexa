package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestDailyTimeRequiresConfirmationAndProtectsHTTPWrites(t *testing.T) {
	now := time.Date(2026, 11, 10, 23, 30, 0, 0, time.UTC)
	f, a := dailyFixture(t, &now, "")
	ctx := context.Background()
	detail, err := f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.State != "time_zone_unconfirmed" || detail.Account.Daily.Report != nil {
		t.Fatal("unconfirmed clock produced daily money", err)
	}
	if _, err = f.Accounts.ClaimDaily(ctx, a.ID); !errors.Is(err, accounts.ErrServerTime) {
		t.Fatal("unconfirmed clock scheduled history", err)
	}
	if err = f.Auth.Initialize(ctx, testutil.Password); err != nil {
		t.Fatal(err)
	}
	api := apiServer(t, f)
	cookie, csrf := loginHTTP(t, api)
	path := "/api/servers/" + a.ServerID + "/time-zone"
	input := `{"previous_ruleset_id":"","time_zone":"Europe/Helsinki","evidence_ref":"synthetic-confirmation","confirm":true}`
	if w := request(api, "PUT", path, input, nil, "", "https://portexa.test"); w.Code != 401 {
		t.Fatal("unauthenticated clock update", w.Code)
	}
	if w := request(api, "PUT", path, input, cookie, "", "https://portexa.test"); w.Code != 403 {
		t.Fatal("clock update bypassed CSRF", w.Code)
	}
	if w := request(api, "PUT", path, input, cookie, csrf, "https://portexa.test"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(api, "PUT", path, input, cookie, csrf, "https://portexa.test"); w.Code != 409 {
		t.Fatal("concurrent clock edit overwritten", w.Code)
	}
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := uint32(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC).Unix())
	if job.Day.Date != "2026-11-11" || job.Day.Start != expected || job.Day.End-job.Day.Start != 86400 {
		t.Fatal("server calendar interpreted as host UTC", job.Day)
	}
	now = now.Add(time.Second)
	if _, err = f.Accounts.ConfirmServerTime(ctx, a.ServerID, accounts.ServerTimeInput{
		PreviousRulesetID: job.Day.TimeRulesetID, TimeZone: "UTC", EvidenceRef: "synthetic-revised-clock", Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := syncer.ConvertDaily(job, dailyResult(job), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishDaily(ctx, job, report, now.Add(time.Minute).UnixMilli()); !errors.Is(err, storage.ErrConflict) {
		t.Fatal("old time rules published", err)
	}
	if err = f.Accounts.FailDaily(ctx, job, accounts.Failure{Canceled: true, Code: "configuration_changed"}); err != nil {
		t.Fatal(err)
	}
	detail, err = f.Accounts.Detail(ctx, a.ID, "")
	if err != nil || detail.Account.Daily.Date != "2026-11-10" || detail.Account.Daily.Report != nil {
		t.Fatal("old day reused under new timezone", err)
	}
	for _, input := range []accounts.ServerTimeInput{
		{TimeZone: "UTC", EvidenceRef: "synthetic", Confirm: false},
		{TimeZone: "Local", EvidenceRef: "synthetic", Confirm: true},
		{TimeZone: "invalid-zone", EvidenceRef: "synthetic", Confirm: true},
	} {
		if _, err = f.Accounts.ConfirmServerTime(ctx, a.ServerID, input); err == nil {
			t.Fatal("unusable time confirmation accepted", input.TimeZone)
		}
	}
}

func TestDailyServerDateRolloverAndDST(t *testing.T) {
	for _, stamp := range []time.Time{
		time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC),
		time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC),
	} {
		t.Run(stamp.Format("2006-01-02"), func(t *testing.T) {
			now := stamp
			f, a := dailyFixture(t, &now, "Europe/Helsinki")
			ctx := context.Background()
			job, err := f.Accounts.ClaimDaily(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Day.End-job.Day.Start != 86400 {
				t.Fatal("raw wall date should not be a UTC DST interval", job.Day)
			}
			r := dailyResult(job)
			r.History = []mt4.Trade{{Ticket: 1, Command: 1, CloseTime: job.Day.Start + 12600, Profit: 9.99}}
			report, err := syncer.ConvertDaily(job, r, now.UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Accounts.PublishDaily(ctx, job, report, job.Day.NextDayUTC.UnixMilli()); err != nil {
				t.Fatal(err)
			}
			now = job.Day.NextDayUTC.Add(time.Second)
			detail, err := f.Accounts.Detail(ctx, a.ID, "")
			if err != nil || detail.Account.Daily.Date == job.Day.Date || detail.Account.Daily.Report != nil {
				t.Fatal("yesterday's profit survived rollover", err)
			}
		})
	}
}

func TestDailyRestoreInterruptsAnInFlightHistoryRun(t *testing.T) {
	now := time.Now()
	f, a := dailyFixture(t, &now, "UTC")
	ctx := context.Background()
	job, err := f.Accounts.ClaimDaily(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = f.Store.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "restored.sqlite")
	if err = storage.RestoreNew(ctx, backup, path); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var run storage.DailyReport
	if err = restored.Read.First(&run, "id = ?", job.Run.ID).Error; err != nil || run.State != "interrupted" {
		t.Fatal("restored history retained its fetch lock", err)
	}
	var account storage.Account
	if err = restored.Read.First(&account, "id = ?", a.ID).Error; err != nil || account.SyncEnabled {
		t.Fatal("restore enabled collection", err)
	}
}
