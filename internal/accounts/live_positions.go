package accounts

import (
	"context"
	"sort"

	"github.com/shopspring/decimal"
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

func (s *Service) LivePositions(ctx context.Context, id string, filter PositionFilter) (LivePositions, error) {
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
