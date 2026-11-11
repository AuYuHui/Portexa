package main

import (
	"context"
	"math"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func seedAssetTrends(ctx context.Context, service *accounts.Service) error {
	now := time.Now()
	from := now.Add(-8 * 24 * time.Hour).Truncate(time.Minute)
	var list []storage.Account
	if err := service.Store.Read.Order("login").Find(&list).Error; err != nil {
		return err
	}
	for index, account := range list {
		if index == 2 || index == 4 {
			continue
		}
		included := from.UnixMilli()
		if index == 3 {
			included = now.Add(-2 * 24 * time.Hour).Truncate(time.Minute).UnixMilli()
		}
		for start := from; start.Before(now.Add(-time.Minute)); {
			var runs []storage.SyncRun
			var samples []storage.Snapshot
			for len(samples) < 100 && start.Before(now.Add(-time.Minute)) {
				stamp := start
				start = start.Add(time.Minute)
				if stamp.UnixMilli() < included || stamp.After(now.Add(-3*time.Hour)) && stamp.Before(now.Add(-150*time.Minute)) {
					continue
				}
				ratio := stamp.Sub(from).Hours()
				balance := 12450.5 + ratio*1.2
				if index == 1 {
					balance = 824500 + ratio*90
				}
				if index == 3 {
					balance = 6200
				}
				floating := math.Sin(ratio/3.5)*40 + math.Cos(ratio/0.8)*12
				if index == 1 {
					floating *= 100
				}
				stamp = stamp.Add(time.Duration(index) * time.Second)
				run := storage.SyncRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID,
					EndpointID: account.PinnedEndpointID, CredentialVersion: account.CredentialVersion,
					ConfigVersion: account.ConfigVersion, Kind: "snapshot", State: "succeeded", Attempt: 1}
				cycle := 60
				sample := storage.Snapshot{ID: storage.NewID(), AccountID: account.ID, SyncRunID: run.ID,
					ObservedFromMS: stamp.Add(-500 * time.Millisecond).UnixMilli(), ObservedToMS: stamp.UnixMilli(),
					NativeCurrency: *account.Currency, USDScale: account.USDScale, UnitRevision: account.UnitRevision,
					Balance: decimal.NewFromFloat(balance), Equity: decimal.NewFromFloat(balance + floating),
					FloatingNetProfit: decimal.NewFromFloat(floating), CollectionIntervalSeconds: &cycle}
				runs, samples = append(runs, run), append(samples, sample)
			}
			if len(samples) == 0 {
				break
			}
			if err := service.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
				if err := tx.Create(&runs).Error; err != nil {
					return err
				}
				return tx.Create(&samples).Error
			}); err != nil {
				return err
			}
		}
		if err := service.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			if err := tx.Model(&account).Updates(map[string]any{"created_at_ms": from.UnixMilli(), "included_at_ms": included}).Error; err != nil {
				return err
			}
			return tx.Model(&storage.AccountUnitChange{}).Where("account_id=?", account.ID).Update("effective_at_ms", from.UnixMilli()).Error
		}); err != nil {
			return err
		}
	}
	return seedTrendCashFlow(ctx, service, list[0], from, now)
}

func seedTrendCashFlow(ctx context.Context, service *accounts.Service, account storage.Account, from, now time.Time) error {
	var server storage.Server
	if err := service.Store.Read.First(&server, "id=?", account.ServerID).Error; err != nil {
		return err
	}
	previous := ""
	if server.ActiveTimeRulesetID != nil {
		previous = *server.ActiveTimeRulesetID
	}
	service.Now = func() time.Time { return from }
	_, err := service.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{
		PreviousRulesetID: previous, TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-trend-calendar",
	})
	service.Now = time.Now
	if err != nil {
		return err
	}
	job, err := service.ClaimHistory(ctx, account.ID)
	if err != nil {
		return err
	}
	stamp := max(job.Coverage.StartWall+1, uint32(now.Add(-20*time.Minute).Unix()))
	result := testutilTrendHistory(job, []mt4.Trade{
		{Ticket: 990001, Command: 6, OpenTime: stamp, Profit: 100, Comment: "Deposit"},
		{Ticket: 990002, Command: 6, OpenTime: stamp + 1, Profit: -20, Comment: "Withdrawal"},
		{Ticket: 990003, Command: 6, OpenTime: stamp + 2, Profit: 7, Comment: "Unknown"},
	})
	orders, err := syncer.ConvertHistory(job, result)
	if err != nil {
		return err
	}
	return service.PublishHistory(ctx, job, orders, time.Now().UnixMilli())
}

func testutilTrendHistory(job accounts.HistoryJob, trades []mt4.Trade) mt4.Result {
	return mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: job.Run.NativeCurrency},
		HistoryRead: true, HistoryComplete: true, HistoryCovered: []mt4.HistoryRange{{Start: job.Coverage.StartWall, End: job.Coverage.EndWall}},
		History: trades}
}
