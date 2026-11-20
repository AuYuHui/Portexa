"""
Portexa 通用策略分析工作台 - 网格与马丁深度分析插件
专门针对识别为网格类的策略，深入计算加仓间距、手数倍率阶梯与一篮子退出收益。
"""

import sqlite3
from collections import Counter
from decimal import Decimal
from typing import Dict, Any, List

def analyze_grid_strategy(conn: sqlite3.Connection, account_id: str) -> Dict[str, Any]:
    c = conn.cursor()

    # 1. 查询当前活跃网格持仓
    pos_rows = c.execute("""
        SELECT ticket, symbol, command, volume_hundredths, exact_lots,
               open_price, current_price, profit, open_time_wall
        FROM positions
        WHERE account_id = ?
        ORDER BY open_time_wall ASC
    """, (account_id,)).fetchall()

    active_grid = {}
    if pos_rows:
        total_lots = Decimal("0")
        total_cost = Decimal("0")
        total_profit = Decimal("0")
        positions = []
        for r in pos_rows:
            lots = Decimal(str(r["exact_lots"])) if r["exact_lots"] else Decimal(r["volume_hundredths"]) / Decimal("100")
            op = Decimal(str(r["open_price"]))
            cp = Decimal(str(r["current_price"]))
            p = Decimal(str(r["profit"]))
            total_lots += lots
            total_cost += op * lots
            total_profit += p
            positions.append({
                "ticket": r["ticket"],
                "command": "BUY" if r["command"] == 0 else "SELL",
                "lots": float(lots),
                "open_price": float(op),
                "profit": float(p)
            })
        avg_price = total_cost / total_lots if total_lots > 0 else Decimal("0")
        cur_price = Decimal(str(pos_rows[-1]["current_price"]))
        dist = float(cur_price - avg_price) if pos_rows[0]["command"] == 0 else float(avg_price - cur_price)

        active_grid = {
            "symbol": pos_rows[0]["symbol"],
            "direction": "BUY" if pos_rows[0]["command"] == 0 else "SELL",
            "layers": len(positions),
            "total_lots": float(total_lots),
            "avg_price": float(avg_price),
            "current_price": float(cur_price),
            "dist_to_cost": dist,
            "total_profit": float(total_profit),
            "positions": positions
        }

    # 2. 统计历史一篮子平仓周期
    cycle_rows = c.execute("""
        SELECT 
            event_time_wall,
            command,
            COUNT(*) as closed_count,
            SUM(CAST(net_profit as REAL)) as total_net_profit
        FROM history_order_revisions
        WHERE account_id = ? AND (command = 0 OR command = 1)
        GROUP BY event_time_wall, command
        ORDER BY event_time_wall ASC
    """, (account_id,)).fetchall()

    total_cycles = len(cycle_rows)
    profit_cycles = 0
    total_net = 0.0
    layer_counter = Counter()

    for r in cycle_rows:
        cnt = r["closed_count"]
        net = r["total_net_profit"] or 0.0
        total_net += net
        layer_counter[cnt] += 1
        if net >= 0:
            profit_cycles += 1

    win_rate = (profit_cycles / total_cycles * 100) if total_cycles > 0 else 0.0
    avg_cycle_profit = (total_net / total_cycles) if total_cycles > 0 else 0.0

    return {
        "active_grid": active_grid,
        "total_cycles": total_cycles,
        "profit_cycles": profit_cycles,
        "cycle_win_rate": win_rate,
        "total_cycle_profit": total_net,
        "avg_cycle_profit": avg_cycle_profit,
        "layer_distribution": sorted(layer_counter.items(), key=lambda x: x[0])
    }
