# Portexa SQLite 数据模型

本文细化[产品方案](product-plan.md)的持久化契约。SQL 是供实现与约束验证使用的设计原型，不是已应用的迁移；第一阶段只建立其实际使用的表，历史相关表在第二阶段引入。

## 存储约定

- 数据库位于服务器本机的私有运行目录，使用 STRICT 表。生产 SQLite 基线为3.51.3及以上，包含官方 WAL-reset 修复；本地原型只做内存约束验证，不代替生产版本验收。
- 应用生成稳定的随机文本 ID。服务器 ID 不因接入 IP、显示名称变化而改变；交易账户唯一键为 `(server_id, login)`，协议类型由服务器记录确定。
- UTC 时间使用 INTEGER 毫秒；MT4 原始墙钟值使用 INTEGER 秒，列名含 `wall`，不能直接解释为 UTC。
- 金额和价格使用规范化十进制 TEXT，保留原币。Go Decimal 负责数值校验、比较与计算，禁止用 SQLite REAL、文本字典序或隐式 `SUM(TEXT)` 处理金额。
- 各数据库连接显式启用外键；缺失数值使用 NULL。JSON 字段必须经过 Go 结构校验，不能成为任意配置或敏感报文的容器。
- 投资者密码只保存 AEAD 密文封装和密钥 ID；会话只保存随机令牌的 SHA-256 摘要。系统密码单独保存在单例 `auth_settings`，没有用户表或角色表。

GORM 模型显式映射本文的表名、主键、INTEGER 毫秒时间和 NULL 语义，金额通过 Decimal 的 Scanner/Valuer 接口按 TEXT 读写。DDL 与版本化迁移是结构依据，ORM 不隐式增加主键、软删除列或时间列，也不在生产启动时通过 `AutoMigrate` 改写表结构。ORM 集成必须验证零值、NULL、原子回滚和现有触发器，不能只验证普通插入成功。

## 实体与引入阶段

| 实体 | 阶段 | 职责 |
| --- | --- | --- |
| `auth_settings`、`sessions` | 一 | 单例登录密码、会话和全局撤销版本 |
| `servers`、`server_endpoints` | 一 | 稳定服务器身份、版本化接入点及核验记录 |
| `server_time_rule_sets` | 一记录，二用于历史 | 有证据和版本的服务器时区规则 |
| `server_directory_evidence` | 一，迁移002 | 服务器目录来源、取得时间、响应摘要及私有原文，随数据库备份 |
| `accounts` | 一 | 交易账号、单位、加密凭据、采集开关和当前快照指针 |
| `sync_runs` | 一 | 一次实际执行、调度键、失败分类和恢复审计 |
| `account_snapshots` | 一 | 完整资金快照及采集时间窗口 |
| `positions` | 一 | 当前买卖持仓和挂单，不复制保存每一分钟的整套订单 |
| `history_coverage` | 二 | 请求区间、拆分关系、成功覆盖及恢复状态 |
| `trades`、`trade_revisions` | 二 | 稳定订单身份、原始字段修订及原子发布 |
| `trade_time_resolutions` | 二 | 原始墙钟到 UTC 的版本化解释和歧义状态 |
| `cash_flow_classifications` | 二 | 人工资金分类及修订历史 |

`servers` 与 `server_endpoints` 分开，防止修改 IP 时改变账户身份或沿用旧核验。`trades` 与 `trade_revisions` 分开，使大批历史可分批暂存、一次发布，失败时不暴露半批结果。

首版采用核验固定接入点并确认剩余风险后启用直连。新接入点初始为 `pending`、`risk_accepted_at_ms` 为 NULL；独立核验、该修订风险确认及账户绑定均完成后才可启用采集。`reviewed` 表示接入来源已核验，不代表密码学身份认证；同一有效修订的日常同步复用已保存的确认。

## 核心表与索引原型

以下 SQL 可按完整依赖图在内存执行；正式迁移按阶段拆分，并维护迁移版本。外键循环中的当前指针先设为 NULL，在子记录建立后更新。

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE auth_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    auth_version INTEGER NOT NULL CHECK (auth_version > 0),
    changed_at_ms INTEGER NOT NULL
) STRICT;

