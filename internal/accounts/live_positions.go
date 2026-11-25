package accounts

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/storage"
)

type PositionFilter struct {
	Symbol   string
	Side     string
	LossOnly bool
}

type LivePositions struct {
	AccountID         string           `json:"account_id"`
	Protocol          string           `json:"protocol"`
	Currency          string           `json:"currency"`
	ObservedToMS      *int64           `json:"observed_to_ms"`
	SyncEnabled       bool             `json:"sync_enabled"`
	SyncState         string           `json:"sync_state"`
	Stale             bool             `json:"stale"`
	MarketRest        bool             `json:"market_rest"`
	TotalCount        *int             `json:"total_count"`
	FloatingNetProfit *decimal.Decimal `json:"floating_net_profit"`
	FilteredNetProfit *decimal.Decimal `json:"filtered_net_profit"`
	Symbols           []string         `json:"symbols"`
	Items             []PositionView   `json:"items"`
}

func (s *Service) LivePositions(ctx context.Context, id string, filter PositionFilter, watching bool) (LivePositions, error) {
	if (filter.Side != "" && filter.Side != "buy" && filter.Side != "sell") || len(filter.Symbol) > 128 {
		return LivePositions{}, &InputError{"不支持此查询参数"}
	}
	// Live polling needs the published snapshot and its positions from one read transaction.
	detail, err := s.readDetail(ctx, id, "", false)
	if err != nil {
		return LivePositions{}, err
	}
	account := detail.Account
	view := LivePositions{AccountID: account.ID, Protocol: account.Protocol, SyncEnabled: account.SyncEnabled,
		SyncState: account.SyncState, Stale: account.Stale, MarketRest: account.MarketRest,
		Items: []PositionView{}, Symbols: []string{}}
	if account.Snapshot == nil {
		return view, nil
	}
	snapshot := account.Snapshot
	if watching {
		queued, err := s.scheduleWatchedSnapshot(ctx, account)
		if err != nil {
			return LivePositions{}, err
		}
		if queued {
			view.SyncState = "queued"
		}
	}
	view.Currency = snapshot.NativeCurrency
	view.ObservedToMS = &snapshot.ObservedToMS
	view.FloatingNetProfit = &snapshot.FloatingNetProfit

	total, subtotal := 0, decimal.Zero
	symbols := make(map[string]bool)
	for _, position := range detail.Positions {
		if position.Command != 0 && position.Command != 1 {
			continue
		}
		total++
		symbols[position.Symbol] = true
		if filter.Symbol != "" && filter.Symbol != position.Symbol ||
			filter.Side == "buy" && position.Command != 0 || filter.Side == "sell" && position.Command != 1 ||
			filter.LossOnly && !position.NetProfit.IsNegative() {
			continue
		}
		subtotal = subtotal.Add(position.NetProfit)
		view.Items = append(view.Items, position)
	}
	view.TotalCount, view.FilteredNetProfit = &total, &subtotal
	for symbol := range symbols {
		view.Symbols = append(view.Symbols, symbol)
	}
	sort.Strings(view.Symbols)
	sort.Slice(view.Items, func(i, j int) bool {
		a, b := view.Items[i], view.Items[j]
		if cmp := a.NetProfit.Cmp(b.NetProfit); cmp != 0 {
			return cmp < 0
		}
		return a.Ticket < b.Ticket
	})
	return view, nil
}

func (s *Service) scheduleWatchedSnapshot(ctx context.Context, account AccountView) (bool, error) {
	now := s.Now()
	const cadence = 10 * time.Second
	next := max(now.UnixMilli(), account.Snapshot.ObservedToMS+cadence.Milliseconds())
	boundary, _ := dailyBoundaryWindow(now)
	if account.SyncState != "succeeded" || !account.EndpointApproved || account.MarketRest ||
		next >= account.NextSyncAtMS || next >= boundary.UnixMilli() {
		return false, nil
	}
	var queued bool
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var current storage.Account
		err := tx.Where("id = ? AND config_version = ? AND removed_at_ms IS NULL AND sync_enabled = 1 AND failure_count = 0", account.ID, account.ConfigVersion).
			Where("current_snapshot_id = ? AND next_sync_at_ms = ?", account.Snapshot.ID, account.NextSyncAtMS).
			Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		// Keep the deadline promised to an in-flight history, daily or calibration fetch.
		busy, err := accountFetching(tx, current.ID)
		if err != nil || busy {
			return err
		}
		// A shorter gap can make a due job ineligible even before it is claimed.
		reserved, err := backgroundCollectionDue(tx, current, now.UnixMilli(), next)
		if err != nil || reserved {
			return err
		}
		if err := tx.Model(&current).Update("next_sync_at_ms", next).Error; err != nil {
			return err
		}
		queued = next <= now.UnixMilli()
		return nil
	})
	return queued, err
}

func backgroundCollectionDue(tx *gorm.DB, account storage.Account, now, next int64) (bool, error) {
	if !account.DailySyncPaused && account.NextDailySyncAtMS <= next ||
		!account.HistorySyncPaused && account.NextHistorySyncAtMS <= next {
		var count int64
		err := tx.Model(&storage.Server{}).
			Where("id = ? AND retired_at_ms IS NULL AND active_time_ruleset_id IS NOT NULL", account.ServerID).
			Count(&count).Error
		if err != nil || count > 0 {
			return count > 0, err
		}
	}
	if account.ClockSyncPaused || account.NextClockSyncAtMS > next {
		return false, nil
	}
	var count int64
	err := pendingClock(tx.Model(&storage.ClockObservation{}), account, now).Count(&count).Error
	return count > 0, err
}
