package accounts

import (
	"context"
	"math"
	"sort"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
)

type StrategyMetrics struct {
	TotalTrades     int64           `json:"total_trades"`
	WinCount        int64           `json:"win_count"`
	LossCount       int64           `json:"loss_count"`
	WinRate         float64         `json:"win_rate"`
	GrossProfit     decimal.Decimal `json:"gross_profit"`
	GrossLoss       decimal.Decimal `json:"gross_loss"`
	NetProfit       decimal.Decimal `json:"net_profit"`
	ProfitFactor    float64         `json:"profit_factor"`
	AvgWin          decimal.Decimal `json:"avg_win"`
	AvgLoss         decimal.Decimal `json:"avg_loss"`
	TotalCommission decimal.Decimal `json:"total_commission"`
	TotalSwap       decimal.Decimal `json:"total_swap"`
}

type ActiveGrid struct {
	HasActive        bool            `json:"has_active"`
	Symbol           string          `json:"symbol"`
	Direction        string          `json:"direction"`
	DominantSide     string          `json:"dominant_side"`
	BuyCount         int             `json:"buy_count"`
	SellCount        int             `json:"sell_count"`
	BuyLots          decimal.Decimal `json:"buy_lots"`
	SellLots         decimal.Decimal `json:"sell_lots"`
	Layers           int             `json:"layers"`
	TotalLots        decimal.Decimal `json:"total_lots"`
	AvgPrice         decimal.Decimal `json:"avg_price"`
	CurrentPrice     decimal.Decimal `json:"current_price"`
	DistToCost       decimal.Decimal `json:"dist_to_cost"`
	TotalProfit      decimal.Decimal `json:"total_profit"`
	MaxHistoricLayer int             `json:"max_historic_layer"`
}

