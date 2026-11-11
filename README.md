# Portexa

交易平台接入项目。当前已形成可独立使用的 MT4 原生协议 Go 模块，运行时无需 MT4 终端、Wine、EA 或第三方账户服务。业务聚合、存储与调度留在主工程，协议包只负责连接、认证和读取协议数据。

## 工程入口

| 路径 | 职责 |
| --- | --- |
| [packages/mt4](packages/mt4/README.md) | 独立协议模块 portexa/mt4 |
| [cmd/mt4probe](cmd/mt4probe/README.md) | 联网验证命令，输出脱敏结果 |
| packages/mt4/tests、tests | 协议、公开接口、CLI 和采集测试 |
| [tools/mt4-capture](tools/mt4-capture/README.md) | 本地采集工具 |
| [research](research/README.md) | 保留的早期实验 |
| [docs/architecture.md](docs/architecture.md) | 目录、依赖方向及独立发布方式 |
| [docs/product-plan.md](docs/product-plan.md) | 多账户聚合产品方案、Analytics 口径及单用户登录规则 |
| [docs/research](docs/research/README.md) | 验证记录与协议研究依据 |

模块按项目命名，不绑定个人托管账户。单文件不超过1000行，结构检查会自动执行此限制。

## 开发与验证

需要 Go 1.26+ 和 Python 3。仓库根目录执行：

```sh
make check
make build
```

产物为 .local/bin/mt4probe 和 .local/bin/mt4verify。普通检查仅运行合成与回环测试，不登录真实服务。

协议模块也能独立检查：

```sh
GOWORK=off go -C packages/mt4 test ./...
```

当前已完成 EC Markets、DPrime、DLSMarkets 的原生只读兼容性实测及官方字段对照，范围与方法见[本轮验收记录](docs/research/mt4-readonly-acceptance.md)；早先 Linux 运行保留在[历史验证报告](docs/research/mt4-native-validation.md)。凭据、接入地址、设备材料、抓包和真实数值均不进入源码、文档或测试。
