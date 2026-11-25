package accounts

import (
	"context"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/analytics"
	"portexa/internal/storage"
	"portexa/mt5"
)

const OrderHistoryPageSize = 50

type OrderHistoryFilter struct {
	From, To, Symbol, Side, Result string
	Ticket                         int64
	Page                           int
}

type ClosedOrder struct {
	analytics.TradeDetails
	Symbol          string           `json:"symbol"`
	CloseTimeMS     *int64           `json:"close_time_ms"`
	CloseTimeWallMS int64            `json:"close_time_wall_ms"`
	OpenTimeWall    uint32           `json:"open_time_wall"`
	Digits          int32            `json:"digits"`
	OpenPrice       *decimal.Decimal `json:"open_price"`
	ClosePrice      decimal.Decimal  `json:"close_price"`
	Profit          decimal.Decimal  `json:"profit"`
	Commission      decimal.Decimal  `json:"commission"`
	Swap            decimal.Decimal  `json:"swap"`
	Taxes           decimal.Decimal  `json:"taxes"`
	Fee             *decimal.Decimal `json:"fee"`
	NetProfit       decimal.Decimal  `json:"net_profit"`
}

type OrderHistoryPage struct {
	Items          []ClosedOrder    `json:"items"`
	Total          int              `json:"total"`
	Page           int              `json:"page"`
	PageSize       int              `json:"page_size"`
	Currency       string           `json:"currency"`
	Complete       bool             `json:"complete"`
	NetProfit      *decimal.Decimal `json:"net_profit"`
	KnownNetProfit decimal.Decimal  `json:"known_net_profit"`
	AsOfMS         *int64           `json:"as_of_ms"`
}

func (s *Service) OrderHistory(ctx context.Context, id string, f OrderHistoryFilter) (OrderHistoryPage, error) {
	page := OrderHistoryPage{Page: 1, PageSize: OrderHistoryPageSize, Items: []ClosedOrder{}}
	now := s.Now()
	period, err := orderHistoryPeriod(f, now)
	if err != nil {
		return page, err
	}
	err = s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.Where("id=? AND removed_at_ms IS NULL", id).First(&account).Error; err != nil {
			return err
		}
		if account.Currency == nil {
			return nil
		}
		page.Currency = *account.Currency
		var server storage.Server
		if err := tx.First(&server, "id=?", account.ServerID).Error; err != nil {
			return err
		}
		clock := orderHistoryClock{tx: tx, server: server, blocks: make(map[int64][]timeRuleEntry)}
		complete, asOf, err := orderHistoryCoverage(&clock, account, period, now)
		if err != nil {
			return err
		}
		page.AsOfMS = asOf
		page.Complete = complete && account.UnitVerifiedAtMS != nil
		query := closedOrderQuery(tx, account, server.Protocol, f)
		if period.dated() {
			bounds, err := period.wallBounds(&clock)
			if err != nil {
				return err
			}
			query = query.Where("o.event_time_wall>=? AND o.event_time_wall<?", bounds[0]/1000, (bounds[1]+999)/1000)
		}
		keys, err := matchingClosedOrders(query, &clock, period, f.Result, &page)
		if err != nil {
			return err
		}
		page.Total = len(keys)
		if page.Complete {
			page.NetProfit = &page.KnownNetProfit
		}
		if page.Total == 0 {
			return nil
		}
		page.Page = min(f.Page, 1+(page.Total-1)/OrderHistoryPageSize)
		first := (page.Page - 1) * OrderHistoryPageSize
		visible := keys[first:min(first+OrderHistoryPageSize, page.Total)]
		tickets := make([]int64, len(visible))
		for i, key := range visible {
			tickets[i] = key.ticket
		}
		var rows []storage.HistoryOrder
		if err := currentHistory(tx, id).Where("o.ticket IN ?", tickets).Select("o.*").Scan(&rows).Error; err != nil {
			return err
		}
		byTicket := make(map[int64]storage.HistoryOrder, len(rows))
		for _, row := range rows {
			byTicket[row.Ticket] = row
		}
		for _, key := range visible {
			order := byTicket[key.ticket]
			row := ClosedOrder{TradeDetails: *analyticsTradeDetails(order), Symbol: order.Symbol, OpenTimeWall: order.OpenTimeWall,
				CloseTimeWallMS: orderWallMS(order), Digits: order.Digits, ClosePrice: order.ClosePrice,
				Profit: order.Profit, Commission: order.Commission, Swap: order.Swap, Taxes: order.Taxes, Fee: order.Fee, NetProfit: order.NetProfit}
			if order.MT5DealType == nil {
				row.OpenPrice = &order.OpenPrice
			}
			if key.verified {
				row.CloseTimeMS = &key.stamp
			}
			page.Items = append(page.Items, row)
		}
		return nil
	})
	return page, err
}

