package main

import (
	"context"
	"time"

	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

// An isolated historical weekend, with immutable bracketing snapshot evidence.
// The fixture uses no transport and keeps the real HTTP/auth/UI paths.
func seedMarketRest(ctx context.Context, s *accounts.Service) error {
	var a storage.Account
	if err := s.Store.Read.Where("currency='USD'").Order("login").First(&a).Error; err != nil {
		return err
	}
	var server storage.Server
	if err := s.Store.Read.First(&server, "id=?", a.ServerID).Error; err != nil {
		return err
	}
	zone, _ := time.LoadLocation("America/New_York")
	local := time.Now().In(zone)
	days := (int(local.Weekday()) - int(time.Saturday) + 7) % 7
	saturday := local.AddDate(0, 0, -days)
	start := time.Date(saturday.Year(), saturday.Month(), saturday.Day(), 1, 0, 0, 0, zone).UTC()
	if start.Add(3 * time.Hour).After(time.Now()) {
		start = start.AddDate(0, 0, -7)
	}
	now := start.Add(-time.Hour)
	s.Now = func() time.Time { return now }
	defer func() { s.Now = time.Now }()
	previous := ""
	if server.ActiveTimeRulesetID != nil {
		previous = *server.ActiveTimeRulesetID
	}
	if _, err := s.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{PreviousRulesetID: previous, TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-rest-calendar"}); err != nil {
		return err
	}
	if err := s.Store.Read.First(&server, "id=?", server.ID).Error; err != nil {
		return err
	}
	off, on := marketrest.Off, marketrest.Template
	for _, template := range []*string{&off, &on} {
		if _, err := s.UpdateCollectionSettings(ctx, accounts.CollectionInput{SnapshotIntervalSeconds: 60, MarketRestTemplate: template}); err != nil {
			return err
		}
	}
	if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		if err := tx.Where("account_id=?", a.ID).Delete(&storage.Position{}).Error; err != nil {
			return err
		}
		return tx.Model(&a).Updates(map[string]any{"label": "Market rest synthetic", "current_snapshot_id": nil, "included_at_ms": nil, "created_at_ms": now.UnixMilli(), "sync_enabled": true, "rest_blocked_until_ms": 0}).Error
	}); err != nil {
		return err
	}
	for i := 0; i <= 25; i++ {
		at := start
		if i > 0 {
			at = start.Add(time.Minute + time.Duration(i-1)*marketrest.Interval)
		}
		now = at
		if err := s.RequestSync(ctx, a.ID); err != nil {
			return err
		}
		job, err := s.Claim(ctx, a.ID)
		if err != nil {
			return err
		}
		result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: "USD", Balance: 1000, Equity: 1010},
			Trades: []mt4.Trade{{Ticket: 78401, Command: 0, Symbol: "XAUUSD.c", VolumeHundredths: 10, Profit: 10, OpenPrice: 1, ClosePrice: 1}}}
		snapshot, positions, err := syncer.Convert(job, result, at.UnixMilli()-500, at.UnixMilli())
		if err != nil {
			return err
		}
		if err = s.PublishScheduled(ctx, job, snapshot, positions); err != nil {
			return err
		}
	}
	now = now.Add(time.Minute)
	coverage := storage.HistoryCoverage{ID: storage.NewID(), AccountID: a.ID, UnitRevision: a.UnitRevision, TimeRulesetID: *server.ActiveTimeRulesetID,
		StartWall: uint32(start.Add(-time.Hour).Unix()), EndWall: uint32(now.Add(time.Hour).Unix()), State: "complete", CheckedAtMS: new(now.UnixMilli())}
	run := storage.HistoryRun{ID: storage.NewID(), AccountID: a.ID, ServerID: a.ServerID, EndpointID: a.PinnedEndpointID, CoverageID: coverage.ID,
		ConfigVersion: a.ConfigVersion, UnitRevision: a.UnitRevision, NativeCurrency: "USD", State: "succeeded", StartedAtMS: now.UnixMilli(), FinishedAtMS: new(now.UnixMilli())}
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		if err := tx.Create(&coverage).Error; err != nil {
			return err
		}
		return tx.Create(&run).Error
	})
}
