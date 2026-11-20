package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/tests/testutil"
)

func publishExcursionSnapshot(t *testing.T, f testutil.Fixture, account storage.Account, stamp time.Time, positions []storage.Position) {
	t.Helper()
	ctx := t.Context()
	if err := f.Accounts.RequestSync(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := storage.Snapshot{
		ID: storage.NewID(), AccountID: account.ID, SyncRunID: job.Run.ID,
		ObservedFromMS: stamp.UnixMilli(), ObservedToMS: stamp.UnixMilli(),
		NativeCurrency: *account.Currency, UnitRevision: account.UnitRevision,
	}
	for i := range positions {
		positions[i].SnapshotID, positions[i].AccountID = snapshot.ID, account.ID
	}
	if err := f.Accounts.Publish(ctx, job, snapshot, positions, stamp.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func excursionReport(t *testing.T, f testutil.Fixture, account storage.Account, now time.Time, exits []accounts.DailyPositionExit) *storage.DailyPositionExcursions {
	t.Helper()
	ctx := t.Context()
	if err := f.Accounts.RequestDaily(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.ClaimDaily(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := dailyResult(job)
	result.Account.Currency = job.NativeCurrency
	report, err := syncer.ConvertDaily(job, result, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	job.PositionExits = exits
	if err := f.Accounts.PublishDaily(ctx, job, report, now.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	view, err := f.Accounts.Detail(ctx, account.ID, "")
	if err != nil || view.Account.Daily.Report == nil || view.Account.Daily.Report.PositionExcursions == nil {
		t.Fatalf("daily excursion report missing: %v", err)
	}
	return view.Account.Daily.Report.PositionExcursions
}

func TestMT4DailyPositionExcursionsUseCrossDaySamples(t *testing.T) {
	now := time.Date(2026, 11, 11, 23, 50, 0, 0, time.UTC)
	f, account := dailyFixture(t, &now, "UTC")
	open := uint32(now.Add(-time.Minute).Unix())
	position := storage.Position{Ticket: 3101, Command: 0, Symbol: "SYNTH", OpenTimeWall: open,
		Profit: decimal.NewFromInt(-119), Commission: decimal.NewFromInt(-1)}
	now = now.Add(time.Minute)
	first := now
	publishExcursionSnapshot(t, f, account, now, []storage.Position{position})
	now = now.Add(15 * time.Minute)
	second := now
	position.Profit = decimal.NewFromInt(81)
	publishExcursionSnapshot(t, f, account, now, []storage.Position{position})
	now = now.Add(time.Minute)
	exit := accounts.DailyPositionExit{Identifier: 3101, Ticket: 3101, Symbol: "SYNTH", Side: 0,
		OpenTimeWall: open, CloseWallMS: now.UnixMilli()}
	missing := accounts.DailyPositionExit{Identifier: 3102, Ticket: 3102, Symbol: "SYNTH", Side: 0,
		OpenTimeWall: uint32(now.Unix()), CloseWallMS: now.UnixMilli()}
	now = now.Add(time.Minute)
	publishExcursionSnapshot(t, f, account, now, nil)
	peaks := excursionReport(t, f, account, now, []accounts.DailyPositionExit{exit, missing})
	if peaks.Closed != 2 || peaks.Sampled != 1 ||
		peaks.Loss == nil || peaks.Loss.Amount.String() != "-120" || peaks.Loss.Ticket != "3101" || peaks.Loss.AtMS != first.UnixMilli() ||
		peaks.Profit == nil || peaks.Profit.Amount.String() != "80" || peaks.Profit.AtMS != second.UnixMilli() {
		t.Fatalf("unexpected MT4 peaks: %+v", peaks)
	}
}

func TestMT5DailyPositionExcursionsSeparatePartialClosesAndReversals(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	f := testutil.New(t, false)
	f.Accounts.Now = func() time.Time { return now }
	ctx := t.Context()
	server, endpoint, err := f.Accounts.CreateServer(ctx, accounts.ServerInput{
		Protocol: "mt5", BrokerName: "Synthetic Broker", ServerName: "Synthetic-MT5-Peaks",
		IPAddress: "203.0.113.9", Port: 2085,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.Review(ctx, endpoint.ID, accounts.ReviewInput{
		EvidenceRef: "synthetic verification", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Accounts.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		Mode: "manual", TimeZone: "UTC", EffectiveDate: now.Format(time.DateOnly),
	}); err != nil {
		t.Fatal(err)
	}
	account := f.Account(t, server, endpoint, 3199, "USD")
	buy := storage.Position{Ticket: 4101, MT5Identifier: 4101, MT5NativeTicket: 5101, Command: 0,
		Symbol: "SYNTH", OpenTimeWall: uint32(now.Unix()), Profit: decimal.NewFromInt(-25)}
	publishExcursionSnapshot(t, f, account, now, []storage.Position{buy})
	now = now.Add(time.Minute)
	partial := accounts.DailyPositionExit{Identifier: 4101, Ticket: 6101, Symbol: "SYNTH", Side: 0, CloseWallMS: now.UnixMilli()}
	now = now.Add(time.Minute)
	buy.Profit = decimal.NewFromInt(7)
	publishExcursionSnapshot(t, f, account, now, []storage.Position{buy})
	if peaks := excursionReport(t, f, account, now, []accounts.DailyPositionExit{partial}); peaks.Closed != 0 {
		t.Fatalf("a partially closed position was included: %+v", peaks)
	}
	now = now.Add(time.Minute)
	reverse := accounts.DailyPositionExit{Identifier: 4101, Ticket: 6102, Symbol: "SYNTH", Side: 0, CloseWallMS: now.UnixMilli(), Reversal: true}
	sell := storage.Position{Ticket: 4101, MT5Identifier: 4101, MT5NativeTicket: 5102, Command: 1,
		Symbol: "SYNTH", OpenTimeWall: uint32(now.Unix()), Profit: decimal.NewFromInt(-90)}
	now = now.Add(time.Minute)
	publishExcursionSnapshot(t, f, account, now, []storage.Position{sell})
	peaks := excursionReport(t, f, account, now, []accounts.DailyPositionExit{reverse, partial})
	if peaks.Closed != 1 || peaks.Sampled != 1 || peaks.Loss == nil || peaks.Profit == nil ||
		peaks.Loss.Ticket != "5101" || peaks.Loss.Amount.String() != "-25" || peaks.Profit.Amount.String() != "7" {
		t.Fatalf("MT5 reversal mixed the old and new sides: %+v", peaks)
	}
	now = now.Add(time.Minute)
	closeSell := accounts.DailyPositionExit{Identifier: 4101, Ticket: 6103, Symbol: "SYNTH", Side: 1, CloseWallMS: now.UnixMilli()}
	now = now.Add(time.Minute)
	publishExcursionSnapshot(t, f, account, now, nil)
	peaks = excursionReport(t, f, account, now, []accounts.DailyPositionExit{partial, reverse, closeSell})
	if peaks.Closed != 2 || peaks.Sampled != 2 || peaks.Loss == nil ||
		peaks.Loss.Ticket != "5102" || peaks.Loss.Amount.String() != "-90" {
		t.Fatalf("MT5 did not capture both completed sides: %+v", peaks)
	}
}
