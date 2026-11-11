# MT4 验证命令

在仓库根目录执行 `make build`，使用 `.local/bin/mt4probe`。命令仅负责参数、配置和脱敏输出，原生协议来自独立模块 `portexa/mt4`。

```sh
.local/bin/mt4probe --authenticate --history-days 2 < /absolute/path/to/private-config.json
```

配置从标准输入读取。以下均为合成占位示例，地址不能用于真实测试：

```json
{
  "target": "203.0.113.10:443",
  "server": "ExampleBroker-Live1",
  "login": 1234567,
  "password": "<本地配置>",
  "password_type": "investor"
}
```

实际配置仅保留在私有目录，文件权限设为0600；不把密码放进命令行、日志或版本库。

| 参数 | 行为 |
| --- | --- |
| 无选项 | 新生成首段并解析初始响应 |
| `--authenticate` | 完成认证、会话配置和账户同步 |
| `--history-days 2` | 额外请求最近两日历史，允许1–31日 |
| `--history-all` | 从0请求至协议最大时间值，覆盖服务器时区超前的近期历史；仍受16 MiB上限约束 |
| `--history-window-seconds 86400` | 分窗查询，相邻窗重叠一秒，按订单号归并；与历史选项配合使用 |

`password_type` 必须为 `investor`，否则在连接前拒绝。`--history-all` 与 `--history-days` 互斥，历史选项要求 `--authenticate`；全部窗口共用20秒期限。具体日期需按服务器墙钟时间解释，`--history-days` 使用本机时间回退作为近似下界。

输出只含状态、长度、条数和检查标志，不含真实数值、品种明细、登录号或密码。可选输入 `expected_account` 支持 `balance`、`credit`、`equity`、`margin` 按显示的两位小数比较，`currency` 按原始币种比较，`leverage` 按整数比较；输出仅保留布尔结果。只有五项账户参考值全部匹配时 `account_reference_verified=true`，参考值不全不会被算作验收通过。活动净值和浮盈的参考值必须对齐报价时刻。

`history_complete` 与 `history_completed_windows` 明确区分完整与部分结果。失败输出附脱敏的 `error_code`、`error_operation`、版本及原始状态码，命令同时以非零状态退出；调用方不能用部分历史覆盖已存快照。SDK 的[同步与错误约定](../../packages/mt4/README.md)有完整说明。

`authentication_verified=true` 要求同一线上会话的配置和账户响应均通过。投资者原始指示不等同所有交易权限，`read_only_permission_verified` 保持未确认。命令不发送交易请求。

离线验证从联网命令中拆出，使用 `.local/bin/mt4verify --vector /private/vector.json`；标准输入格式相同但不支持参考值比较。它仅核对本地向量，不建立网络连接。运行和接口详见[协议模块](../../packages/mt4/README.md)。
