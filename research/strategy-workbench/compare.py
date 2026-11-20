"""
Portexa 通用策略分析工作台 - 多账户策略交叉比对工具
横向对比两个不同跟单账户的策略流派、盈利质量、回撤与执行差异。
"""

import sqlite3
from typing import Dict, Any

from classifier import classify_strategy
from metrics import calculate_universal_metrics

def compare_accounts(conn: sqlite3.Connection, acc_a: Dict[str, Any], acc_b: Dict[str, Any]):
    class_a = classify_strategy(conn, acc_a["id"])
    class_b = classify_strategy(conn, acc_b["id"])

    met_a = calculate_universal_metrics(conn, acc_a["id"])
    met_b = calculate_universal_metrics(conn, acc_b["id"])

    print("=" * 75)
    print("           Portexa 通用策略工作台 - 多账户横向对比报告")
    print("=" * 75)
    print(f"{'对比维度':<22} {'账户 A (' + str(acc_a['login']) + ')':<25} {'账户 B (' + str(acc_b['login']) + ')':<25}")
    print("-" * 75)
    print(f"{'识别流派':<20} {class_a['archetype']:<24} {class_b['archetype']:<24}")
    print(f"{'币种单位':<22} {acc_a['currency']:<25} {acc_b['currency']:<25}")
    print(f"{'总订单记录数':<20} {met_a['total_trades']:<25} {met_b['total_trades']:<25}")
    print(f"{'当前活跃持仓数':<19} {class_a['active_positions_count']:<25} {class_b['active_positions_count']:<25}")
    print(f"{'胜率 (Win Rate)':<20} {met_a['win_rate']:<24.2f}% {met_b['win_rate']:<24.2f}%")
    print(f"{'盈亏比 (Profit Factor)':<17} {met_a['profit_factor']:<25.2f} {met_b['profit_factor']:<25.2f}")
    print(f"{'累计净收益':<21} {met_a['net_profit']:<24.2f} {met_b['net_profit']:<24.2f}")
    print(f"{'一篮子组合平仓比例':<17} {class_a['basket_exit_ratio']:<24.1f}% {class_b['basket_exit_ratio']:<24.1f}%")
    print(f"{'预设硬SL/TP比例':<18} {class_a['sltp_ratio']:<24.1f}% {class_b['sltp_ratio']:<24.1f}%")
    print(f"{'持仓时长中位数(P50)':<17} {met_a['p50_duration_sec']:<23}秒 {met_b['p50_duration_sec']:<23}秒")
    print("-" * 75)

    # 判定同源性
    is_same_archetype = class_a["archetype"] == class_b["archetype"]
    print("策略关联度诊断:")
    if is_same_archetype and abs(class_a["basket_exit_ratio"] - class_b["basket_exit_ratio"]) < 15:
        print("  => 两个账户交易行为高度相近，大概率属于【同一策略家族】或不同参数配置的变体。")
    else:
        print("  => 两个账户表现出不同的出场机制或仓位管理逻辑，属于【不同流派】的独立策略。")
    print("=" * 75)
