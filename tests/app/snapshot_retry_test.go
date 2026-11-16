package app_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func TestSnapshotRetryPolicyKeepsHistoryBackoffIndependent(t *testing.T) {
	for _, code := range []string{"timeout", "connection", "snapshot_invalid", "invalid_response"} {
		for count := int64(0); count < 5; count++ {
			want := accounts.RetryDelay(count)
			if code == "timeout" || code == "connection" {
				want = []time.Duration{3 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute}[count]
			}
			if got := accounts.SnapshotRetryDelay(accounts.Failure{Code: code}, count); got != want {
				t.Fatalf("%s failure %d: got %s, want %s", code, count, got, want)
			}
		}
	}
	for _, failure := range []accounts.Failure{{Code: "timeout", Canceled: true}, {Code: "connection", Permanent: true}} {
		if accounts.SnapshotRetryDelay(failure, 0) != time.Minute {
			t.Fatal("canceled or permanent failure received quick retry")
		}
	}
	for count, want := range []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		if accounts.RetryDelay(int64(count)) != want {
			t.Fatal("history backoff changed")
		}
	}
}

func TestSnapshotQuickRetryRunsAndLogsPersistedSchedule(t *testing.T) {
	for _, recover := range []bool{true, false} {
		name := "backoff"
		if recover {
			name = "recovery"
		}
		t.Run(name, func(t *testing.T) {
			f := testutil.New(t, false)
			server, endpoint := f.Server(t, "Synthetic-Quick-Retry")
			a := f.Account(t, server, endpoint, 8811, "USD")
			var output bytes.Buffer
			var calls atomic.Int32
			second := make(chan time.Time, 1)
			fetch := func(context.Context, mt4.Config, mt4.Options) (mt4.Result, error) {
				if calls.Add(1) == 2 {
					second <- time.Now()
					if recover {
						return testutil.Result("USD"), nil
					}
				}
				return mt4.Result{}, &mt4.Error{Code: mt4.Timeout, Operation: "connect"}
			}
			e := syncer.New(f.Accounts, fetch, syncer.Options{Interval: time.Hour, Logger: slog.New(slog.NewJSONHandler(&output, nil))})
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- e.Run(ctx) }()
			stopped := false
			t.Cleanup(func() {
				cancel()
				if !stopped {
					<-done
				}
			})
			var failed storage.SyncRun
			var firstRetryAt int64
			eventually(t, func() bool {
				var current storage.Account
				if f.Store.Read.First(&current, "id=?", a.ID).Error != nil || current.FailureCount != 1 {
					return false
				}
				firstRetryAt = current.NextSyncAtMS
				return f.Store.Read.First(&failed, "account_id=? AND state='failed'", a.ID).Error == nil
			})
			if delta := firstRetryAt - *failed.FinishedAtMS; delta < 2800 || delta > 3000 {
				t.Fatal("first retry was not scheduled three seconds after failure", delta)
			}
			select {
			case started := <-second:
				if started.UnixMilli() < firstRetryAt || started.UnixMilli()-firstRetryAt > 2500 {
					t.Fatal("quick retry missed its persisted schedule", started, firstRetryAt)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("quick retry did not run automatically")
			}
			var current storage.Account
			eventually(t, func() bool {
				if f.Store.Read.First(&current, "id=?", a.ID).Error != nil {
					return false
				}
				if recover {
					return current.FailureCount == 0 && current.CurrentSnapshotID != nil
				}
				return current.FailureCount == 2
			})
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			stopped = true
			records := decodeLogs(t, output.Bytes())
			if len(records) < 5 || records[2]["msg"] != "sync_retry_scheduled" || records[2]["retry_delay_ms"] != float64(3000) || records[2]["next_retry_at_ms"] != float64(firstRetryAt) {
				t.Fatal("quick retry log disagrees with durable schedule", records)
			}
			if records[3]["msg"] != "sync_started" || records[3]["attempt"] != float64(2) || records[3]["due_at_ms"] != float64(firstRetryAt) {
				t.Fatal("retry start lacks attempt and due time", records)
			}
			if recover {
				if len(records) != 5 || records[4]["msg"] != "sync_completed" || records[4]["recovered"] != true || records[4]["previous_failure_count"] != float64(1) {
					t.Fatal("missing recovery log", records)
				}
			} else if len(records) != 6 || records[5]["msg"] != "sync_retry_scheduled" || records[5]["retry_delay_ms"] != float64(60000) || records[5]["next_retry_at_ms"] != float64(current.NextSyncAtMS) {
				t.Fatal("repeated failure did not return to logged one-minute backoff", records)
			}
			if calls.Load() != 2 {
				t.Fatal("unexpected extra retry", calls.Load())
			}
		})
	}
}

func TestSnapshotFailureReportsNoRetryWhenStopped(t *testing.T) {
	for _, reason := range []string{"permanent", "canceled", "configuration_changed"} {
		t.Run(reason, func(t *testing.T) {
			f := testutil.New(t, false)
			server, endpoint := f.Server(t, "Synthetic-No-Retry")
			a := f.Account(t, server, endpoint, 8812, "USD")
			job, err := f.Accounts.Claim(t.Context(), a.ID)
			if err != nil {
				t.Fatal(err)
			}
			failure := accounts.Failure{Code: "timeout", Operation: "connect", Permanent: reason == "permanent", Canceled: reason == "canceled"}
			if reason == "configuration_changed" {
				enabled := false
				if err := f.Accounts.UpdateAccount(t.Context(), a.ID, accounts.AccountPatch{SyncEnabled: &enabled}); err != nil {
					t.Fatal(err)
				}
			}
			if scheduled, err := f.Accounts.Fail(t.Context(), job, failure, time.Now().Add(3*time.Second).UnixMilli()); err != nil || scheduled {
				t.Fatal("stopped task reported a scheduled retry", scheduled, err)
			}
		})
	}
}
