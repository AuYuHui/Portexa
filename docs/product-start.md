# 第一阶段启动与管理

当前交付单用户登录、可信账户接入、每60秒快照、总览和持仓看板。协议 SDK 冻结，历史同步、收益率、回撤与分钟归档尚未交付。验收仅使用合成数据，未登录真实账户。

## 构建和本地启动

开发需要 Go 1.26+、Node 22.12+（或20.19+）、Python 3；运行服务只需 Go 二进制、前端静态目录和私有运行目录。支持 macOS / Linux。

```sh
make web-install
make check
make build
.local/bin/portexa init
.local/bin/portexa serve -dev-http
```

初始化在终端交互输入两次系统密码，至少16个 Unicode 字符，无用户名、角色或默认密码。已有系统不能重复初始化。非交互管理可用 `init -password-stdin` / `password-reset -password-stdin` 从安全标准输入管道读取，不能把密码写在命令行参数或提交到脚本。

本地浏览器打开 `http://127.0.0.1:8080`。HTTP 开发模式必须显式开启且只监听环回地址，不改变 MT4 接入策略。需要热更新时，另运行 `npm --prefix web run dev`，Go 服务添加 `-origin http://127.0.0.1:5173`，由 Vite 代理 API；Origin/Host 必须与浏览器来源一致。

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

## 第一次账户接入

1. 在“接入与设置”登记经纪商、准确服务器名、固定公开 IP 和端口。登记本身不发送凭据。
2. 通过独立官方渠道核验地址及名称，保存私有证据。表单登记证据引用与 SHA-256；可本机选择文件计算摘要，文件内容不上传。
3. 阅读并确认该修订的剩余风险。冻结 SDK 在服务端身份确认前会发送密码派生材料；固定 IP、显示名称和成功认证均不能替代密码学服务端身份认证。部署侧另行设置目的 IP/端口出站允许列表。
4. 接入账户，填写 MT4 登录号、只读投资者密码和明确的服务器修订；投资者密码按 SDK 限制为1至16个 UTF-8 字节，与系统密码不同。
5. 明确确认单位，启用采集。USD 按1:1、USC 按100:1折算美元，其他币种单列；采集币种与确认不符时停止换算。原币始终保留。

调度最多10个快照 Fetch、每账户最多1个，周期60秒并按账户分配相位。首次批量接入与稳态采集分开看待；手动采集和定时需求合并。网络错误按60/120/300秒退避；拒绝、协议不支持或资源超限时暂停，核对后再恢复。

若无法读取或持久化任务状态，服务会停止而非留下永久执行锁；检查存储后由进程监管重启，启动恢复会将遗留运行标记中断并重建到期需求。

总览标注最近已知小计、纳入范围及采样时间范围，缺失不当零，超过3分钟过期。详情区分持仓与挂单。服务器时区登记不等于核验，原始墙钟不用于日报或月报。SDK float64 转 Decimal 无法恢复上游已损失的精度；转换后聚合不提前舍入，API 金额为字符串。

地址或核验来源变更使用“新建接入修订”，旧修订停用、账户暂停。新修订重新核验并确认风险后，进入账户配置明确重新绑定、重加密并启用，旧任务不能跟随新地址。

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

保留层按首条日志时间清理当前文件及压缩归档，避免 lumberjack 按归档轮转时间计龄而延长旧记录寿命。空闲时清理也继续运行。该窗口限制保留时间，七天内的日志文件总大小取决于实际日志量。

## 备份、恢复和重置

服务使用 SQLite Online Backup API，每日和每周生成并验证一致副本，保留7份日备份与4份周备份。新备份失败不会清理旧有效副本；每5分钟做低优先级 PASSIVE checkpoint。总览显示备份失败或 WAL 积压异常。不会自动把备份发送到外部服务。

本地管理命令持有同一运行锁，先停止主服务再执行。`-file` 输出必须为新文件；恢复必须使用新的空运行目录，避免覆盖现有数据库。

```sh
.local/bin/portexa backup -data /path/to/private-data -file /path/to/new-backup.sqlite
.local/bin/portexa restore -data /path/to/new-private-data -file /path/to/new-backup.sqlite
.local/bin/portexa password-reset -data /path/to/new-private-data
```

恢复先验证应用版本、完整性和外键，再以临时库发布；旧会话全部撤销，遗留任务标记中断，采集默认暂停。恢复匹配密钥和使用计数后，核对当前接入点允许列表再在网页逐一启用。数据库恢复不自动恢复或生成密钥。

当前保留所有分钟快照与成功运行来源，不进行未归档清理。90天分钟、730天小时和长期日汇总的归档策略会在后续阶段实施；上线需监控磁盘空间。备份和性能验收复现见[验收记录](phase1-validation.md)。
