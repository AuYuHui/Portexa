"""
Portexa 通用策略分析工作台 - 通用量化指标引擎
计算任意策略的胜率、盈亏比、回撤特征、交易时长分布与摩擦成本。
"""

import sqlite3
from typing import Dict, Any, List

def calculate_universal_metrics(conn: sqlite3.Connection, account_id: str) -> Dict[str, Any]:
    c = conn.cursor()
    
    # 统计已平仓交易 (MT5 撮合成交 entry=1, MT4 command=0/1)
    rows = c.execute("""
        SELECT 
            ticket, command, symbol, exact_lots, volume_hundredths,
            open_time_wall, close_time_wall, event_time_wall,
            open_price, close_price,
            CAST(profit as REAL) as profit,
            CAST(net_profit as REAL) as net_profit,
            CAST(commission as REAL) as commission,
            CAST(swap as REAL) as swap
        FROM history_order_revisions
        WHERE account_id = ? AND (command = 0 OR command = 1)
        ORDER BY event_time_wall ASC
    """, (account_id,)).fetchall()

    if not rows:
        return {"total_trades": 0}

    total_trades = len(rows)
    wins = []
    losses = []
    durations = []
    total_commission = 0.0
    total_swap = 0.0
    symbol_counts: Dict[str, int] = {}

    for r in rows:
        sym = r["symbol"]
        symbol_counts[sym] = symbol_counts.get(sym, 0) + 1

        net = r["net_profit"]
        if net > 0:
            wins.append(net)
        elif net < 0:
            losses.append(abs(net))

        total_commission += r["commission"]
        total_swap += r["swap"]

        # 持仓时间估算
        ot = r["open_time_wall"]
        ct = r["close_time_wall"] or r["event_time_wall"]
        if ot and ct and ct >= ot:
            durations.append(ct - ot)

    gross_profit = sum(wins)
    gross_loss = sum(losses)
    net_profit = gross_profit - gross_loss

    win_count = len(wins)
    loss_count = len(losses)
    win_rate = (win_count / total_trades * 100) if total_trades > 0 else 0.0
    profit_factor = (gross_profit / gross_loss) if gross_loss > 0 else (999.0 if gross_profit > 0 else 0.0)

    avg_win = (gross_profit / win_count) if win_count > 0 else 0.0
    avg_loss = (gross_loss / loss_count) if loss_count > 0 else 0.0
    payoff_ratio = (avg_win / avg_loss) if avg_loss > 0 else 0.0

    durations.sort()
    p50_dur = durations[len(durations) // 2] if durations else 0
    p90_dur = durations[int(len(durations) * 0.9)] if durations else 0

    return {
        "total_trades": total_trades,
        "win_count": win_count,
        "loss_count": loss_count,
        "win_rate": win_rate,
        "gross_profit": gross_profit,
        "gross_loss": gross_loss,
        "net_profit": net_profit,
        "profit_factor": profit_factor,
        "avg_win": avg_win,
        "avg_loss": avg_loss,
        "payoff_ratio": payoff_ratio,
        "p50_duration_sec": p50_dur,
        "p90_duration_sec": p90_dur,
        "total_commission": total_commission,
        "total_swap": total_swap,
        "symbols": symbol_counts
    }
