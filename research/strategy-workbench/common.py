"""
Portexa 通用策略分析工作台 - 基础数据库与账户工具
"""

import os
import sqlite3
from typing import Dict, List, Optional, Any

DEFAULT_DB_PATH = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "../../.local/portexa/db/portexa.sqlite")
)

def get_connection(db_path: str = DEFAULT_DB_PATH) -> sqlite3.Connection:
    if not os.path.exists(db_path):
        raise FileNotFoundError(f"未找到 Portexa 数据库文件: {db_path}")
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    return conn

def list_accounts(conn: sqlite3.Connection) -> List[Dict[str, Any]]:
    c = conn.cursor()
    rows = c.execute("""
        SELECT a.id, a.login, a.label, a.currency, a.sync_enabled,
               (SELECT COUNT(*) FROM history_order_revisions h WHERE h.account_id = a.id) as order_count,
               (SELECT COUNT(*) FROM positions p WHERE p.account_id = a.id) as position_count
        FROM accounts a
        ORDER BY a.login ASC
    """).fetchall()
    return [dict(r) for r in rows]

def get_account(conn: sqlite3.Connection, login: int) -> Optional[Dict[str, Any]]:
    c = conn.cursor()
    row = c.execute("""
        SELECT a.id, a.login, a.label, a.currency, a.sync_enabled,
               (SELECT COUNT(*) FROM history_order_revisions h WHERE h.account_id = a.id) as order_count,
               (SELECT COUNT(*) FROM positions p WHERE p.account_id = a.id) as position_count
        FROM accounts a
        WHERE a.login = ?
    """, (login,)).fetchone()
    return dict(row) if row else None
