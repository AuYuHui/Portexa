package app_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/internal/vault"
	"portexa/mt4"
	"portexa/mt5"
	"portexa/tests/testutil"
)

func reviewedCollectionPoint(t *testing.T, f testutil.Fixture, serverID string, port int) storage.Endpoint {
	t.Helper()
	endpoint, paused, err := f.Accounts.NewEndpoint(t.Context(), serverID, accounts.EndpointInput{
		IPAddress: "203.0.113.9", Port: port, KeepExisting: true,
	})
	if err != nil || len(paused) != 0 {
		t.Fatalf("adding an endpoint disturbed existing collection: %v (%d paused)", err, len(paused))
	}
	endpoint, err = f.Accounts.Review(t.Context(), endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic independent evidence", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestCollectionFailoverPublishesOneSnapshotAndPrefersLastSuccess(t *testing.T) {
	for _, protocol := range []string{"mt4", "mt5"} {
		t.Run(protocol, func(t *testing.T) {
			f := testutil.New(t, false)
			ctx := t.Context()
			server, _, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{Protocol: protocol, BrokerName: "Synthetic Broker",
				ServerName: "Synthetic-Failover", IPAddress: "203.0.113.9", Port: 443})
			if err != nil {
				t.Fatal(err)
			}
			primary := reviewedCollectionPoint(t, f, server.ID, 443)
			account := f.Account(t, server, primary, 8100, "USD")
			second := reviewedCollectionPoint(t, f, server.ID, 444)
			third := reviewedCollectionPoint(t, f, server.ID, 445)
			backups := []string{second.ID, third.ID}
			if err := f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{BackupEndpointIDs: &backups}); err != nil {
				t.Fatal(err)
			}

			var mu sync.Mutex
			var calls []int
			try := func(ctx context.Context, port int, password string) error {
				mu.Lock()
				calls = append(calls, port)
				mu.Unlock()
				if password != testutil.InvestorPassword {
					t.Error("failover lost the authorized credential")
				}
				switch port {
				case 443:
					<-ctx.Done()
					return ctx.Err()
				case 444:
					if protocol == "mt4" {
						return &mt4.Error{Code: mt4.ConnectionFailed, Operation: "initial"}
					}
					return errors.Join(mt5.ErrProtocol, mt5.ErrConnection)
				case 445:
					return nil
				default:
					t.Error("unexpected endpoint")
					return errors.New("unexpected endpoint")
				}
			}
			engine := syncer.New(f.Accounts, func(ctx context.Context, cfg mt4.Config, options mt4.Options) (mt4.Result, error) {
				port, _ := strconv.Atoi(cfg.Address[strings.LastIndexByte(cfg.Address, ':')+1:])
				return testutil.Result("USD"), try(ctx, port, cfg.Password)
			}, syncer.Options{Interval: time.Hour, Timeout: 900 * time.Millisecond,
				FetchMT5: func(ctx context.Context, cfg mt5.Config) (mt5.Result, error) {
					return testutil.MT5Snapshot(), try(ctx, cfg.Port, cfg.Password)
				}})
			startEngine(t, engine)
			for want := int64(1); want <= 2; want++ {
				eventually(t, func() bool {
					var count int64
					f.Store.Read.Model(&storage.SyncRun{}).Where("account_id = ? AND state = 'succeeded'", account.ID).Count(&count)
					return count == want
				})
				if want == 1 {
					if err := f.Accounts.RequestSync(ctx, account.ID); err != nil {
						t.Fatal(err)
					}
					engine.Wake()
				}
			}
			mu.Lock()
			got := slices.Clone(calls)
			mu.Unlock()
			if !slices.Equal(got, []int{443, 444, 445, 445}) {
				t.Fatalf("unexpected endpoint attempts: %v", got)
			}
			var runs []storage.SyncRun
			if err := f.Store.Read.Where("account_id = ?", account.ID).Find(&runs).Error; err != nil {
				t.Fatal(err)
			}
			if len(runs) != 2 || runs[0].EndpointID != third.ID || runs[1].EndpointID != third.ID {
				t.Fatalf("run provenance did not record the successful endpoint: %+v", runs)
			}
			view, err := f.Accounts.Detail(ctx, account.ID, "")
			if err != nil || view.Account.LastCollectionRevision == nil || *view.Account.LastCollectionRevision != third.Revision ||
				view.Account.EndpointID != primary.ID || !slices.Equal(view.Account.BackupEndpointIDs, backups) {
				t.Fatalf("unexpected endpoint view: %v", err)
			}
		})
	}
}