type LayerDist struct {
	Layer   int     `json:"layer"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

type GridHistory struct {
	TotalCycles       int             `json:"total_cycles"`
	CycleWinRate      float64         `json:"cycle_win_rate"`
	TotalCycleProfit  decimal.Decimal `json:"total_cycle_profit"`
	AvgCycleProfit    decimal.Decimal `json:"avg_cycle_profit"`
	LayerDistribution []LayerDist     `json:"layer_distribution"`
}

type StrategyView struct {
	AccountID       string          `json:"account_id"`
	Currency        string          `json:"currency"`
	Archetype       string          `json:"archetype"`
	Confidence      float64         `json:"confidence"`
	Tags            []string        `json:"tags"`
	BasketExitRatio float64         `json:"basket_exit_ratio"`
	SLTPRatio       float64         `json:"sltp_ratio"`
	Metrics         StrategyMetrics `json:"metrics"`
	ActiveGrid      ActiveGrid      `json:"active_grid"`
	GridHistory     GridHistory     `json:"grid_history"`
}

func (s *Service) Strategy(ctx context.Context, id string) (StrategyView, error) {
	view := StrategyView{
		AccountID:   id,
		Tags:        make([]string, 0),
		ActiveGrid:  ActiveGrid{},
		GridHistory: GridHistory{LayerDistribution: make([]LayerDist, 0)},
	}

	read := s.Store.Read.WithContext(ctx)

	var account storage.Account
	if err := read.First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
		return view, err
	}
	if account.Currency != nil {
		view.Currency = *account.Currency
	}

	// 1. 查询当前活跃持仓并计算活跃网格状态
	var positions []storage.Position
	if err := read.Where("account_id = ?", id).Order("open_time_wall ASC").Find(&positions).Error; err != nil {
		return view, err
	}

	tradePositions := make([]storage.Position, 0)
	for _, p := range positions {
		if p.Command <= 1 {
			tradePositions = append(tradePositions, p)
		}
	}

	if len(tradePositions) > 0 {
		view.ActiveGrid.HasActive = true
		view.ActiveGrid.Layers = len(tradePositions)
		view.ActiveGrid.Symbol = tradePositions[0].Symbol

		totalLots := decimal.Zero
		totalProfit := decimal.Zero
		buyLots := decimal.Zero
		sellLots := decimal.Zero
		buyCost := decimal.Zero
		sellCost := decimal.Zero
		buyCount := 0
		sellCount := 0

		for _, p := range tradePositions {
			lots := p.Lots()
			totalLots = totalLots.Add(lots)
			totalProfit = totalProfit.Add(p.Profit)
			if p.Command == 0 {
				buyCount++
				buyLots = buyLots.Add(lots)
				buyCost = buyCost.Add(p.OpenPrice.Mul(lots))
			} else {
				sellCount++
				sellLots = sellLots.Add(lots)
				sellCost = sellCost.Add(p.OpenPrice.Mul(lots))
			}
		}

		view.ActiveGrid.TotalLots = totalLots
		view.ActiveGrid.TotalProfit = totalProfit
		view.ActiveGrid.BuyCount = buyCount
		view.ActiveGrid.SellCount = sellCount
		view.ActiveGrid.BuyLots = buyLots
		view.ActiveGrid.SellLots = sellLots

		lastPos := tradePositions[len(tradePositions)-1]
		view.ActiveGrid.CurrentPrice = lastPos.CurrentPrice

		// 判断主防守/主网格方向
		if sellLots.GreaterThan(buyLots) {
			view.ActiveGrid.DominantSide = "SELL"
			view.ActiveGrid.Direction = "双向持仓（主空头）"
			if sellLots.GreaterThan(decimal.Zero) {
				avgSell := sellCost.DivRound(sellLots, 2)
				view.ActiveGrid.AvgPrice = avgSell
				view.ActiveGrid.DistToCost = avgSell.Sub(view.ActiveGrid.CurrentPrice)
			}
		} else if buyLots.GreaterThan(sellLots) {
			view.ActiveGrid.DominantSide = "BUY"
			view.ActiveGrid.Direction = "双向持仓（主多头）"
			if buyLots.GreaterThan(decimal.Zero) {
				avgBuy := buyCost.DivRound(buyLots, 2)
				view.ActiveGrid.AvgPrice = avgBuy
				view.ActiveGrid.DistToCost = view.ActiveGrid.CurrentPrice.Sub(avgBuy)
			}
		} else {
			view.ActiveGrid.DominantSide = "NEUTRAL"
			view.ActiveGrid.Direction = "双向完全对冲"
			if totalLots.GreaterThan(decimal.Zero) {
				allCost := buyCost.Add(sellCost)
				view.ActiveGrid.AvgPrice = allCost.DivRound(totalLots, 2)
				view.ActiveGrid.DistToCost = decimal.Zero
			}
		}
	}

	// 2. 统计已结交易指标与一篮子平仓周期
	var orders []storage.HistoryOrder
	if err := read.Where("account_id = ? AND (command = 0 OR command = 1)", id).
		Order("event_time_wall ASC").Find(&orders).Error; err != nil {
		return view, err
	}

	if len(orders) == 0 {
		view.Archetype = "数据积累中"
		view.Confidence = 0.5
		return view, nil
	}

	view.Metrics.TotalTrades = int64(len(orders))
	grossProfit := decimal.Zero
	grossLoss := decimal.Zero
	totalNet := decimal.Zero
	totalComm := decimal.Zero
	totalSwap := decimal.Zero
	withSLTP := int64(0)

	type cycleKey struct {
		eventTime uint32
		command   int32
	}
	cycleCounts := make(map[cycleKey]int)
	cycleProfits := make(map[cycleKey]decimal.Decimal)

	for _, o := range orders {
		net := o.NetProfit
		totalNet = totalNet.Add(net)
		totalComm = totalComm.Add(o.Commission)
		totalSwap = totalSwap.Add(o.Swap)

		if net.GreaterThan(decimal.Zero) {
			view.Metrics.WinCount++
			grossProfit = grossProfit.Add(net)
		} else if net.LessThan(decimal.Zero) {
			view.Metrics.LossCount++
			grossLoss = grossLoss.Add(net.Abs())
		}

		if o.SL.GreaterThan(decimal.Zero) || o.TP.GreaterThan(decimal.Zero) {
			withSLTP++
		}

		key := cycleKey{eventTime: o.EventTimeWall, command: o.Command}
		cycleCounts[key]++
		cycleProfits[key] = cycleProfits[key].Add(net)
	}

	view.Metrics.GrossProfit = grossProfit
	view.Metrics.GrossLoss = grossLoss
	view.Metrics.NetProfit = totalNet
	view.Metrics.TotalCommission = totalComm
	view.Metrics.TotalSwap = totalSwap

	if view.Metrics.TotalTrades > 0 {
		view.Metrics.WinRate = math.Round(float64(view.Metrics.WinCount)/float64(view.Metrics.TotalTrades)*10000) / 100
		view.SLTPRatio = math.Round(float64(withSLTP)/float64(view.Metrics.TotalTrades)*10000) / 100
	}

	if view.Metrics.LossCount > 0 && grossLoss.GreaterThan(decimal.Zero) {
		view.Metrics.ProfitFactor, _ = grossProfit.DivRound(grossLoss, 2).Float64()
	} else if grossProfit.GreaterThan(decimal.Zero) {
		view.Metrics.ProfitFactor = 999.0
	}

	if view.Metrics.WinCount > 0 {
		view.Metrics.AvgWin = grossProfit.DivRound(decimal.NewFromInt(view.Metrics.WinCount), 2)
	}
	if view.Metrics.LossCount > 0 {
		view.Metrics.AvgLoss = grossLoss.DivRound(decimal.NewFromInt(view.Metrics.LossCount), 2)
	}

	// 3. 统计一篮子平仓特征
	multiCloseOrders := 0
	winCycles := 0
	layerFreq := make(map[int]int)
	totalCycleProfit := decimal.Zero
	maxLayer := 0

	for key, count := range cycleCounts {
		if count >= 2 {
			multiCloseOrders += count
		}
		if count > maxLayer {
			maxLayer = count
		}
		profit := cycleProfits[key]
		totalCycleProfit = totalCycleProfit.Add(profit)
		if profit.GreaterThanOrEqual(decimal.Zero) {
			winCycles++
		}
		layerFreq[count]++
	}

	view.ActiveGrid.MaxHistoricLayer = maxLayer

	if view.Metrics.TotalTrades > 0 {
		view.BasketExitRatio = math.Round(float64(multiCloseOrders)/float64(view.Metrics.TotalTrades)*10000) / 100
	}

	totalCycles := len(cycleCounts)
	view.GridHistory.TotalCycles = totalCycles
	view.GridHistory.TotalCycleProfit = totalCycleProfit
	if totalCycles > 0 {
		view.GridHistory.CycleWinRate = math.Round(float64(winCycles)/float64(totalCycles)*10000) / 100
		view.GridHistory.AvgCycleProfit = totalCycleProfit.DivRound(decimal.NewFromInt(int64(totalCycles)), 2)
	}

	layerDists := make([]LayerDist, 0, len(layerFreq))
	for lyr, cnt := range layerFreq {
		pct := 0.0
		if totalCycles > 0 {
			pct = math.Round(float64(cnt)/float64(totalCycles)*10000) / 100
		}
		layerDists = append(layerDists, LayerDist{Layer: lyr, Count: cnt, Percent: pct})
	}
	sort.Slice(layerDists, func(i, j int) bool {
		return layerDists[i].Layer < layerDists[j].Layer
	})
	if len(layerDists) > 8 {
		layerDists = layerDists[:8]
	}
	view.GridHistory.LayerDistribution = layerDists

	// 4. 判定策略流派 Archetype
	if view.BasketExitRatio > 40.0 {
		view.Archetype = "双向循环网格 (Bidirectional Grid)"
		view.Confidence = 0.95
		view.Tags = append(view.Tags, "一篮子组合平仓", "逆势补仓解套", "多空循环在市")
	} else if view.SLTPRatio > 60.0 {
		view.Archetype = "固定风控/趋势突破 (Fixed Risk / Trend)"
		view.Confidence = 0.85
		view.Tags = append(view.Tags, "预设硬SL/TP", "单笔独立风控")
	} else {
		view.Archetype = "单单短线交易 (Scalper / Day Trade)"
		view.Confidence = 0.70
		view.Tags = append(view.Tags, "高频进出", "软止损平仓")
	}

	return view, nil
}
