package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/accounts"
	"portexa/internal/storage"
)

// Seed complete, reconciled synthetic evidence. No production transport is used.
func seedAnalytics(ctx context.Context, s *accounts.Service) error {
	now := time.Now()
	end := now.Truncate(time.Minute).Add(-time.Minute)
	from := end.Add(-31 * 24 * time.Hour)
	var list []storage.Account
	if err := s.Store.Read.Order("login").Find(&list).Error; err != nil {
		return err
	}
	var server storage.Server
	if err := s.Store.Read.First(&server, "id=?", list[0].ServerID).Error; err != nil {
		return err
	}
	previous := ""
	if server.ActiveTimeRulesetID != nil {
		previous = *server.ActiveTimeRulesetID
	}
	s.Now = func() time.Time { return from.Add(-time.Hour) }
	_, err := s.ConfirmServerTime(ctx, server.ID, accounts.ServerTimeInput{PreviousRulesetID: previous, TimeZone: "UTC", Confirm: true, EvidenceRef: "synthetic-analytics-calendar"})
	s.Now = time.Now
	if err != nil {
		return err
	}
	if err := s.Store.Read.First(&server, "id=?", server.ID).Error; err != nil {
		return err
	}
	for index, account := range list {
		if index == 2 || index == 4 {
			continue
		}
		if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			if err := tx.Where("account_id=?", account.ID).Delete(&storage.Position{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&account).Updates(map[string]any{"current_snapshot_id": nil, "created_at_ms": from.Add(-time.Hour).UnixMilli(), "included_at_ms": from.Add(-time.Minute).UnixMilli(), "sync_enabled": false}).Error; err != nil {
				return err
			}
			return tx.Where("account_id=?", account.ID).Delete(&storage.Snapshot{}).Error
		}); err != nil {
			return err
		}
		scale := decimal.NewFromInt(1)
		if *account.Currency == "USC" {
			scale = decimal.NewFromInt(100)
		}
		base := decimal.NewFromInt([]int64{12000, 8000, 0, 6000}[index])
		coverage := storage.HistoryCoverage{ID: storage.NewID(), AccountID: account.ID, UnitRevision: account.UnitRevision, TimeRulesetID: *server.ActiveTimeRulesetID,
			StartWall: uint32(from.Add(-time.Hour).Unix()), EndWall: uint32(now.Add(24 * time.Hour).Unix()), State: "complete", CheckedAtMS: new(now.UnixMilli())}
		run := storage.HistoryRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID, EndpointID: account.PinnedEndpointID, CoverageID: coverage.ID,
			ConfigVersion: account.ConfigVersion, UnitRevision: account.UnitRevision, NativeCurrency: *account.Currency, State: "succeeded", StartedAtMS: now.UnixMilli(), FinishedAtMS: new(now.UnixMilli())}
		orders := []storage.HistoryOrder{}
		for at, n := from.Add(30*time.Second), 0; at.Before(end); at, n = at.Add(6*time.Hour), n+1 {
			cents := int64(2400 + (n%9)*300)
			if n%7 == 3 {
				cents = -int64(4300 + (n%5)*700)
			}
			if index == 1 {
				cents = cents * 2
			}
			if index == 3 {
				cents = 0
			}
			net := decimal.NewFromInt(cents).Div(decimal.NewFromInt(100)).Mul(scale)
			fee := decimal.RequireFromString("-0.50").Mul(scale)
			profit := net.Sub(fee)
			hash := sha256.Sum256([]byte(fmt.Sprintf("synthetic:%d:%d:%s", index, n, net)))
			orders = append(orders, storage.HistoryOrder{AccountID: account.ID, Ticket: int32(700000 + index*10000 + n), Revision: 1, SourceRunID: run.ID, SourceHash: hash[:], UnitRevision: account.UnitRevision,
				NativeCurrency: *account.Currency, Command: 0, Symbol: "EURUSD", OpenTimeWall: uint32(at.Add(-time.Hour).Unix()), CloseTimeWall: uint32(at.Unix()), EventTimeWall: uint32(at.Unix()),
				Profit: profit, Commission: fee, NetProfit: net, VolumeHundredths: 10, Comment: "Synthetic closed trade"})
		}
		if index == 0 {
			for j, event := range []struct {
				at            time.Time
				amount        int64
				kind, comment string
			}{{from.Add(8*24*time.Hour + 30*time.Second), 1000, "deposit", "Deposit"}, {from.Add(21*24*time.Hour + 30*time.Second), -500, "withdrawal", "Withdrawal"}} {
				amount := decimal.NewFromInt(event.amount)
				hash := sha256.Sum256([]byte(fmt.Sprintf("synthetic-cash:%d", j)))
				orders = append(orders, storage.HistoryOrder{AccountID: account.ID, Ticket: int32(799000 + j), Revision: 1, SourceRunID: run.ID, SourceHash: hash[:], UnitRevision: account.UnitRevision,
					NativeCurrency: *account.Currency, Command: 6, OpenTimeWall: uint32(event.at.Unix()), EventTimeWall: uint32(event.at.Unix()), Profit: amount, NetProfit: amount,
					Comment: event.comment, AutomaticCategory: event.kind, AutomaticRuleVersion: accounts.FundingRuleVersion, AutomaticReason: "synthetic explicit cash flow"})
			}
		}
		if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			if err := tx.Create(&coverage).Error; err != nil {
				return err
			}
			if err := tx.Create(&run).Error; err != nil {
				return err
			}
			if err := tx.CreateInBatches(&orders, 100).Error; err != nil {
				return err
			}
			heads := make([]storage.HistoryOrderHead, 0, len(orders))
			for _, order := range orders {
				heads = append(heads, storage.HistoryOrderHead{AccountID: account.ID, Ticket: order.Ticket, Revision: 1})
			}
			return tx.CreateInBatches(&heads, 100).Error
		}); err != nil {
			return err
		}
		sort.Slice(orders, func(i, j int) bool { return orders[i].EventTimeWall < orders[j].EventTimeWall })
		lastID := ""
		runningBalance, nextOrder := base.Mul(scale), 0
		for start := from; !start.After(end); {
			samples := []storage.Snapshot{}
			runs := []storage.SyncRun{}
			for len(samples) < 500 && !start.After(end) {
				at := start
				start = start.Add(time.Minute)
				for nextOrder < len(orders) && int64(orders[nextOrder].EventTimeWall)*1000 < at.UnixMilli() {
					runningBalance = runningBalance.Add(orders[nextOrder].NetProfit)
					nextOrder++
				}
				balance := runningBalance
				hours := at.Sub(from).Hours()
				floating := math.Sin(hours/19)*72 + math.Cos(hours/3)*16
				if index == 1 {
					floating = math.Sin(hours/27)*430 - math.Pow(math.Max(0, math.Sin(hours/61)), 8)*900
				}
				if index == 3 {
					floating = 0
				}
				floatingAmount := decimal.NewFromFloat(floating).Round(2).Mul(scale)
				stamp := at.Add(time.Duration(-5+index) * time.Second)
				cycle := 60
				run := storage.SyncRun{ID: storage.NewID(), AccountID: account.ID, ServerID: account.ServerID, EndpointID: account.PinnedEndpointID,
					CredentialVersion: account.CredentialVersion, ConfigVersion: account.ConfigVersion, Kind: "snapshot", State: "succeeded", Attempt: 1}
				snapshot := storage.Snapshot{ID: storage.NewID(), AccountID: account.ID, SyncRunID: run.ID, ObservedFromMS: stamp.UnixMilli() - 250, ObservedToMS: stamp.UnixMilli(), NativeCurrency: *account.Currency,
					USDScale: account.USDScale, UnitRevision: account.UnitRevision, Balance: balance, Equity: balance.Add(floatingAmount), FloatingNetProfit: floatingAmount, CollectionIntervalSeconds: &cycle}
				runs = append(runs, run)
				samples = append(samples, snapshot)
				lastID = snapshot.ID
			}
			if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
				if err := tx.CreateInBatches(&runs, 100).Error; err != nil {
					return err
				}
				return tx.CreateInBatches(&samples, 100).Error
			}); err != nil {
				return err
			}
		}
		if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			return tx.Model(&account).Updates(map[string]any{"current_snapshot_id": lastID, "initial_statistical_equity": base.Mul(scale)}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
