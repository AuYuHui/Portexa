# 工程结构

[English README](../README.md) · [中文 README](../README.zh-CN.md) · [文档索引](README.md)

Portexa 使用一个主工程和可独立使用的 MT4、MT5 Go 模块。模块路径按项目命名，不绑定个人托管账户。`go.work` 方便编辑器同时加载模块；主工程的本地 `replace` 让关闭 workspace 后也能构建。

```text
Portexa/
├── README.md / README.zh-CN.md   # 英文与中文公开入口
├── LICENSE / CONTRIBUTING.md    # MIT 许可证与贡献指南
├── cmd/portexa/                  # 产品服务与本地管理入口
├── cmd/mt4probe/                 # 联网验证命令入口
├── internal/appcli/              # 初始化、运行锁、启动、重置与备份恢复
├── internal/auth/                # 单密码认证、会话和撤销
├── internal/httpapi/             # 同源 API、Cookie、Origin、CSRF 与静态服务
├── internal/accounts/            # 可信接入、加密凭据管理和资金聚合
├── internal/analytics/           # Decimal 收益、采样回撤、日历与指标质量
├── internal/discovery/           # MT4、MT5 服务器目录、固定地址与来源证据
├── internal/marketrest/          # 休市模板与采集窗口学习
├── internal/syncer/              # 有界快照调度、SDK 适配与结果校验
├── internal/storage/             # STRICT 迁移、GORM、单写队列与在线备份
├── internal/logging/             # slog JSONL、大小/每日轮转、压缩与七天保留
├── internal/vault/               # 独立 AES-GCM 密钥与持久化加密计数
├── internal/cli/                 # 本地输入、参数和脱敏输出
├── web/                          # Vue 看板，静态构建到 .local/web
├── packages/mt4/                 # 独立模块 portexa/mt4
│   ├── LICENSE                  # 可随模块独立分发的 MIT 许可证
│   ├── THIRD_PARTY_NOTICES.md   # 固定版本依赖的许可原文
│   ├── client.go                 # Probe / Fetch，连接生命周期与上下文
│   ├── types.go                  # 对外协议数据类型
│   ├── internal/protocol/        # 编码、握手、认证、同步和解码
│   ├── internal/verification/    # 离线样本核对，不进入 SDK 运行依赖
│   └── tests/                    # 协议测试、公开接口测试和合成服务端
│       └── cmd/verify/           # 离线验证命令
├── packages/mt5/                 # 独立模块 portexa/mt5
│   ├── internal/protocol/        # MT5 原生 TCP 握手、同步与历史读取
│   └── tests/                    # 原生 TCP 合成协议测试
├── tests/                        # 主工程接口、同步、CLI、浏览器与性能验收
│   └── e2e/screenshots.py        # 独立合成服务生成公开桌面截图
├── tools/mt4-capture/            # 本地采集脚本
├── research/                     # MT4、MT5 协议实验，与运行代码隔离
├── docs/README.md                # 使用、工程与研究文档索引
├── docs/screenshots.md           # 截图示例及生成方法，图片位于 docs/screenshots/
├── docs/research/                # 历史研究、版本依据和验证边界
├── scripts/check_structure.py    # 目录、模块名及1000行限制
├── go.mod                       # 主工程 portexa
├── go.work                      # 本地多模块开发入口
└── Makefile                     # 统一构建与检查入口
```

## 依赖方向

`cmd/mt4probe → internal/cli → portexa/mt4 → internal/protocol`。协议模块不依赖主工程或业务代码，不读配置文件，不打印日志，也不落库。调用方传入凭据和上下文，取得类型化结果；命令行层只输出脱敏检查结果。

产品入口为 `cmd/portexa → internal/appcli`。HTTP 层调用 `auth` 和 `accounts`，调度层 `syncer` 通过注入的 Fetcher 调用协议模块。`accounts` 管理存储事务，`storage` 管理 GORM、SQLite 连接与写队列。网络等待在事务之外，快照发布前重新检查配置版本。前端接收脱敏 DTO。

快照调度根据已核验服务器的协议和固定接入修订选择原生 TCP 采集器。主工程负责 MT5 快照适配和净值估算。挂单与持仓共享快照，逐笔成交复用日结调度、覆盖窗口和历史修订的发布链路。[MT5 模块说明](../packages/mt5/README.md)

离线验证路径是 `packages/mt4/tests/cmd/verify → internal/verification → internal/protocol`。它读取私有样本且不联网，不能被当作运行中的连接器。`research/mt4-wire` 保留早期上游行为实验，有独立模块及测试，不作为原生协议依赖。

## 业务与独立模块

业务通过协议模块读取快照和历史，在主工程完成调度、存储、当日统计与资金分类。`internal/analytics` 消费适配后的样本与事件，负责 Decimal 计算、指标质量、收益日历及显示抽样。来源适配及修订校验在 `internal/accounts`。[收益风险分析](analytics.md)

`packages/mt4` 与 `packages/mt5` 均自带 Go 模块定义、文档、许可证、第三方声明和测试。关闭 workspace 后可独立验证。主工程使用 `go.mod` 中的本地 `replace` 解析 `portexa/mt4` 和 `portexa/mt5`。

根目录英文 README 面向首次使用者，中文版本保留同等的功能、启动和许可说明；详细操作与设计文档通过[文档索引](README.md)区分。公开桌面截图只从独立合成服务生成，方法见[截图说明](screenshots.md)，历史线上证据继续保留原有日期和边界。

## 测试与约束

`make check` 运行结构检查、各模块独立 Go 测试和 `go vet`、Python 测试及前端类型、翻译和构建检查。主工程与采集工具测试在根目录 `tests`，协议测试在各模块的 `tests`，研究测试在对应的 `research/*/tests`。浏览器、CLI 全流程与历史发布性能可单独运行。[历史验收与复现命令](phase1-validation.md)

源码、测试、脚本和文档单文件上限均为1000行；本地数据、依赖缓存及构建产物由 Git 忽略。npm 生成的 `web/package-lock.json` 保留标准排版和完整依赖锁，结构检查仅对这个生成锁文件豁免行数。真实材料只留在私有目录，历史线上证据保留原日期，不能因产品开发改写为本次线上结果。
