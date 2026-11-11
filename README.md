# Portexa

多账户只读聚合系统。第一阶段提供单用户密码登录、可信服务器与账户接入、周期快照和浏览器看板。主工程使用 Go、GORM、SQLite 及 Vue；MT4 协议 SDK 可独立使用，业务分类留在主工程，无需 MT4 终端、Wine、EA 或第三方账户服务。

## 工程入口

| 路径 | 职责 |
| --- | --- |
| cmd/portexa、internal/appcli | 产品服务、本地初始化、密码重置、备份恢复 |
| internal/accounts、internal/syncer | 可信账户管理、快照采集、Decimal 聚合 |
| internal/auth、internal/httpapi、internal/vault | 会话、受保护 API 与独立凭据密钥 |
| internal/storage | STRICT 迁移、GORM 映射、单写队列与在线备份 |
| internal/logging | JSONL 文件日志、大小及每日轮转、gzip 压缩与七天保留 |
| web | Vue 3 / TypeScript 中英文账户看板 |
| [packages/mt4](packages/mt4/README.md) | 独立协议模块 portexa/mt4 |
| [cmd/mt4probe](cmd/mt4probe/README.md) | 联网验证命令，输出脱敏结果 |
| packages/mt4/tests、tests | 协议、公开接口、CLI 和采集测试 |
| [tools/mt4-capture](tools/mt4-capture/README.md) | 本地采集工具 |
| [research](research/README.md) | 保留的早期实验 |
| [docs/architecture.md](docs/architecture.md) | 目录、依赖方向及独立发布方式 |
| [docs/product-plan.md](docs/product-plan.md) | 多账户聚合产品方案、Analytics 口径及单用户登录规则 |
| [docs/product-start.md](docs/product-start.md) | 第一阶段启动、部署与本地管理 |
| [docs/account-statistics.md](docs/account-statistics.md) | 当日胜负、保证金水平、历史回填与自动识别出入金 |
| [docs/phase1-validation.md](docs/phase1-validation.md) | 合成验收与历史发布性能结果 |
| [docs/research](docs/research/README.md) | 验证记录与协议研究依据 |

模块按项目命名，不绑定个人托管账户。单文件不超过1000行，结构检查会自动执行此限制。

## 开发与验证

需要 Go 1.26+、Node 22.12+ 和 Python 3。仓库根目录执行：

```sh
make web-install
make check
make build
.local/bin/portexa init
.local/bin/portexa serve -dev-http
```

浏览器访问 `http://127.0.0.1:8080`。系统密码至少16个字符，无用户名。生产部署使用 HTTPS 来源及反向代理，详见[启动说明](docs/product-start.md)。产物包含 `.local/bin/portexa`、`.local/bin/mt4probe` 和 `.local/bin/mt4verify`；产品运行时不需要 Node。

运行日志默认写入 `.local/portexa/logs/portexa.jsonl`，单文件上限100 MiB，轮转后 gzip 压缩，保留窗口七天。可用 `serve -log-max-size 50` 调整大小上限；保留和脱敏规则见[日志说明](docs/product-start.md#jsonl-运行日志)。

普通检查仅运行合成与回环测试，不登录真实服务。已支持按 MT4 服务器日期展示当日利润、平仓笔数、胜率和盈利/亏损，以及账户保证金水平；可返回历史按窗口回填，出入金根据明确备注自动识别，支持人工校正。原币保留，未覆盖和待识别数据明确标记；收益率、回撤和分钟归档仍属后续阶段，当前不会清理未归档的分钟快照。

看板支持简体中文与英语。首次访问按浏览器首选语言选择，不匹配时使用中文；登录页与顶部均可切换，选择保存在当前浏览器。日期随界面语言格式化，账户名称、原始备注、金额精度和服务器墙钟含义保持不变。

界面运行时使用 [Vue I18n](https://vue-i18n.intlify.dev/)，翻译资源位于 `web/src/locales/{locale}.json`。[x18n](https://github.com/x18n-dev/x18n) 用于翻译检查与管理：`npm --prefix web run i18n:check` 检查缺失、未使用的键及插值参数；`npm --prefix web run i18n:ui` 打开管理界面。`make check` 已包含翻译检查。新增语言需同时注册运行时语言、切换选项和 `web/x18n.config.json` 中的语言列表；接口现有文本提示通过本地资源映射，不改写业务记录。

协议模块也能独立检查：

```sh
GOWORK=off go -C packages/mt4 test ./...
```

当前已完成 EC Markets、DPrime、DLSMarkets 的原生只读兼容性实测及官方字段对照，范围与方法见[本轮验收记录](docs/research/mt4-readonly-acceptance.md)；早先 Linux 运行保留在[历史验证报告](docs/research/mt4-native-validation.md)。凭据、接入地址、设备材料、抓包和真实数值均不进入源码、文档或测试。
