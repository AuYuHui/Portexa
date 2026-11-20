# 文档索引 / Documentation

[English README](../README.md) · [中文 README](../README.zh-CN.md) · [贡献指南 / Contributing](../CONTRIBUTING.md)

首次部署从启动指南开始，统计口径见下方各功能文档。

Start with the setup guide for deployment. The detailed guides are primarily in Chinese.

## 使用与部署 / Usage and deployment

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [启动与管理 / Getting started](product-start.md) | 构建、HTTPS、MT4 与 MT5 首次接入、日志、升级与恢复 |
| [服务器搜索与登记 / Server discovery](server-discovery.md) | 目录来源、原生 TCP 固定接入点及修订 |
| [账户统计 / Account statistics](account-statistics.md) | 当日净利润、胜负、保证金、历史回填与出入金分类 |
| [收益与风险分析 / Analytics](analytics.md) | 区间净收益、估算收益率、采样回撤、日历、账户比较与数据依据 |
| [资产走势 / Asset trends](asset-trends.md) | 组合与账户曲线、时间范围、单位、缺口及采样口径 |
| [休市采样与自动学习 / Market rest](market-rest.md) | 内置模板、报价学习、独立验证、降频及休市估值证据 |
| [桌面截图示例 / Desktop screenshots](screenshots.md) | 中英文界面、合成数值解释和重新生成步骤 |

## 开发与协议模块 / Development and SDKs

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [工程结构 / Architecture](architecture.md) | 目录、依赖方向、各模块的测试与构建 |
| [MT4 SDK](../packages/mt4/README.md) | `Probe`、`Fetch`、协议类型与资源限制 |
| [MT5 SDK](../packages/mt5/README.md) | 原生 `Probe`、`Fetch`、`FetchHistory` 与独立测试 |
| [MT4 验证命令 / Probe CLI](../cmd/mt4probe/README.md) | 联网探测、脱敏输出与离线样本校验 |
| [SQLite 迁移 / SQLite migrations](../internal/storage/migrations/) | 实际应用的版本化表结构与约束 |

## 历史验收与研究 / Validation records and research

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [验收记录 / Acceptance](phase1-validation.md) | 合成测试、浏览器验收和历史性能结果 |
| [协议研究索引 / Protocol research](research/README.md) | MT4 只读验收、MT5 原生握手研究及各自验证范围 |
| [协议实验 / Experiments](../research/README.md) | 独立的 MT4 与 MT5 协议实验 |
| [本地采集工具 / Capture tooling](../tools/mt4-capture/README.md) | 私有采集流程、范围限制和脱敏约束 |

## 早期设计与路线图 / Early design and roadmap

这些文档保留阶段目标和设计决策。其中的资源预算、SQL 原型和 API 草案应结合当时阶段阅读。

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [产品方案 / Product plan](product-plan.md) | 多账户看板的阶段目标、统计设计和归档规划 |
| [第一阶段实施规格 / Phase 1 specification](phase1-implementation.md) | 第一阶段登录、账户接入、采集与看板契约 |
| [SQLite 数据模型原型 / Data model prototype](product-data-model.md) | 键、外键、状态和历史原子发布约束 |
| [运行与验收设计 / Runtime design](product-runtime.md) | 调度和资源预算、凭据保护、备份与归档设计 |