CREATE TABLE servers (
    id TEXT PRIMARY KEY,
    protocol TEXT NOT NULL CHECK (protocol = 'mt4'),
    broker_name TEXT NOT NULL,
    server_name TEXT NOT NULL,
    active_time_ruleset_id TEXT,
    created_at_ms INTEGER NOT NULL,
    retired_at_ms INTEGER,
    FOREIGN KEY (id, active_time_ruleset_id)
        REFERENCES server_time_rule_sets(server_id, id)
) STRICT;

CREATE TABLE server_endpoints (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    ip_address TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    state TEXT NOT NULL CHECK (state IN ('pending', 'reviewed', 'retired')),
    evidence_ref TEXT,
    evidence_sha256 BLOB,
    reviewed_at_ms INTEGER,
    risk_accepted_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    UNIQUE (server_id, revision),
    UNIQUE (server_id, id),
    CHECK (state != 'reviewed' OR
        (evidence_ref IS NOT NULL AND reviewed_at_ms IS NOT NULL))
) STRICT;

CREATE TRIGGER endpoint_address_is_immutable
BEFORE UPDATE OF server_id, revision, ip_address, port ON server_endpoints
BEGIN
    SELECT RAISE(ABORT, 'create a new endpoint revision');
END;

CREATE TABLE server_time_rule_sets (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    rules_json TEXT NOT NULL CHECK (json_valid(rules_json)),
    evidence_ref TEXT,
    verified_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    UNIQUE (server_id, revision),
    UNIQUE (server_id, id)
) STRICT;

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id),
    pinned_endpoint_id TEXT,
    login INTEGER NOT NULL CHECK (login BETWEEN 1 AND 4294967295),
    label TEXT NOT NULL,
    currency TEXT,
    usd_scale TEXT,
    unit_revision INTEGER NOT NULL DEFAULT 1 CHECK (unit_revision > 0),
    unit_verified_at_ms INTEGER,
    credential_envelope BLOB NOT NULL,
    credential_key_id TEXT NOT NULL,
    credential_version INTEGER NOT NULL CHECK (credential_version > 0),
    config_version INTEGER NOT NULL DEFAULT 1 CHECK (config_version > 0),
    sync_enabled INTEGER NOT NULL DEFAULT 0 CHECK (sync_enabled IN (0, 1)),
    next_sync_at_ms INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    current_snapshot_id TEXT,
    included_at_ms INTEGER,
    initial_statistical_equity TEXT,
    removed_at_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    UNIQUE (server_id, login),
    UNIQUE (id, server_id),
    FOREIGN KEY (server_id, pinned_endpoint_id)
        REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (id, current_snapshot_id)
        REFERENCES account_snapshots(account_id, id)
) STRICT;

CREATE TABLE history_coverage (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    parent_id TEXT,
    start_wall INTEGER NOT NULL CHECK (start_wall >= 0),
    end_wall INTEGER NOT NULL CHECK (end_wall <= 2147483647),
    state TEXT NOT NULL CHECK (state IN ('pending', 'complete', 'split', 'blocked')),
    checked_at_ms INTEGER,
    last_success_run_id TEXT,
    created_at_ms INTEGER NOT NULL,
    CHECK (start_wall < end_wall),
    CHECK (state != 'complete' OR
        (checked_at_ms IS NOT NULL AND last_success_run_id IS NOT NULL)),
    UNIQUE (account_id, start_wall, end_wall),
    UNIQUE (id, account_id),
    FOREIGN KEY (parent_id, account_id)
        REFERENCES history_coverage(id, account_id),
    FOREIGN KEY (last_success_run_id, account_id)
        REFERENCES sync_runs(id, account_id)
) STRICT;