func TestCollectionBindingRejectsUnsafeChanges(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	server, primary := f.Server(t, "Synthetic-Failover")
	_, unbound := f.Server(t, "Synthetic-Other")
	account := f.Account(t, server, primary, 8101, "USD")
	pending, _, err := f.Accounts.NewEndpoint(ctx, server.ID, accounts.EndpointInput{IPAddress: "203.0.113.9", Port: 444, KeepExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{primary.ID}, {unbound.ID}, {pending.ID}} {
		if err := f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{BackupEndpointIDs: &ids}); err == nil {
			t.Fatal("unsafe endpoint was bound to the account")
		}
	}
	backup := reviewedCollectionPoint(t, f, server.ID, 445)
	if err := f.Accounts.RequestSync(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.SwitchCollectionTarget(ctx, job.Account, &storage.SyncRun{}, job.Run.ID, backup.ID); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("credential issued to an unbound endpoint: %v", err)
	}
	ids := []string{backup.ID}
	if err := f.Accounts.UpdateAccount(ctx, account.ID, accounts.AccountPatch{BackupEndpointIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.SwitchCollectionTarget(ctx, job.Account, &storage.SyncRun{}, job.Run.ID, backup.ID); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale collection acquired credentials after account edit: %v", err)
	}
	if _, err := f.Accounts.Fail(ctx, job, accounts.Failure{Canceled: true}, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		return tx.Model(&account).Update("backup_endpoint_ids", "[]").Error
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.Claim(ctx, account.ID); !errors.Is(err, vault.ErrDecrypt) {
		t.Fatalf("credential envelope did not authenticate the endpoint set: %v", err)
	}
}

func TestCollectionFailoverStopsOnRejection(t *testing.T) {
	f := testutil.New(t, false)
	server, primary := f.Server(t, "Synthetic-Rejection")
	backup := reviewedCollectionPoint(t, f, server.ID, 444)
	a, err := f.Accounts.CreateAccount(t.Context(), accounts.AccountInput{ServerID: server.ID, EndpointID: primary.ID,
		BackupEndpointIDs: []string{backup.ID}, Login: 8102, Label: "Synthetic", Password: testutil.InvestorPassword, SyncEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := syncer.New(f.Accounts, func(_ context.Context, cfg mt4.Config, _ mt4.Options) (mt4.Result, error) {
		if !strings.HasSuffix(cfg.Address, ":443") {
			t.Error("the backup received credentials after an explicit rejection")
		}
		return mt4.Result{}, &mt4.Error{Code: mt4.ServerRejected, Operation: "initial", Status: 1}
	}, syncer.Options{Interval: time.Hour})
	startEngine(t, engine)
	var run storage.SyncRun
	eventually(t, func() bool {
		return f.Store.Read.First(&run, "account_id = ? AND state = 'failed'", a.ID).Error == nil
	})
	if run.EndpointID != primary.ID || run.ErrorCode == nil || *run.ErrorCode != "server_rejected" {
		t.Fatalf("unexpected run after server rejection: %+v", run)
	}
}

func TestCollectionFailoverForDailyAndHistoryKeepsSnapshotSource(t *testing.T) {
	now := time.Now()
	f, account := dailyFixture(t, &now, "UTC")
	backup := reviewedCollectionPoint(t, f, account.ServerID, 444)
	ids := []string{backup.ID}
	if err := f.Accounts.UpdateAccount(t.Context(), account.ID, accounts.AccountPatch{BackupEndpointIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	engine := syncer.New(f.Accounts, func(_ context.Context, cfg mt4.Config, options mt4.Options) (mt4.Result, error) {
		result := testutil.Result("USC")
		if options.History == nil {
			if !strings.HasSuffix(cfg.Address, ":443") {
				t.Error("a history connection changed the snapshot's preferred endpoint")
			}
			return result, nil
		}
		if strings.HasSuffix(cfg.Address, ":443") {
			return mt4.Result{}, &mt4.Error{Code: mt4.ConnectionFailed, Operation: "initial"}
		}
		result.HistoryRead, result.HistoryComplete = true, true
		result.HistoryCovered = []mt4.HistoryRange{*options.History}
		return result, nil
	}, syncer.Options{Interval: time.Hour})
	startEngine(t, engine)
	eventually(t, func() bool {
		var daily, history int64
		f.Store.Read.Model(&storage.DailyReport{}).Where("account_id = ? AND endpoint_id = ? AND state = 'succeeded'", account.ID, backup.ID).Count(&daily)
		f.Store.Read.Model(&storage.HistoryRun{}).Where("account_id = ? AND endpoint_id = ? AND state = 'succeeded'", account.ID, backup.ID).Count(&history)
		return daily > 0 && history > 0
	})
	var persisted storage.Account
	if err := f.Store.Read.First(&persisted, "id = ?", account.ID).Error; err != nil || persisted.LastCollectionEndpointID != account.PinnedEndpointID {
		t.Fatalf("unexpected snapshot endpoint preference: %v", err)
	}
}

func TestMT5DailyRetainsRemainingBudgetForDeals(t *testing.T) {
	f := testutil.New(t, false)
	ctx := t.Context()
	server, _, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{Protocol: "mt5", BrokerName: "Synthetic Broker",
		ServerName: "Synthetic-MT5-History-Budget", IPAddress: "203.0.113.9", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	primary := reviewedCollectionPoint(t, f, server.ID, 443)
	second := reviewedCollectionPoint(t, f, server.ID, 444)
	third := reviewedCollectionPoint(t, f, server.ID, 445)
	a, err := f.Accounts.CreateAccount(ctx, accounts.AccountInput{ServerID: server.ID, EndpointID: primary.ID,
		BackupEndpointIDs: []string{second.ID, third.ID}, Login: 8104, Label: "Synthetic", Password: testutil.InvestorPassword,
		ConfirmedCurrency: "USD", ConfirmUnit: true, SyncEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		Mode: "manual", TimeZone: "UTC", EffectiveDate: time.Now().UTC().AddDate(0, 0, -2).Format(time.DateOnly),
	}); err != nil {
		t.Fatal(err)
	}
	const budget = 1800 * time.Millisecond
	engine := syncer.New(f.Accounts, nil, syncer.Options{Interval: time.Hour, Timeout: budget,
		FetchMT5: func(context.Context, mt5.Config) (mt5.Result, error) {
			return testutil.MT5Snapshot(), nil
		},
		FetchMT5History: func(ctx context.Context, cfg mt5.Config, window mt5.HistoryRange) (mt5.HistoryResult, error) {
			if cfg.Port != primary.Port {
				t.Errorf("a slow deal request unnecessarily moved to revision with port %d", cfg.Port)
			}
			// Simulate a complete reply after the previous per-endpoint budget.
			timer := time.NewTimer(850 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				return testutil.MT5History(window, nil), nil
			case <-ctx.Done():
				return mt5.HistoryResult{}, ctx.Err()
			}
		}})
	startEngine(t, engine)
	var report storage.DailyReport
	eventually(t, func() bool {
		return f.Store.Read.First(&report, "account_id = ? AND state = 'succeeded'", a.ID).Error == nil
	})
	if report.EndpointID != primary.ID {
		t.Fatal("the completed daily report did not use the preferred endpoint")
	}
}