func closedOrderQuery(tx *gorm.DB, account storage.Account, protocol string, f OrderHistoryFilter) *gorm.DB {
	query := currentHistory(tx, account.ID).Where("o.unit_revision=? AND o.native_currency=? AND o.command IN (0,1) AND o.close_time_wall>0", account.UnitRevision, *account.Currency)
	if protocol == "mt5" {
		query = query.Where("o.mt5_deal_type IN (?,?) AND o.mt5_entry IN (?,?,?)", mt5.DealBuy, mt5.DealSell, mt5.EntryOut, mt5.EntryInOut, mt5.EntryOutBy)
	} else {
		query = query.Where("o.mt5_deal_type IS NULL")
	}
	if f.Symbol != "" {
		query = query.Where("o.symbol=? COLLATE NOCASE", strings.TrimSpace(f.Symbol))
	}
	if f.Ticket != 0 {
		query = query.Where("(o.ticket=? OR o.mt5_order_ticket=?)", f.Ticket, f.Ticket)
	}
	switch f.Side {
	case "buy":
		query = query.Where("o.command=0")
	case "sell":
		query = query.Where("o.command=1")
	}
	return query
}

type closedOrderKey struct {
	ticket, stamp int64
	verified      bool
}

func matchingClosedOrders(query *gorm.DB, clock *orderHistoryClock, period historyPeriod, outcome string, page *OrderHistoryPage) ([]closedOrderKey, error) {
	// Only the visible 50 need full prices and fee details. Scan exact Decimal
	// amounts for the total; SQLite REAL cannot preserve their precision.
	rows, err := query.Select("o.ticket,o.event_time_wall,o.mt5_wall_time_ms,o.net_profit").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []closedOrderKey
	for rows.Next() {
		var o storage.HistoryOrder
		if err := rows.Scan(&o.Ticket, &o.EventTimeWall, &o.MT5WallTimeMS, &o.NetProfit); err != nil {
			return nil, err
		}
		sign := o.NetProfit.Sign()
		if outcome == "profit" && sign <= 0 || outcome == "loss" && sign >= 0 || outcome == "flat" && sign != 0 {
			continue
		}
		stamp, verified, err := clock.resolve(o)
		if err != nil {
			return nil, err
		}
		if !verified {
			if period.dated() {
				page.Complete = false
				continue
			}
			stamp = orderWallMS(o)
		}
		if period.dated() && (stamp < period.from || stamp >= period.to) {
			continue
		}
		page.KnownNetProfit = page.KnownNetProfit.Add(o.NetProfit)
		keys = append(keys, closedOrderKey{ticket: o.Ticket, stamp: stamp, verified: verified})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].verified != keys[j].verified {
			return keys[i].verified
		}
		if keys[i].stamp == keys[j].stamp {
			return keys[i].ticket > keys[j].ticket
		}
		return keys[i].stamp > keys[j].stamp
	})
	return keys, nil
}

func orderWallMS(order storage.HistoryOrder) int64 {
	if order.MT5WallTimeMS != nil {
		return *order.MT5WallTimeMS
	}
	return int64(order.EventTimeWall) * 1000
}
