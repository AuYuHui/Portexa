"""
Portexa 通用策略量化分析工作台 (Universal Strategy Quant Workbench)
支持任意账户的策略流派自动诊断、核心量化指标统计、深度网格剖析与多策略交叉比对。
"""

import argparse
import json
import os
import sys

from common import get_connection, list_accounts, get_account
from classifier import classify_strategy
from metrics import calculate_universal_metrics
from grid_analyzer import analyze_grid_strategy
from compare import compare_accounts

def handle_list(conn):
    accounts = list_accounts(conn)
    print("=" * 65)
    print("           Portexa 数据库已连接的账户列表")
    print("=" * 65)
    print(f"{'登录号(Login)':<15} {'币种':<8} {'历史订单数':<14} {'活跃持仓数':<12} {'同步状态':<8}")
    print("-" * 65)
    for a in accounts:
        sync_str = "启用中" if a["sync_enabled"] else "已暂停"
        print(f"{a['login']:<15} {a['currency']:<8} {a['order_count']:<14} {a['position_count']:<12} {sync_str:<8}")
    print("=" * 65)

def handle_analyze(conn, login: int):
    acc = get_account(conn, login)
    if not acc:
        print(f"错误: 数据库中未找到登录号为 {login} 的账户。可运行 'list' 查看可用账户。")
        return

    acc_id = acc["id"]
    cls_res = classify_strategy(conn, acc_id)
    met_res = calculate_universal_metrics(conn, acc_id)

    print("=" * 68)
    print(f"       Portexa 策略量化诊断报告 - 账户: {login} ({acc['currency']})")
    print("=" * 68)
    print("【第一部分：策略画像与流派识别】")
    print(f"  识别流派:   {cls_res['archetype']} (置信度: {cls_res['confidence']*100:.0f}%)")
    print(f"  特征标签:   {', '.join(cls_res['tags'])}")
    print(f"  出场结构:   一篮子批量平仓占比 {cls_res['basket_exit_ratio']:.1f}% | 硬SL/TP占比 {cls_res['sltp_ratio']:.1f}%")
    print(f"  当前状态:   活跃持仓 {cls_res['active_positions_count']} 笔")
    print("-" * 68)
    print("【第二部分：通用核心量化指标】")
    print(f"  样本订单总数: {met_res['total_trades']} 笔 (盈利 {met_res['win_count']} / 亏损 {met_res['loss_count']})")
    print(f"  订单综合胜率: {met_res['win_rate']:.2f}%")
    print(f"  总净盈亏额:   {met_res['net_profit']:,.2f} {acc['currency']}")
    print(f"  盈亏比 (PF):  {met_res['profit_factor']:.2f}")
    print(f"  赔率 (盈亏均比): {met_res['payoff_ratio']:.2f} (均赢: {met_res['avg_win']:.2f} / 均亏: {met_res['avg_loss']:.2f})")
    print(f"  持仓时长分位: P50 中位数 {met_res['p50_duration_sec']} 秒 | P90 {met_res['p90_duration_sec']} 秒")
    print(f"  交易摩擦损耗: 手续费 {met_res['total_commission']:.2f} | 隔夜利息 {met_res['total_swap']:.2f}")
    print(f"  主要交易品种: {met_res['symbols']}")
    print("-" * 68)

    # 深度下钻：如果是网格类策略，调用网格插件
    if cls_res["recommend_tool"] == "grid":
        print("【第三部分：网格/马丁专属深度下钻】")
        grid_res = analyze_grid_strategy(conn, acc_id)
        act = grid_res["active_grid"]
        if act:
            print(f"  当前活动网格: {act['symbol']} {act['direction']} {act['layers']} 层 (累计 {act['total_lots']:.2f} 手)")
            print(f"  加权持仓成本: {act['avg_price']:.2f} | 当前市价: {act['current_price']:.2f} (偏离 {act['dist_to_cost']:+.2f})")
            print(f"  当前网格浮盈: {act['total_profit']:.2f} {acc['currency']}")
        print(f"  历史一篮子周期: 共统计 {grid_res['total_cycles']} 轮 | 周期胜率: {grid_res['cycle_win_rate']:.2f}%")
        print(f"  单轮平均收益:   {grid_res['avg_cycle_profit']:.2f} {acc['currency']}")
        print("  主要解套层数前5名:")
        for lyr, cnt in grid_res["layer_distribution"][:5]:
            pct = (cnt / grid_res['total_cycles'] * 100) if grid_res['total_cycles'] > 0 else 0
            print(f"    - 第 {lyr} 层出场: {cnt} 轮 ({pct:.1f}%)")
    print("=" * 68)

def handle_compare(conn, login1: int, login2: int):
    acc1 = get_account(conn, login1)
    acc2 = get_account(conn, login2)
    if not acc1 or not acc2:
        print(f"错误: 需提供两个在库有效的账户。登录号1: {login1}, 登录号2: {login2}")
        return
    compare_accounts(conn, acc1, acc2)

def handle_export(conn, login: int, out_dir: str):
    acc = get_account(conn, login)
    if not acc:
        print(f"错误: 账户 {login} 不存在。")
        return
    os.makedirs(out_dir, exist_ok=True)
    c = conn.cursor()
    rows = c.execute("""
        SELECT ticket, command, symbol, comment, exact_lots,
               event_time_wall, open_price, close_price, profit, net_profit, mt5_entry
        FROM history_order_revisions
        WHERE account_id = ?
        ORDER BY event_time_wall ASC
    """, (acc["id"],)).fetchall()
    
    out_file = os.path.join(out_dir, f"account_{login}_orders.json")
    with open(out_file, "w", encoding="utf-8") as f:
        json.dump([dict(r) for r in rows], f, ensure_ascii=False, indent=2)
    print(f"已导出账户 {login} 的 {len(rows)} 笔订单到 -> {out_file}")

def main():
    parser = argparse.ArgumentParser(description="Portexa 通用策略量化分析工作台")
    subparsers = parser.add_subparsers(dest="subcommand", help="工作台子命令")

    subparsers.add_parser("list", help="列出所有可分析的已连接账户")

    p_analyze = subparsers.add_parser("analyze", help="深入分析指定账户的策略流派与量化表现")
    p_analyze.add_argument("--login", type=int, default=88001135, help="账户登录号 (默认 88001135)")

    p_compare = subparsers.add_parser("compare", help="横向对比两个账户的策略特征")
    p_compare.add_argument("--login1", type=int, required=True, help="账户1登录号")
    p_compare.add_argument("--login2", type=int, required=True, help="账户2登录号")

    p_export = subparsers.add_parser("export", help="导出指定账户的标准化交易数据")
    p_export.add_argument("--login", type=int, default=88001135, help="账户登录号")
    p_export.add_argument("--out", type=str, default="research/strategy-workbench/data", help="输出目录")

    args = parser.parse_args()
    if not args.subcommand:
        # 默认执行 analyze 88001135
        args.subcommand = "analyze"
        args.login = 88001135

    try:
        conn = get_connection()
    except Exception as e:
        print(f"数据库连接失败: {e}")
        sys.exit(1)

    if args.subcommand == "list":
        handle_list(conn)
    elif args.subcommand == "analyze":
        handle_analyze(conn, args.login)
    elif args.subcommand == "compare":
        handle_compare(conn, args.login1, args.login2)
    elif args.subcommand == "export":
        handle_export(conn, args.login, args.out)

if __name__ == "__main__":
    main()
