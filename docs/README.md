# 文档索引 / Documentation

[English README](../README.md) · [中文 README](../README.zh-CN.md) · [贡献指南 / Contributing](../CONTRIBUTING.md)

英文 README 提供功能介绍、截图、快速启动和开发入口。详细操作与工程文档目前以中文为主；产品方案和研究记录包含规划或历史证据，不代表所有内容均已交付。

The English README covers features, desktop screenshots, setup, and development. Detailed guides below are primarily in Chinese. Planning documents describe future work as well as current behavior; research records retain their original validation scope and dates.

## 使用与部署 / Usage and deployment

建议依次阅读启动说明、截图示例和统计口径，再按需查看接入规则。

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [启动与管理 / Getting started](product-start.md) | 构建、HTTPS、首次接入、私有数据、日志、备份与恢复 |
| [桌面截图示例 / Desktop screenshots](screenshots.md) | 中英文界面、合成数值解释和重新生成步骤 |
| [账户统计 / Account statistics](account-statistics.md) | 当日净利润、胜负、保证金、历史回填与出入金分类 |
| [资产走势 / Asset trends](asset-trends.md) | 组合与账户曲线、时间范围、单位、缺口及采样口径 |
| [服务器发现 / Server discovery](server-discovery.md) | 候选目录、独立核验、风险接受及地址变更 |
| [MT4 SDK](../packages/mt4/README.md) | `Probe` / `Fetch`、协议类型、错误和资源限制 |
| [验证命令 / Probe CLI](../cmd/mt4probe/README.md) | 私有输入、联网探测、脱敏输出与离线校验 |

## 工程与设计 / Engineering and design

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [工程结构 / Architecture](architecture.md) | 目录、依赖方向、测试与 SDK 独立分发 |
| [实施规格 / Implementation](phase1-implementation.md) | 登录、采集和看板契约，及后续已交付扩展 |
| [数据模型 / Data model](product-data-model.md) | 键、外键、状态、版本和迁移 |
| [运行规则 / Runtime](product-runtime.md) | 调度、资源配额、凭据、备份及计划中的归档规则 |
| [产品方案 / Product plan](product-plan.md) | 产品边界、统计定义与后续收益率/回撤规划 |

## 验证与研究 / Validation and research

| 文档 / Guide | 内容 / Contents |
| --- | --- |
| [验收记录 / Acceptance](phase1-validation.md) | 合成测试、浏览器验收和历史性能结果 |
| [协议研究索引 / Protocol research](research/README.md) | 协议依据、已验证经纪商范围和历史线上证据 |
| [早期实验 / Experiments](../research/README.md) | 隔离的候选库与报文重放实验，不属于生产运行依赖 |
| [本地采集工具 / Capture tooling](../tools/mt4-capture/README.md) | 私有采集流程、范围限制和脱敏约束 |

公开示例只使用合成数据。普通检查不连接真实 MT4 服务；复用历史证据时保留原日期，不将其称为本次运行。开源许可见根目录 [MIT License](../LICENSE)，SDK 第三方许可见[声明](../packages/mt4/THIRD_PARTY_NOTICES.md)。
