"""
Portexa 通用策略分析工作台 - 策略指纹与自动分类器
通过订单持仓并发度、出场特征与风控设定，自动诊断策略大类。
"""

import sqlite3
from typing import Dict, Any

def classify_strategy(conn: sqlite3.Connection, account_id: str) -> Dict[str, Any]:
    c = conn.cursor()

    # 1. 查询当前持仓并发度
    active_count = c.execute(
        "SELECT COUNT(*) FROM positions WHERE account_id = ?", (account_id,)
    ).fetchone()[0]

    # 2. 检查是否有硬止损/止盈 (SL/TP)
    sl_tp_stats = c.execute("""
        SELECT 
            COUNT(*) as total,
            SUM(CASE WHEN CAST(sl as REAL) > 0 OR CAST(tp as REAL) > 0 THEN 1 ELSE 0 END) as with_sltp
        FROM history_order_revisions
        WHERE account_id = ? AND (command = 0 OR command = 1)
    """, (account_id,)).fetchone()
    
    total_orders = sl_tp_stats["total"] or 0
    with_sltp = sl_tp_stats["with_sltp"] or 0
    sltp_ratio = (with_sltp / total_orders * 100) if total_orders > 0 else 0.0

    # 3. 检查是否有“同秒组合平仓”（一篮子平仓特征）
    basket_stats = c.execute("""
        SELECT COUNT(*) as multi_close_groups, SUM(group_size) as multi_close_orders
        FROM (
            SELECT event_time_wall, COUNT(*) as group_size
            FROM history_order_revisions
            WHERE account_id = ? AND (command = 0 OR command = 1)
            GROUP BY event_time_wall, command
            HAVING COUNT(*) >= 2
        )
    """, (account_id,)).fetchone()

    multi_close_orders = basket_stats["multi_close_orders"] or 0
    basket_exit_ratio = (multi_close_orders / total_orders * 100) if total_orders > 0 else 0.0

    # 4. 检查是否有两向持仓
    direction_stats = c.execute("""
        SELECT DISTINCT command
        FROM history_order_revisions
        WHERE account_id = ? AND (command = 0 OR command = 1)
    """, (account_id,)).fetchall()
    has_buy = any(r[0] == 0 for r in direction_stats)
    has_sell = any(r[0] == 1 for r in direction_stats)

    # 5. 综合判定流派 (Archetype)
    tags = []
    archetype = "未知策略类型"
    confidence = 0.5
    recommend_tool = "general"

    if basket_exit_ratio > 40.0:
        tags.append("一篮子组合平仓 (Basket Exit)")
        if has_buy and has_sell:
            archetype = "双向循环网格 (Bidirectional Grid)"
            tags.append("多空双向常驻")
        else:
            archetype = "单向网格/马丁加仓 (Single-Direction Grid)"
        tags.append("逆势补仓解套机制")
        confidence = 0.95
        recommend_tool = "grid"
    elif sltp_ratio > 60.0:
        archetype = "固定风控/趋势突破 (Fixed Risk / Trend)"
        tags.append("预设硬止盈止损")
        confidence = 0.85
        recommend_tool = "trend"
    else:
        if active_count <= 1:
            archetype = "单单独立/短线交易 (Single Position Scalper)"
            tags.append("单笔进出不重叠")
            confidence = 0.70
            recommend_tool = "scalper"
        else:
            archetype = "混合并发策略 (Multi-position Scalper/Grid)"
            tags.append("多单并发持仓")
            confidence = 0.65
            recommend_tool = "general"

    if sltp_ratio < 5.0:
        tags.append("无硬止损/软止损平仓")

    return {
        "archetype": archetype,
        "confidence": confidence,
        "tags": tags,
        "recommend_tool": recommend_tool,
        "total_analyzed_orders": total_orders,
        "sltp_ratio": sltp_ratio,
        "basket_exit_ratio": basket_exit_ratio,
        "active_positions_count": active_count,
        "has_buy": has_buy,
        "has_sell": has_sell
    }