CREATE TABLE sync_runs (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    server_id TEXT NOT NULL,
    endpoint_id TEXT NOT NULL,
    credential_version INTEGER NOT NULL,
    config_version INTEGER NOT NULL DEFAULT 1 CHECK (config_version > 0),
    kind TEXT NOT NULL CHECK (kind IN
        ('snapshot', 'history_recent', 'history_backfill', 'history_repair')),
    state TEXT NOT NULL CHECK (state IN
        ('queued', 'running', 'succeeded', 'failed', 'canceled', 'interrupted')),
    coverage_id TEXT,
    due_at_ms INTEGER NOT NULL,
    not_before_ms INTEGER NOT NULL,
    started_at_ms INTEGER,
    finished_at_ms INTEGER,
    retry_of_id TEXT REFERENCES sync_runs(id),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    error_code TEXT,
    error_operation TEXT,
    raw_status INTEGER,
    completion_reason TEXT,
    UNIQUE (id, account_id),
    FOREIGN KEY (account_id, server_id) REFERENCES accounts(id, server_id),
    FOREIGN KEY (server_id, endpoint_id) REFERENCES server_endpoints(server_id, id),
    FOREIGN KEY (coverage_id, account_id)
        REFERENCES history_coverage(id, account_id),
    CHECK ((kind = 'snapshot' AND coverage_id IS NULL) OR
        (kind != 'snapshot' AND coverage_id IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX sync_one_live_kind
    ON sync_runs(account_id, kind) WHERE state IN ('queued', 'running');
CREATE UNIQUE INDEX sync_one_running_account
    ON sync_runs(account_id) WHERE state = 'running';
CREATE INDEX sync_due ON sync_runs(state, not_before_ms, due_at_ms);
CREATE INDEX sync_account_time ON sync_runs(account_id, started_at_ms DESC);
CREATE TRIGGER sync_terminal_state_is_immutable
BEFORE UPDATE OF state ON sync_runs
WHEN OLD.state IN ('succeeded', 'failed', 'canceled', 'interrupted')
 AND NEW.state != OLD.state
BEGIN
    SELECT RAISE(ABORT, 'terminal run cannot change state');
END;

CREATE INDEX coverage_lookup ON history_coverage(account_id, state, start_wall, end_wall);
CREATE INDEX coverage_committed
    ON history_coverage(account_id, start_wall, end_wall)
    WHERE last_success_run_id IS NOT NULL;

CREATE TABLE account_snapshots (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    sync_run_id TEXT NOT NULL UNIQUE,
    observed_from_ms INTEGER NOT NULL,
    observed_to_ms INTEGER NOT NULL,
    native_currency TEXT NOT NULL,
    usd_scale TEXT,
    unit_revision INTEGER NOT NULL,
    balance TEXT NOT NULL,
    credit TEXT NOT NULL,
    equity TEXT NOT NULL,
    floating_net_profit TEXT NOT NULL,
    margin TEXT,
    margin_available INTEGER NOT NULL CHECK (margin_available IN (0, 1)),
    leverage INTEGER NOT NULL,
    CHECK (observed_from_ms <= observed_to_ms),
    CHECK ((margin_available = 1 AND margin IS NOT NULL) OR
        (margin_available = 0 AND margin IS NULL)),
    UNIQUE (account_id, id),
    FOREIGN KEY (sync_run_id, account_id) REFERENCES sync_runs(id, account_id)
) STRICT;

CREATE INDEX snapshots_account_time
    ON account_snapshots(account_id, observed_to_ms DESC, id);
CREATE INDEX snapshots_retention ON account_snapshots(observed_to_ms, id);

CREATE TABLE positions (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    snapshot_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    command INTEGER NOT NULL CHECK (command BETWEEN 0 AND 5),
    digits INTEGER NOT NULL,
    volume_hundredths INTEGER NOT NULL,
    open_time_wall INTEGER NOT NULL,
    close_time_wall INTEGER NOT NULL,
    open_price TEXT NOT NULL,
    current_price TEXT NOT NULL,
    sl TEXT NOT NULL,
    tp TEXT NOT NULL,
    profit TEXT NOT NULL,
    commission TEXT NOT NULL,
    swap TEXT NOT NULL,
    taxes TEXT NOT NULL,
    PRIMARY KEY (account_id, ticket),
    FOREIGN KEY (account_id, snapshot_id) REFERENCES account_snapshots(account_id, id)
) STRICT;

CREATE TABLE trades (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    ticket INTEGER NOT NULL,
    current_revision INTEGER,
    UNIQUE (account_id, ticket),
    UNIQUE (id, account_id),
    FOREIGN KEY (id, current_revision) REFERENCES trade_revisions(trade_id, revision)
) STRICT;

CREATE TABLE trade_revisions (
    trade_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    source_run_id TEXT NOT NULL,
    source_sha256 BLOB NOT NULL CHECK (length(source_sha256) = 32),
    native_currency TEXT NOT NULL,
    unit_revision INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    command INTEGER NOT NULL CHECK (command BETWEEN 0 AND 7),
    digits INTEGER NOT NULL,
    volume_hundredths INTEGER NOT NULL,
    open_time_wall INTEGER NOT NULL,
    close_time_wall INTEGER NOT NULL,
    open_price TEXT NOT NULL,
    close_price TEXT NOT NULL,
    sl TEXT NOT NULL,
    tp TEXT NOT NULL,
    profit TEXT NOT NULL,
    commission TEXT NOT NULL,
    swap TEXT NOT NULL,
    taxes TEXT NOT NULL,
    recorded_at_ms INTEGER NOT NULL,
    PRIMARY KEY (trade_id, revision),
    UNIQUE (source_run_id, trade_id),
    FOREIGN KEY (trade_id, account_id) REFERENCES trades(id, account_id),
    FOREIGN KEY (source_run_id, account_id) REFERENCES sync_runs(id, account_id)
) STRICT;

CREATE INDEX revisions_source_run ON trade_revisions(source_run_id, trade_id);

CREATE TRIGGER trade_identity_starts_unpublished
BEFORE INSERT ON trades
WHEN NEW.current_revision IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'insert identity without a current revision');
END;

CREATE TRIGGER trade_revision_requires_successful_run
BEFORE UPDATE OF current_revision ON trades
WHEN NEW.current_revision IS NOT NULL
BEGIN
    SELECT CASE WHEN NOT EXISTS (
        SELECT 1 FROM trade_revisions r JOIN sync_runs s ON s.id = r.source_run_id
        WHERE r.trade_id = NEW.id AND r.revision = NEW.current_revision
          AND r.account_id = NEW.account_id AND s.state = 'succeeded'
    ) THEN RAISE(ABORT, 'revision is not published') END;
END;

CREATE VIEW current_trades AS
    SELECT r.*, t.ticket FROM trades t JOIN trade_revisions r
    ON r.trade_id = t.id AND r.revision = t.current_revision;

CREATE TABLE trade_time_resolutions (
    trade_id TEXT NOT NULL,
    trade_revision INTEGER NOT NULL,
    time_ruleset_id TEXT NOT NULL REFERENCES server_time_rule_sets(id),
    opened_at_ms INTEGER,
    closed_at_ms INTEGER,
    event_at_ms INTEGER,
    event_time_basis TEXT NOT NULL CHECK (event_time_basis IN
        ('open_time', 'close_time', 'verified_override', 'unresolved')),
    quality TEXT NOT NULL CHECK (quality IN
        ('confirmed', 'unknown', 'ambiguous', 'nonexistent')),
    evidence_ref TEXT,
    resolved_at_ms INTEGER NOT NULL,
    PRIMARY KEY (trade_id, trade_revision, time_ruleset_id),
    FOREIGN KEY (trade_id, trade_revision) REFERENCES trade_revisions(trade_id, revision)
) STRICT;

CREATE INDEX resolved_close_time
    ON trade_time_resolutions(time_ruleset_id, quality, closed_at_ms, trade_id);
CREATE INDEX resolved_event_time
    ON trade_time_resolutions(time_ruleset_id, quality, event_at_ms, trade_id);

CREATE TABLE cash_flow_classifications (
    trade_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    based_trade_revision INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN (
        'unclassified', 'external_deposit', 'external_withdrawal',
        'rebate', 'fee', 'profit_share_income', 'profit_share_expense',
        'internal_transfer', 'capital_adjustment', 'credit')),
    transfer_group_id TEXT,
    reason TEXT NOT NULL,
    classified_at_ms INTEGER NOT NULL,
    PRIMARY KEY (trade_id, revision),
    FOREIGN KEY (trade_id, based_trade_revision) REFERENCES trade_revisions(trade_id, revision)
) STRICT;

CREATE INDEX classifications_transfer
    ON cash_flow_classifications(transfer_group_id, trade_id, revision);

CREATE TABLE sessions (
    token_sha256 BLOB PRIMARY KEY CHECK (length(token_sha256) = 32),
    auth_version INTEGER NOT NULL CHECK (auth_version > 0),
    created_at_ms INTEGER NOT NULL,
    last_interactive_at_ms INTEGER NOT NULL,
    idle_expires_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    revoked_at_ms INTEGER,
    CHECK (created_at_ms < expires_at_ms),
    CHECK (idle_expires_at_ms <= expires_at_ms)
) STRICT;

CREATE INDEX sessions_expiry ON sessions(expires_at_ms);
```

SQL 外键负责实体归属，业务事务还必须校验接入点核验状态、规则集归属、Decimal 格式、资金分类语义及调度状态转换。不得将建表成功当作这些业务约束已经实现。

第一阶段的实际迁移为 [001_phase1.sql](../internal/storage/migrations/001_phase1.sql)。`config_version` 用于拒绝旧任务发布，`next_sync_at_ms` 与 `failure_count` 保存到期计划和重试进度；成功快照首次确认美元单位时保存原币首次统计权益及纳入时间。

任务冗余保存 `server_id`，通过两条复合外键保证任务账户和接入点属于同一服务器；覆盖父子区间通过 `(parent_id, account_id)` 复合外键保证属于同一账户。第一阶段迁移仅包含快照业务，不引用尚未建立的历史表；第二阶段引入历史表及对应外键。

账户已核验的币种不随新响应静默改写；SDK 历史订单没有逐笔币种字段，币种或单位变化必须核验其生效范围后再发布历史和归一化结果，不能把新单位套到旧订单。

## 发布与修订事务

### 快照和当前持仓

完成一次成功的无历史 `Fetch` 后，在同一个短事务内插入 `account_snapshots`、替换该账户 `positions`、更新当前快照指针并结束对应 `sync_runs`。空持仓只有在完整成功时才替换旧集合。

事务提交前重查账户采集开关、固定接入点和凭据版本。期间发生修改则取消发布，不能把旧配置下完成的结果冒充新配置结果。`positions` 查询同时匹配 `accounts.current_snapshot_id`，防止混合不同快照。

### 历史分批暂存与原子发布

SDK 请求完整成功后，先比较规范字段摘要；同账户同订单内容未变化时不新增修订。变化的记录按每批最多500行写入 `trade_revisions`，尚无有效版本的新 `trades` 保持空指针，网页只查询 `current_trades`。

摘要按原始业务字段的固定顺序与规范 Decimal 字符串生成，排除抓取时间、运行 ID 等变化元数据。不能因为检查时间变化就为全部历史制造新修订。

全部暂存完成后，用一个事务将运行标记为成功、更新本次受影响订单的当前版本、推进覆盖状态，并触发统计失效。该事务失败则全部回滚，旧版本和覆盖状态保持不变；未发布的暂存修订可清理后重试。

运行进入终态后不再改为其他终态；重试建立新运行并关联 `retry_of_id`。提交返回状态不确定时先按运行 ID 查询已提交结果，不能直接将可能已成功的运行改成失败。禁止用 `INSERT OR REPLACE` 重建订单身份或当前指针。

成功发布所需的最小运行记录长期保留，不能因诊断保留期到期而破坏外键或修订来源。

`history_coverage.state` 描述区间的当前处理状态，`last_success_run_id` 与 `checked_at_ms` 保存上次成功事实。重查失败或二分时保留成功事实；覆盖集合按已成功发布事实与核验时点计算，不能仅过滤 `state = 'complete'` 而丢掉旧有效覆盖，也不能让请求终点代替已核验至时间。

### 时间和资金分类

`rules_json` 为规则区间数组，每项使用 `from_utc_ms`、`until_utc_ms`、`kind`、`zone`、`offset_seconds`、`tzdata_version` 和 `source_ref`。`kind` 为 `iana` 或 `fixed_offset`，按类型校验相应字段；有效期采用半开区间，区间不能重叠，历史无法归属唯一规则时保持未确认。规则集只追加版本，核验后切换服务器的活动规则集。

夏令时重复墙钟对应多个 UTC 候选时标记 `ambiguous`，不存在的墙钟标记 `nonexistent`。不能由 Go 默认选择一个候选后宣称确认。资金事件时间按已核验的记录语义确定，不能无条件套用成交平仓时间。

时间修正追加 `trade_time_resolutions`，不改写原始墙钟或原始订单版本；切换规则集后，旧解释不继续充当新规则下的已确认时间。相关日报、月报和资金流权重重新计算。

资金分类只追加修订。当前分类为最新分类修订，且其 `based_trade_revision` 必须匹配订单当前版本；源记录变化后重新确认，不能静默沿用旧分类。类别不改变原始金额，跟单分成收入、分成支出、返佣和提现分别处理。

内部转账需要核对两个账户、单位、方向、金额和实际时间。无法配平、费用尚未拆清或资金在途时保留未解决状态，不自动抵消；部分金额需要拆分时先保留待分类，避免把费用伪造成资本流出。

## 后续持久化扩展

| 实体 | 键与必需字段 | 引入条件 |
| --- | --- | --- |
| `membership_events` | 事件 ID、账户 ID、生效 UTC、纳入或移出、原币统计权益、单位版本、来源样本与归档引用 | 第三阶段前；第一阶段先保存首次有效纳入时间和估值 |
| `capital_adjustments` | 调整 ID、账户 ID、生效 UTC、金额、币种、资本或收益属性、原因、被修订事件及修订号 | 第三阶段，保留人工历史调整的证据和修订链 |
| `snapshot_rollups` | 账户、粒度、时区、桶起止、单位版本；各资金指标的首末值与极值、实际观测范围、样本数及缺口信息 | 任何在线分钟数据清理之前 |
| `snapshot_archives` | 归档 ID、格式版本、账户与时间范围、文件引用、原始行数、摘要、状态和备份确认时间 | 任何在线分钟数据清理之前 |

归档与降采样执行规则见[运行与验收规则](product-runtime.md)。归档尚未实现或验证失败时，不清除分钟样本；交易和分类修订长期保留。

## 数据模型验收

1. 同服务器重复 Login 被拒绝，不同服务器相同 Login 可并存。
2. 账户不能绑定另一个服务器的接入点，持仓不能引用另一个账户的快照。
3. 同账户不能并发运行两个 Fetch；各类任务重复排队被合并。
4. SDK 失败、数据库回滚或进程在发布前退出时，当前快照、订单视图及完整覆盖不被部分结果替换。
5. 历史重查无变化时不增加修订；新版本失败时旧版本仍可见，分类修订可追溯。
6. 未核验时区、夏令时歧义与未分类资金不能进入要求准确时间和资金语义的指标。
7. 改密后旧 `auth_version` 会话全部失效，数据库没有用户名、明文密码或原始会话令牌。

## 官方依据

- [SQLite STRICT 表](https://www.sqlite.org/stricttables.html)规定可用类型；金额 TEXT 的解析和精度规则由 Go 业务层执行。
- [SQLite 外键](https://www.sqlite.org/foreignkeys.html)需要在每个连接启用，组合外键必须引用完整唯一键。
- [SQLite WAL 文档](https://www.sqlite.org/wal.html)记录 WAL-reset 修复版本；生产驱动应核验其实际内嵌引擎版本。
