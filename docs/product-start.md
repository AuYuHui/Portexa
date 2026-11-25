# Portexa 启动与管理

[English README](../README.md) · [中文 README](../README.zh-CN.md) · [文档索引](README.md) · [桌面截图示例](screenshots.md)

Portexa 支持 MT4、MT5 多账户只读采集。历史回填、当日统计和资金分类见[账户统计](account-statistics.md)，净收益、估算收益率、采样回撤及日历见[收益分析](analytics.md)。

## 构建和本地启动

支持 macOS / Linux。构建需要 Go 1.26+、Node.js 22.12+ 和 npm，仓库检查还需要 Python 3。服务运行需要 Go 二进制、前端静态目录和私有运行目录。

```sh
git clone https://github.com/AuYuHui/Portexa.git
cd Portexa
make web-install
make build
.local/bin/portexa init
.local/bin/portexa serve -dev-http
```

以上命令在仓库根目录执行。`make build` 生成 `.local/bin/portexa`、`.local/bin/mt4probe`、`.local/bin/mt4verify`，前端构建到 `.local/web/`。部署时同时携带服务程序与静态目录。完整开发检查见[贡献指南](../CONTRIBUTING.md#local-setup)。

休市期间可在“接入与设置 → 采集设置 → 休市降频”选择内置模板或关闭，并设置例外账户。适用条件、保护窗口、估值证据和升级权限检查见[休市降频与估值](market-rest.md)。

初始化在终端交互输入两次系统密码，至少16个 Unicode 字符，无用户名、角色或默认密码。已有系统不能重复初始化。非交互管理可用 `init -password-stdin` / `password-reset -password-stdin` 从安全标准输入管道读取，不能把密码写在命令行参数或提交到脚本。

本地浏览器打开 `http://127.0.0.1:8080`。HTTP 开发模式必须显式开启且只监听环回地址。需要热更新时，另运行 `npm --prefix web run dev`，Go 服务添加 `-origin http://127.0.0.1:5173`，由 Vite 代理 API；Origin/Host 必须与浏览器来源一致。

## 生产 HTTPS

先运行 `init -data /path/to/private-data`，再将 Go 服务放在 HTTPS 反向代理后：

```sh
.local/bin/portexa serve \
  -data /path/to/private-data \
  -web /path/to/built-web \
  -listen 127.0.0.1:8080 \
  -origin https://portexa.example.com
```

HTTPS 来源必须精确匹配，不含尾斜线或路径。反向代理转发原始 `Host` 和 `Origin`，将浏览器 API 与静态资源保持同源；配置有效证书和正常的服务进程监管。默认绑定环回，不将内部 HTTP 端口暴露给公网。仅静态资源、登录流程和最小 `/healthz` 可匿名访问，业务接口一律要求有效会话。

登录 Cookie 为 HttpOnly、Secure、SameSite=Strict，空闲30分钟、绝对8小时过期。后台轮询不续期，用户实际操作按分钟节流续期。改密撤销所有会话，失败登录按来源和全局限流。接口错误和服务日志不输出密码、密文或 SQL 参数。

## MT5 采集运行依赖

标准构建可直接部署看板和 MT4 采集。MT5 采集另外需要挑战计算程序及配套终端镜像。仓库提供 Linux x86_64 构建工具，镜像由部署者准备。

### 在 Linux x86_64 构建

1. 在 Apple 芯片 Mac 上，从 [App Store 的 MetaTrader 5 页面](https://apps.apple.com/us/app/metatrader-5/id413251709)取得受支持的 iPhone / iPad 版。当前支持 **5.0.6300（5251）**，以 SHA-256 `02b9f6103789bc652eb179264e99d8697b2a33d97ed6f06dabef3e83fc28a161` 为准。镜像是应用包内的 `Wrapper/MetaTrader5Terminal.app/MetaTrader5Terminal` 可执行文件。
2. 将镜像复制到 Linux x86_64 的私有目录，例如 `/opt/portexa/mt5/terminal-image`。构建需要 Python 3、能解析 ARM64 Mach-O 的 `llvm-objdump`、C 编译器及静态 C 库，已用 LLVM 20 和 GCC 14 验证。
3. 从仓库根目录构建。工具先核验镜像，再生成程序并执行离线自检。默认产物为 `.local/mt5-worker/compute`。系统使用版本化工具名时，可传入 `--objdump llvm-objdump-20`。

```sh
python3 scripts/mt5-worker/build.py --image /opt/portexa/mt5/terminal-image
```

### 配置服务并确认采集

将程序和镜像放在前端静态目录之外，给服务账户计算程序的执行权限和镜像的读取权限。在配置服务前，用服务账户检查安装后的实际路径。`--check` 只需要 Python 3，不使用编译器或网络。

```sh
python3 scripts/mt5-worker/build.py --check \
  --image /opt/portexa/mt5/terminal-image --output /opt/portexa/mt5/compute
```

如果使用 systemd，运行 `sudo systemctl edit portexa.service`，将下列绝对路径替换为实际部署路径：

```ini
[Service]
Environment="MT5_COMPUTE_WORKER=/opt/portexa/mt5/compute"
Environment="MT5_TERMINAL_IMAGE=/opt/portexa/mt5/terminal-image"
```

保存后运行 `sudo systemctl restart portexa.service`，到 MT5 账户详情确认已生成完整快照。升级服务程序或静态目录时，保留配套文件和服务环境配置。HTTPS 健康检查成功只表示 Web 服务在线。如果 MT5 尚无完整快照且显示“响应无法完整解析”，先用服务账户复查上述离线自检，再检查券商连接和响应。

## 第一次账户接入

1. 在“接入与设置 → 登记服务器”选择 MT4 或 MT5，搜索经纪商或准确的服务器名称。选中要登记的采集点，展开“查看接入地址与来源”可核对目录来源。
2. 核对经纪商、服务器和固定接入点，确认本次修订的连接风险。MT4 使用 MetaQuotes 目录，MT5 使用第三方 MTAPI 目录，交易连接均使用原生 TCP。[目录来源与核验规则](server-discovery.md)
3. 目录无可用地址时，打开“手动填写（高级）”，填写经纪商、准确的服务器名称和已核验的固定 IP、端口。登记后提供独立证据并确认风险。部署侧可按核验结果设置目的 IP 与端口出站允许列表。
4. 选择“接入账户”，填写对应平台的登录号和投资者只读密码，选择一个已核验的主采集点和最多两个备用点。MT4 投资者密码为 1–16 个 UTF-8 字节，MT5 为 1–32 个 UTF-16 代码单元。
5. 确认单位并启用采集。USD 按 1:1、USC 按 100:1 折算为美元，其他币种单列。采集币种与确认单位不符时停止换算，原币金额仍保留。

MT5 自动识别 D Prime 的 Doo Technology Singapore MT5 Demo 和 Live 服务器，按美国夏令时采用 UTC+2 / UTC+3。首次登记或启动补齐的首条官方规则，从有公告依据的 2024-11-04 起解释历史成交。[MT5 接入说明](https://help.dooprime.com/hc/en-gb/articles/15572865365007-About-MT5) · [2024 年平台时间公告](https://www.dooprimenews.com/service-updates/trading-hours/trading-hours-adjustment-for-us-time-shift)

缺少已识别规则的 MT5 服务器，在服务器卡片的“服务器时间”中手动确认经纪商时区，供成交回填、当日统计、资金分类和收益分析使用。首次手动确认可填入有依据的历史生效日期。MT4 的校准流程见[当日利润与结余](#当日利润与结余)。

## 采集与接入修订

调度最多10个快照 Fetch、每账户最多1个，默认周期60秒并按账户分配相位。首次批量接入与稳态采集分开看待；手动采集和定时需求合并。快照首次超时或断连后3秒重试一次，连续失败再按60/120/300秒退避，上限300秒，成功后重置失败计数。其他快照错误及历史任务仍按60/120/300秒退避；拒绝、协议不支持或资源超限时暂停，核对后再恢复。快速重试减少短暂故障造成的空白，但不保证恢复前的分钟估值完整，也不放宽90秒估值有效期。

在“接入与设置 → 采集设置”统一选择30、60、120或300秒，保存到数据库后无需重启。总览显示实际配置，页面每10秒读取一次看板；当日统计与历史回填仍按独立计划运行。修改周期只调整正常启用账户的后续快照，不恢复暂停账户、不绕过失败退避，也不推迟已到期或手动请求的采集。

每次快照完成后至少预留27秒，供历史任务获得20秒执行预算、5秒快照保护及调度余量；30秒档遇到较慢采集时，下一轮快照会顺延。快照过期阈值为配置周期的3倍，至少3分钟；修改周期不改变已有采样的实际时间或历史交易时间校准证据的3分钟上限。300秒周期通常不能形成这一观察窗口，未识别的服务器可在“服务器时间”中手动校正；已有官方时间规则仍可使用。

北京时间23:58至次日00:02临时使用30秒采集档，调度尽量对齐每分钟27秒、57秒，让短请求有机会在零点前完成。较长周期会提前唤醒进入此窗口；历史任务仍保留27秒执行窗口，采集结束后按原配置恢复。此策略不绕过账户暂停或网络失败退避，也不保证成交密集、请求缓慢时能取得有效零点估值。收益日历展示每天已入账的交易盈亏，点开日期可查看具体订单及每笔净额。缺少日界估值时，已确认的交易盈亏仍可展示。

若无法读取或持久化任务状态，服务会停止而非留下永久执行锁；检查存储后由进程监管重启，启动恢复会将遗留运行标记中断并重建到期需求。

总览标注最近已知小计、纳入范围及采样时间范围，缺失不当零，超过配置周期的3倍（至少3分钟）标记过期。详情区分持仓与挂单。服务器时区登记不等于核验，原始墙钟不用于日报或月报。SDK float64 转 Decimal 无法恢复上游已损失的精度；转换后聚合不提前舍入，API 金额为字符串。

在服务器卡片用“添加采集点”登记新地址。新增后，在账户配置绑定希望使用的备用点。断连或超时会先尝试已绑定备用点，再进入常规快照重试和退避。账户详情显示上次完整快照使用的采集点。[接入修订规则](server-discovery.md#登记与修订)

在账户详情点击“删除账户”并确认，Portexa 会停止采集、清除当前数据库中保存的投资者凭据，并将账户移出总览和当前资产组合。既有采集记录继续用于历史统计。再次接入同一交易账号时，会创建新的账户记录和组合纳入时间。

## 当日利润与结余

MT4 支持自动时间校准和手动校正。MT5 使用已匹配的券商规则或手动确认的服务器时区，按成交归集当日统计。两种协议的统计口径见[账户统计](account-statistics.md)。

MT4 通过新平仓事件自动校准当前服务器时差：上一轮仍明确持仓、本轮消失，并在完整历史中确认同一笔平仓，才能与两次采集的 UTC 时间窗口交叉判断。连续至少3笔不同订单、覆盖至少2轮采集且时差一致后自动采用，未知经纪商也适用；历史旧订单、挂单取消、超过3分钟的采集空窗及不完整响应不计入。

校准证据不足时使用已收录官方规则初始化和兜底。当前支持 D Prime / Doo Prime 的已知 MT4 服务器系列：同时匹配经纪商和服务器名称，自动应用冬令时 UTC+2、夏令时 UTC+3，并随美国夏令时切换。新增服务器登记和服务启动时自动补齐，用户无需填写时区、依据或勾选确认。规则来源为 [D Prime 官方平台时间公告](https://www.dooprimenews.com/service-updates/trading-schedule-update-us-daylight-saving-time-shift-and-all-souls-day)。

MT4 没有统一的交易时区；例如 [Exness 官方服务器使用 UTC+0](https://get.exness.help/hc/en-us/articles/4405235684498-Instrument-trading-hours)。现有 SDK 尚未暴露已验证的实时服务器时钟；事件校准只确认当前 UTC 偏移，不推断全年的夏令时规则，也不使用本机时区、IP 地理位置或旧订单时间猜测。测量支持 UTC−12至UTC+14的15分钟整数倍偏移。

在“接入与设置”点击“服务器时间”可查看采用的规则、校准笔数和最近校准时间。缺少新事件且没有官方规则时显示“时间校准中”，无需选择时区。手动校正折叠在“高级”中，只需填写服务器时区后保存；已保存的手动校正优先保留，可一键“使用自动识别”恢复事件校准或官方规则，也可恢复等待新事件。美国与欧洲夏令时切换日期不同，不能用 `Europe/Helsinki` 代替跟随美国夏令时的交易服务器规则。

事件证据以实际持仓消失时间为基准，默认24小时有效。已识别外汇、黄金或白银的已接受校准观察，按内置纽约周五18:00至周日16:00的休市窗口暂停老化，开市后继续累计剩余有效时间；未知和加密品种保持24小时墙钟期限，不自动推断节假日。休市前已经失效的证据不会复活，重查同一笔历史不会延期。开市后新成交偏移一致则续期、保持原规则版本；现存证据有效期内偏移冲突时重新累计，并撤销未经开市成交确认的跨休市时间延续，保留休市前已确认区间。已知夏令时切换仍截断旧偏移；到期时回落官方规则，没有可靠规则时暂停当日归属统计。账户配置变更后不会沿用旧配置的事件证据。校准历史与当日历史共用2个采集名额、同账户互斥，并在资金快照到期时让位；校准失败不暂停资金快照。

服务器时间就绪后，总览账户表和账户详情显示该服务器自然日的两项原币统计：

- **当日利润**：按平仓时间落入当日 `[00:00, 次日00:00)` 的 buy/sell，合计 `Profit + Commission + Swap + Taxes`；开仓日不影响平仓归属，删除的挂单不计入。
- **当日结余**：当日利润加当日 balance 记录的净额，表示该日余额变动。credit 单列，不混入结余；不将正 balance 自动认定为外部入金。

USC 始终保留美分金额，这两项不会再除以100。时间范围使用原始服务器墙钟，页面同时标明服务器日期、采用的时区规则和实际采样完成时间；数值是截至该次采样的当日结果。

当日历史默认每5分钟采集，可在详情点击“刷新当日”。历史独立使用2个 Fetch 名额，资金与持仓保留10个名额；同账户不重叠，距离下一轮快照至少25秒才启动，在快照前5秒结束或让位。历史失败不延后资金快照，部分响应或覆盖缺口不发布；明确完整的空历史才显示零。10分钟未更新标记过期，同日失败保留上次成功结果，跨日、币种单位或时区规则变化后等待对应的新结果。

日报保留当日汇总及采集事实；版本5、6另外增加独立历史回填、备注自动分类及人工校正，并展示平仓笔数、胜率、盈利/亏损及累计出入金，口径与操作见[账户统计](account-statistics.md)。日报自身的单日请求超出 SDK 资源或期限时保留失败状态和上次结果；独立历史回填会自动细分过大的窗口，但不会用其不同采样时刻的数据拼接日报。独立的[Analytics](analytics.md)按已核验规则解释历史 UTC、计算估算收益率；歧义时间仍不可用，不改变日报的服务器日归属。口径依据见 [MT4 官方历史说明](https://www.metatrader4.com/en/trading-platform/help/overview/terminal/terminal_account_history)。

## 私有数据和密钥

默认目录：

```text
.local/portexa/
├── .portexa.lock
├── db/portexa.sqlite             # WAL 数据库，禁止热复制主文件代替备份
├── keys/credentials.key          # 独立32字节密钥，0600
├── keys/credentials.key.usage    # 持久化加密次数，0600
├── logs/portexa.jsonl            # JSONL 运行日志，轮转归档同目录
└── backups/                      # 数据库副本，不含密钥
```

目录默认0700，数据库和密钥0600。`-key-file` 可将密钥指定到数据库目录以外的私有位置。密钥及 `.usage` 单独备份，不与数据库打包或外发；`.usage.lock` 只是运行锁，不是恢复材料。恢复不得降低密钥的已知使用次数，不完整时停止加密而不是自动重建。密钥缺失、不匹配或认证失败时暂停对应采集，保留旧数据；不要运行初始化覆盖密钥。

## JSONL 运行日志

使用 Go 标准库 `log/slog` 编码 JSONL，配合 [lumberjack v2.2.1](https://github.com/natefinch/lumberjack) 处理大小轮转和 gzip。它提供独立的 `io.Writer`，无需再引入完整日志框架；[Timberjack](https://github.com/DeRuina/timberjack) 也支持定时轮转，本工程采用 lumberjack 加小型定时保留层。

| 项目 | 规则 |
| --- | --- |
| 位置 | `DATA/logs/portexa.jsonl`，随 `-data` 调整 |
| 格式 | 每行一个 JSON 对象，时间统一 UTC，默认 INFO 及以上 |
| 单文件上限 | 默认100 MiB，`-log-max-size N` 指定正整数 MiB；写入将超限时先轮转 |
| 时间轮转 | 运行中至少每24小时检查轮转，低日志量或空闲服务也会轮转非空文件 |
| 归档 | `portexa-UTC时间戳.jsonl.gz`，异步 gzip；当前文件保持 JSONL 便于跟踪 |
| 保留窗口 | 固定7×24小时，按首条记录时间整文件删除；边界文件中的较新记录可能提前删除 |
| 清理时机 | 每次命令启动及运行中每分钟检查，过期删除有至多一个检查周期的延迟；停机期间由下次启动补清理 |
| 权限与并发 | 目录0700，日志0600，与产品运行目录共用进程锁 |

例如：`.local/bin/portexa serve -dev-http -log-max-size 50`。本地初始化、重置密码、备份和恢复也写入同一日志。日志目录不可写时命令启动失败；运行中的写入或清理错误输出脱敏 JSON 诊断到标准错误。

记录命令开始、完成或失败、服务启停、HTTP 状态和耗时、采集开始及结果、稳定错误分类、数据库维护异常。HTTP 记录路由模板，例如 `GET /api/accounts/{id}`；不记录完整 URL、请求体、Cookie、令牌、客户端地址或任意底层错误文本。采集仅记录本地账户/任务 ID 和错误分类，不记录登录号、接入地址、凭据、原始报文、资金或持仓。协议 SDK 不引入日志依赖，ORM SQL 日志保持关闭。

快照 `sync_started` 包含 `attempt`、`due_at_ms` 和 `schedule_delay_ms`；`sync_failed` 保留错误码、失败阶段、尝试次数和耗时。重试排期成功写入数据库后才记录 `sync_retry_scheduled`，包含 `failure_count`、`retry_delay_ms` 和 `next_retry_at_ms`（UTC Unix 毫秒）；暂停、取消、配置变化或写入失败不记录该事件。成功日志 `sync_completed` 包含 `recovered` 和 `previous_failure_count`，可串联核对失败、排期、实际重试及恢复。

MT5 当日统计和历史回填通过 `mt5_history_phase` 记录 `connect`（TCP 连接）、`handshake`（登录握手）、`account_sync`（账户同步）与 `deals`（成交收取及解析）。每条包含该阶段耗时、采集点修订号、尝试序号、连接准备预算和结果，失败时附错误码。按 `run_id` 可关联 `collection_endpoint_fallback`、`daily_sync_failed` 或 `history_sync_failed`。

保留层按首条日志时间清理当前文件及压缩归档，避免 lumberjack 按归档轮转时间计龄而延长旧记录寿命。空闲时清理也继续运行。该窗口限制保留时间，七天内的日志文件总大小取决于实际日志量。

## 备份、恢复和重置

升级时先备份数据库并单独保存匹配的密钥及使用计数，再同时更新服务二进制和前端产物。重启时按编号执行数据库迁移，当前数据库版本为 21。版本 21 记录 MT5 历史查询的实际覆盖终点。版本 20 增加备用采集点绑定和最近成功采集点。版本 19 保存当日单笔平仓净盈亏极值。版本 18 开始保存持仓采样极值。版本 17 重新排队回填 MT5 历史。版本 16 允许已删除的交易账号再次接入。版本 15 移除旧 WSS 接入配置，停用对应接入修订并暂停绑定账户。受影响账户需新建并核验原生 TCP 接入点，重新绑定后启用采集。[迁移文件](../internal/storage/migrations/)

服务使用 SQLite Online Backup API，每日和每周生成并验证一致副本，保留7份日备份与4份周备份。新备份失败不会清理旧有效副本；每5分钟做低优先级 PASSIVE checkpoint。总览显示备份失败或 WAL 积压异常。不会自动把备份发送到外部服务。

本地管理命令持有同一运行锁，先停止主服务再执行。`-file` 输出必须为新文件；恢复必须使用新的空运行目录，避免覆盖现有数据库。

```sh
.local/bin/portexa backup -data /path/to/private-data -file /path/to/new-backup.sqlite
.local/bin/portexa restore -data /path/to/new-private-data -file /path/to/new-backup.sqlite
.local/bin/portexa password-reset -data /path/to/new-private-data
```

恢复先验证应用版本、完整性和外键，再以临时库发布；旧会话全部撤销，遗留任务标记中断，采集默认暂停。恢复匹配密钥和使用计数后，核对当前接入点允许列表再在网页逐一启用。数据库恢复不自动恢复或生成密钥。

当前保留所有周期快照与成功运行来源，不进行未归档清理。90天分钟、730天小时和长期日汇总的归档策略会在后续阶段实施；上线需监控磁盘空间。备份和性能验收复现见[验收记录](phase1-validation.md)。
