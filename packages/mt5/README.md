# MT5 只读 Go 客户端

`portexa/mt5` 是独立 Go 模块，通过原生 TCP 读取 MT5 账户。配置包含经独立核验的固定 IP、端口、准确的交易服务器名称、登录号及 1–32 个 UTF-16 代码单元的投资者密码。

| 方法 | 用途 |
| --- | --- |
| `Probe(ctx, config)` | 原生 TCP 握手与双向证明检查，账户同步由 `Fetch` 执行 |
| `Fetch(ctx, config)` | 读取账户资金、当前持仓与挂单 |
| `FetchHistory(ctx, config, window)` | 读取服务器墙钟 `[Start, End)` 窗口的逐笔成交，边界使用秒，结果保留毫秒字段 |

## 原生 TCP

`Fetch` 和 `FetchHistory` 在同步账户时检查投资者权限。原生快照读取服务端账户杠杆。对无挂单且规则完整的 CFD Leverage 持仓，适配器按品种保证金费率、对冲规则及持仓换算率计算已用保证金。其余配置的保证金标记为缺失。当前快照缺少挂单触发价，持仓和挂单价格按 2 位小数展示。历史响应按请求序列重组，逐笔读取类型、方向、价格位数、佣金、隔夜费和单独的成交手续费。[原生握手研究与线上范围](../../docs/research/mt5-native-handshake.md)

应用侧的服务器登记、服务器时区和净值估算见[启动与管理](../../docs/product-start.md)和[账户统计](../../docs/account-statistics.md)。

## 独立验证

常规测试使用合成 TCP 环回服务，覆盖快照与历史读取。经纪商兼容性以对应接入点的实测范围为准。从仓库根目录执行：

```sh
GOWORK=off go -C packages/mt5 test ./...
GOWORK=off go -C packages/mt5 vet ./...
```
