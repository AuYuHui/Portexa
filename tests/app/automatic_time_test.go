package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func automaticServer(t *testing.T, f testutil.Fixture, broker, name string) (storage.Server, storage.Endpoint) {
	t.Helper()
	ctx := context.Background()
	server, endpoint, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{
		BrokerName: broker, ServerName: name, IPAddress: "203.0.113.9", Port: 443,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = f.Accounts.Review(ctx, endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic-directory-review", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, endpoint
}

func TestAutomaticTimeMatchesBothBrokerAndServer(t *testing.T) {
	for _, test := range []struct {
		broker, server string
		available      bool
	}{
		{"D Prime Vanuatu Limited", "DPrimeVU-Demo 999", true},
		{"Doo Prime", "DooPrime-Demo99", true},
		{"Synthetic Markets", "DPrimeVU-Demo 999", false},
		{"D Prime Vanuatu Limited", "Synthetic-Demo999", false},
		{"Synthetic Markets", "Synthetic-Demo999", false},
	} {
		t.Run(test.broker+test.server, func(t *testing.T) {
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) }
			server, _ := automaticServer(t, f, test.broker, test.server)
			views, err := f.Accounts.Servers(context.Background())
			if err != nil || len(views) != 1 {
				t.Fatal(err)
			}
			view := views[0]
			if view.AutomaticTimeZone.Available != test.available || view.TimeZoneVerified != test.available {
				t.Fatal("server matching was not conservative", view)
			}
			if test.available && (view.TimeMode != "automatic" || view.TimeZone != "UTC+2" || server.ActiveTimeRulesetID == nil || view.AutomaticTimeZone.SourceURL == "") {
				t.Fatal("known server was not configured automatically", view)
			}
		})
	}
}

func TestAutomaticServerCalendarFollowsUSRatherThanEuropeDST(t *testing.T) {
	for _, test := range []struct {
		stamp, zone, nextDay string
	}{
		{"2026-02-20T23:30:00Z", "UTC+2", "2026-02-21T22:00:00Z"},
		{"2026-03-20T22:30:00Z", "UTC+3", "2026-03-21T21:00:00Z"},
		{"2026-10-28T22:30:00Z", "UTC+3", "2026-10-29T21:00:00Z"},
		{"2026-11-11T23:30:00Z", "UTC+2", "2026-11-12T22:00:00Z"},
		{"2026-03-08T02:00:00Z", "UTC+2", "2026-03-08T21:00:00Z"},
		{"2026-11-01T00:00:00Z", "UTC+3", "2026-11-01T22:00:00Z"},
	} {
		t.Run(test.stamp, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, test.stamp)
			f := testutil.New(t, false)
			f.Accounts.Now = func() time.Time { return now }
			server, endpoint := automaticServer(t, f, "D Prime Vanuatu Limited", "DPrimeVU-Demo 999")
			a := f.Account(t, server, endpoint, 7801, "USC")
			ctx := context.Background()
			job, err := f.Accounts.Claim(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, positions, err := syncer.Convert(job, testutil.Result("USC"), now.UnixMilli(), now.UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Accounts.Publish(ctx, job, snapshot, positions, now.Add(time.Hour).UnixMilli()); err != nil {
				t.Fatal(err)
			}
			daily, err := f.Accounts.ClaimDaily(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if daily.Day.TimeZone != test.zone || daily.Day.NextDayUTC.UTC().Format(time.RFC3339) != test.nextDay || daily.Day.End-daily.Day.Start != 86400 {
				t.Fatal("incorrect server calendar", daily.Day)
			}
		})
	}
}

func TestStartupAutoTimePreservesManualSettingsAndCanRestoreAutomatic(t *testing.T) {
	f := testutil.New(t, false)
	ctx := context.Background()
	server, _ := f.Server(t, "Synthetic-Existing")
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Model(&storage.Server{}).Where("id = ?", server.ID).Updates(map[string]any{
			"broker_name": "D Prime Vanuatu Limited", "server_name": "DPrimeVU-Demo 999",
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	views, err := f.Accounts.Servers(ctx)
	if err != nil || views[0].TimeMode != "automatic" {
		t.Fatal("existing server was not recognized on restart", err)
	}
	automaticID := views[0].TimeRulesetID
	if err = f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	views, err = f.Accounts.Servers(ctx)
	if err != nil || views[0].TimeRulesetID != automaticID {
		t.Fatal("startup duplicated the active policy", err)
	}
	if _, err = f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		PreviousRulesetID: automaticID, Mode: "manual", TimeZone: "UTC",
	}); err != nil {
		t.Fatal("simple manual correction failed", err)
	}
	if err = f.Accounts.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	views, err = f.Accounts.Servers(ctx)
	if err != nil || views[0].TimeMode != "manual" || views[0].TimeZone != "UTC" {
		t.Fatal("startup overwrote manual correction", err)
	}
	if _, err = f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		PreviousRulesetID: views[0].TimeRulesetID, Mode: "automatic",
	}); err != nil {
		t.Fatal("restoring automatic time required extra user input", err)
	}
	views, err = f.Accounts.Servers(ctx)
	if err != nil || views[0].TimeMode != "automatic" {
		t.Fatal("automatic time not restored", err)
	}
}
