package app_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/discovery"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/mt5"
	"portexa/tests/testutil"
)

func TestMT5SnapshotsDealsFundingAndAnalytics(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	input := accounts.ServerInput{Protocol: "mt5", BrokerName: "Synthetic Broker", ServerName: "Synthetic-MT5",
		IPAddress: "203.0.113.9", Port: 2085, TimeZone: "UTC"}
	server, endpoint, err := f.Accounts.CreateServer(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	cfg := accounts.AccountInput{ServerID: server.ID, EndpointID: endpoint.ID, Login: 7_000_000_001,
		Label: "Synthetic MT5", Password: "synthetic-investor-password", ConfirmedCurrency: "USD", ConfirmUnit: true, SyncEnabled: true}
	if _, err := f.Accounts.CreateAccount(ctx, cfg); !errors.Is(err, accounts.ErrEndpoint) {
		t.Fatalf("account registration without verified endpoint: %v", err)
	}
	if _, err := f.Accounts.Review(ctx, endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic verification", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	}); err != nil {
		t.Fatal(err)
	}
	account, err := f.Accounts.CreateAccount(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Accounts.RequestHistory(ctx, account.ID); err == nil {
		t.Fatal("history request succeeded without a confirmed server time zone")
	}
	if err := f.Accounts.RequestDaily(ctx, account.ID); err == nil {
		t.Fatal("daily report request succeeded without a confirmed server time zone")
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	deals := testutil.MT5Deals(day)
	fetched := make(chan mt5.Config, 1)
	engine := syncer.New(f.Accounts, func(context.Context, mt4.Config, mt4.Options) (mt4.Result, error) {
		t.Error("MT5 account routed through MT4")
		return mt4.Result{}, errors.New("wrong protocol")
	}, syncer.Options{
		Interval: time.Hour,
		FetchMT5: func(ctx context.Context, input mt5.Config) (mt5.Result, error) {
			select {
			case fetched <- input:
			case <-ctx.Done():
				return mt5.Result{}, ctx.Err()
			}
			result := testutil.MT5Snapshot()
			result.Account.Margin = 0.7620692
			result.Account.MarginAvailable = true
			return result, nil
		},
		FetchMT5History: func(_ context.Context, _ mt5.Config, window mt5.HistoryRange) (mt5.HistoryResult, error) {
			return testutil.MT5History(window, deals), nil
		},
	})
	startEngine(t, engine)
	var view accounts.AccountDetail
	eventually(t, func() bool {
		view, err = f.Accounts.Detail(ctx, account.ID, "")
		return err == nil && view.Account.Snapshot != nil
	})
	used := <-fetched
	if used.IPAddress != input.IPAddress || used.Login != cfg.Login ||
		used.Server != input.ServerName || used.Password != cfg.Password || used.Port != input.Port {
		t.Fatal("snapshot did not use the reviewed endpoint and approved account credentials")
	}
	snapshot := view.Account.Snapshot
	if view.Account.Protocol != "mt5" || view.Account.Login != cfg.Login || view.Account.SyncState != "succeeded" ||
		snapshot.Equity.String() != "2054.7" || snapshot.FloatingNetProfit.String() != "4.7" ||
		!snapshot.MarginAvailable || snapshot.Margin == nil || snapshot.Margin.String() != "0.7620692" || snapshot.Leverage != 200 ||
		view.Account.Funding.State != "time_zone_unconfirmed" || view.Account.Daily.State != "time_zone_unconfirmed" {
		t.Fatalf("unexpected MT5 account snapshot: %+v", view.Account)
	}
	if len(view.Positions) != 2 {
		t.Fatalf("partially filled order did not retain its pending and open parts: %+v", view.Positions)
	}
	open, pending := view.Positions[0], view.Positions[1]
	if open.Ticket != pending.Ticket || open.Ticket != 8_000_000_002 || open.Lots.String() != "0.00123456" ||
		pending.Command != 7 || pending.Lots.String() != "0.00654321" || pending.TriggerPrice == nil || pending.TriggerPrice.String() != "1.2299" {
		t.Fatalf("MT5 order or position changed: %+v", view.Positions)
	}
	if _, err := f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		Mode: "manual", TimeZone: "UTC", EffectiveDate: day.AddDate(0, 0, -2).Format(time.DateOnly),
	}); err != nil {
		t.Fatal(err)
	}
	engine.Wake()
	eventually(t, func() bool {
		view, err = f.Accounts.Detail(ctx, account.ID, "")
		return err == nil && view.Account.Daily.Report != nil && view.Account.Funding.CoverageComplete
	})
	report := view.Account.Daily.Report
	if report.NetProfit.String() != "3" || report.ClosedTrades != 3 || *report.WinningTrades != 1 ||
		*report.LosingTrades != 1 || *report.BreakevenTrades != 1 || report.GrossProfit.String() != "9" ||
		report.GrossLoss.String() != "4.5" || report.BalanceAdjustment.String() != "37" ||
		report.BalanceChange.String() != "40" || report.CreditChange.String() != "4" {
		t.Fatalf("unexpected daily report: %+v", report)
	}
	funding := view.Account.Funding
	if funding.Deposits.String() != "50" || funding.Withdrawals.String() != "20" || funding.UnclassifiedCount != 1 {
		t.Fatalf("deal types or unknown balance adjustment misclassified: %+v", funding)
	}
	page, err := f.Accounts.Deals(ctx, account.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Ticket != 8_000_001_012 || page.NextCursor == "" {
		t.Fatalf("latest deal page: %+v, %v", page, err)
	}
	second, err := f.Accounts.Deals(ctx, account.ID, page.NextCursor, 100)
	if err != nil || len(second.Items) != len(deals)-1 || second.Items[0].Ticket != testutil.MT5UnknownFundingTicket || second.NextCursor != "" {
		t.Fatalf("deal page cursor lost a wide ticket: %+v, %v", second, err)
	}
	unknown, err := f.Accounts.Funds(ctx, account.ID, "", 10, true)
	if err != nil || len(unknown.Items) != 1 || unknown.Items[0].Ticket != testutil.MT5UnknownFundingTicket {
		t.Fatalf("unknown funding page: %+v, %v", unknown, err)
	}
	if _, err = f.Accounts.ClassifyFund(ctx, account.ID, testutil.MT5UnknownFundingTicket, accounts.ClassificationInput{
		Category: "deposit", TradeRevision: 1, Reason: "synthetic verification",
	}); err != nil {
		t.Fatal(err)
	}
	view, err = f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || view.Account.Funding.Deposits.String() != "57" || view.Account.Funding.UnclassifiedCount != 0 {
		t.Fatalf("manual classification did not update funding: %+v, %v", view.Account.Funding, err)
	}

	interval := accounts.AnalyticsRequest{AccountID: account.ID, FromMS: day.Add(-time.Minute).UnixMilli(), ToMS: day.UnixMilli(), Unit: "native"}
	activity, err := f.Accounts.AnalyticsActivity(ctx, interval, 0)
	if err != nil || len(activity.Items) != 4 || activity.Items[0].Kind != "trade_booking" ||
		activity.Items[0].TimeMS != day.UnixMilli()-40_000+345 || activity.Items[1].Kind != "trade" ||
		activity.Items[2].Kind != "fee" || activity.Items[3].Kind != "dividend" {
		t.Fatalf("unexpected MT5 activity: %+v, %v", activity, err)
	}
	seedMT5Valuations(t, f, account, interval.FromMS, interval.ToMS)
	performance, err := f.Accounts.Analytics(ctx, interval)
	metrics := performance.Result.Metrics
	if err != nil || metrics.Realized.Value == nil || metrics.Realized.Value.String() != "17" ||
		metrics.Costs.Value == nil || metrics.Costs.Value.String() != "-2" ||
		metrics.OtherIncome.Value == nil || metrics.OtherIncome.Value.String() != "3" ||
		metrics.NetProfit.Value == nil || metrics.NetProfit.Value.String() != "18" ||
		metrics.ReturnPct.Value == nil || metrics.ReturnPct.Value.String() != "1.8" ||
		performance.Result.Quality.Reconciliation != "passed" ||
		len(performance.Result.Days) != 1 || performance.Result.Days[0].Trades == nil || *performance.Result.Days[0].Trades != 1 {
		t.Fatalf("unexpected MT5 performance: metrics %+v, quality %+v, err %v", metrics, performance.Result.Quality, err)
	}
	var revisions int64
	if err := f.Store.Read.Model(&storage.HistoryOrder{}).Where("account_id = ?", account.ID).Count(&revisions).Error; err != nil ||
		revisions != int64(len(deals)) {
		t.Fatalf("overlapping windows changed unchanged deal revisions: %d, %v", revisions, err)
	}
}

func seedMT5Valuations(t *testing.T, fixture testutil.Fixture, account storage.Account, from, to int64) {
	t.Helper()
	err := fixture.Store.Write(t.Context(), storage.High, func(tx *gorm.DB) error {
		for i, at := range []int64{from, to} {
			balance := decimal.NewFromInt([]int64{1000, 1018}[i])
			run := storage.SyncRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID,
				EndpointID: account.PinnedEndpointID, CredentialVersion: account.CredentialVersion, ConfigVersion: account.ConfigVersion,
				Kind: "snapshot", State: "succeeded", DueAtMS: at, NotBeforeMS: at, StartedAtMS: &at, FinishedAtMS: &at, Attempt: 1}
			if err := tx.Create(&run).Error; err != nil {
				return err
			}
			sample := storage.Snapshot{ID: storage.NewID(), AccountID: account.ID, SyncRunID: run.ID, ObservedFromMS: at, ObservedToMS: at,
				NativeCurrency: "USD", UnitRevision: account.UnitRevision, USDScale: account.USDScale, Balance: balance, Equity: balance}
			if err := tx.Create(&sample).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMT5NativeDiscoveryAndSync(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	now := time.Now()

	// 1. Simulate MT5 Candidate from directory search
	evidence := []byte(`[{"companyName":"EC Markets Ltd.","results":[{"name":"ECMarketsLtd-MT5-Live08","access":["203.0.113.9:2085"]}]}]`)
	evidenceSHA := sha256.Sum256(evidence)
	candidate := discovery.Candidate{
		ID:             "mt5-cand-1234",
		Protocol:       "mt5",
		BrokerName:     "EC Markets Ltd.",
		ServerName:     "ECMarketsLtd-MT5-Live08",
		Endpoints:      []discovery.Access{{IPAddress: "203.0.113.9", Port: 2085}},
		SourceName:     "MTAPI MT5 Directory",
		SourceURL:      discovery.MT5SourceURL,
		FetchedAtMS:    now.UnixMilli(),
		ExpiresAtMS:    now.Add(15 * time.Minute).UnixMilli(),
		EvidenceBody:   evidence,
		EvidenceSHA256: evidenceSHA[:],
	}

	// 2. Discover and register MT5 server using native endpoint
	server, endpoint, err := f.Accounts.CreateDiscovered(ctx, candidate, 0, true)
	if err != nil {
		t.Fatalf("CreateDiscovered for MT5 failed: %v", err)
	}
	if server.Protocol != "mt5" || endpoint.IPAddress != "203.0.113.9" || endpoint.Port != 2085 {
		t.Fatalf("unexpected MT5 discovered server or endpoint: server=%+v, endpoint=%+v", server, endpoint)
	}

	// 3. Register account
	cfg := accounts.AccountInput{
		ServerID:          server.ID,
		EndpointID:        endpoint.ID,
		Login:             90000001,
		Label:             "ECMarkets Native",
		Password:          "SyntheticPass123!",
		ConfirmedCurrency: "USD",
		ConfirmUnit:       true,
		SyncEnabled:       true,
	}
	account, err := f.Accounts.CreateAccount(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateAccount for native MT5 failed: %v", err)
	}

	// 4. Verify the scheduler uses the selected native endpoint.
	fetched := make(chan mt5.Config, 1)
	engine := syncer.New(f.Accounts, nil, syncer.Options{
		Interval: time.Hour,
		FetchMT5: func(ctx context.Context, input mt5.Config) (mt5.Result, error) {
			select {
			case fetched <- input:
			case <-ctx.Done():
				return mt5.Result{}, ctx.Err()
			}
			return testutil.MT5Snapshot(), nil
		},
	})
	startEngine(t, engine)

	eventually(t, func() bool {
		view, err := f.Accounts.Detail(ctx, account.ID, "")
		return err == nil && view.Account.Snapshot != nil
	})

	used := <-fetched
	if used.IPAddress != "203.0.113.9" || used.Port != 2085 || used.Login != 90000001 {
		t.Fatal("syncer did not use the reviewed native endpoint and account")
	}
}

func TestMT5ViewsHideOnlyLegacyZeroMargin(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	mt4Server, mt4Endpoint := f.Server(t, "Synthetic-MT4")
	mt4Account := f.Account(t, mt4Server, mt4Endpoint, 10001, "USD")
	mt5Server, mt5Endpoint, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{
		Protocol: "mt5", BrokerName: "Synthetic Broker", ServerName: "Synthetic-MT5", IPAddress: "203.0.113.9", Port: 2085,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.Review(ctx, mt5Endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic verification", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	}); err != nil {
		t.Fatal(err)
	}
	mt5Account := f.Account(t, mt5Server, mt5Endpoint, 10002, "USD")
	now := time.Now().UnixMilli()
	seedMT5Valuations(t, f, mt4Account, now-60_000, now)
	seedMT5Valuations(t, f, mt5Account, now-60_000, now)

	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE account_snapshots SET margin='0', margin_available=1").Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE account_snapshots SET margin='25' WHERE account_id=? AND observed_to_ms=?", mt5Account.ID, now-60_000).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE accounts SET current_snapshot_id=(
			SELECT id FROM account_snapshots WHERE account_id=accounts.id AND observed_to_ms=?
		) WHERE id IN (?, ?)`, now, mt4Account.ID, mt5Account.ID).Error
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		account   storage.Account
		available bool
	}{
		{mt4Account, true},
		{mt5Account, false},
	} {
		detail, err := f.Accounts.Detail(ctx, test.account.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		list, err := f.Accounts.Snapshots(ctx, test.account.ID, 0, 2)
		if err != nil || len(list) != 2 {
			t.Fatalf("expected two snapshots: %v", err)
		}
		for _, snapshot := range []*storage.Snapshot{detail.Account.Snapshot, &list[0]} {
			if snapshot == nil || snapshot.MarginAvailable != test.available ||
				(snapshot.Margin != nil) != test.available {
				t.Fatalf("unexpected latest margin display: %+v", snapshot)
			}
		}
		if test.account.ID == mt5Account.ID &&
			(!list[1].MarginAvailable || list[1].Margin == nil || list[1].Margin.String() != "25") {
			t.Fatal("nonzero MT5 margin was hidden")
		}
	}
}
