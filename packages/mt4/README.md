# MT4 原生协议模块

[English overview](../../README.md#standalone-mt4-module) · [中文项目说明](../../README.zh-CN.md) · [文档索引](../../docs/README.md)

模块名为 `portexa/mt4`，只依赖 Go 标准库和固定版本的 MIT 许可纯 Go LZO 解码器。它可脱离 Portexa 主工程独立编译、测试，运行时不依赖 MT4、Wine、EA、手机或第三方账户服务。第三方许可见[声明](THIRD_PARTY_NOTICES.md)。

模块使用 [MIT License](LICENSE)，许可证与第三方声明随目录保留。当前模块路径用于仓库内本地开发，主工程通过 `replace` 引用；尚未配置可直接 `go get` 的公开模块地址。独立迁移时需设置正式模块路径并更新导入。

## 调用接口

- `Probe(ctx, config)`：新建连接、生成首段、解码首响应，然后关闭连接。
- `Fetch(ctx, config, options)`：完成认证、会话核对及数据同步，然后关闭连接。
- `Result`：类型化协议值、订单、历史及同步状态；失败时可能附带部分结果，需检查错误和状态。

```go
import (
    "context"
    "portexa/mt4"
)

func read(ctx context.Context, config mt4.Config) (mt4.Result, error) {
    return mt4.Fetch(ctx, config, mt4.Options{
        History: &mt4.HistoryRange{Start: 1700000000, End: 1700003600},
    })
}
```

`Config` 包含 `Address`、`Server`、`Login`、`Password`、`PasswordType`，由调用方在本地提供。必须明确设置 `PasswordType: mt4.InvestorPassword`；缺省、主密码及其他类型在连接前拒绝。此项是调用方对密码来源的声明，不是服务端权限证明。地址必须是已独立核验的 IP 和端口，密码限制为1–16个 UTF-8 字节。代码不读取环境变量、标准输入、配置文件或数据库，不打印结果。

每次调用重新生成安装随机材料和认证。连接在调用结束时关闭，上限20秒，调用方更早的 deadline 或取消会中断连接。没有自动重试、交易命令、实时订阅或后台连接；认证后的写入还受固定只读查询形状白名单保护。

## 账户与订单

`Account` 提供 `Balance`、`Credit`、`Currency`、`Equity`、`Margin`、杠杆及原始指示。金额保留账户原始币种单位，例如 USC 不会换算成 USD。净值按同一账户响应的余额、信用及持仓净盈亏计算；保证金使用同期订单价格、转换率、品种规则和账户组覆盖，包含五种保证金模式及锁仓处理。只有 `MarginAvailable=true` 才能使用保证金；缺少所需配置或遇到未知模式返回错误。

`Trade` 包含品种、操作类型、整数百分之一手、`Lots()`、`OpenTime/CloseTime`、开/平价格、SL/TP、利润、佣金、隔夜费和税费；`NetProfit()` 汇总各项费用。当前持仓的 `CloseTime` 为0，`ClosePrice` 是当前价格；历史订单对应实际平仓字段。`Time20/Time40` 保留为旧字段别名。

`Comment` 保留服务端订单备注（记录 `0x94` 起最多32字节，遇NUL结束），不扩展到相邻字段，不解释资金用途。原始字节未声明为UTF-8时调用方负责显示处理；出入金识别、规则版本、人工校正和持久化均属于调用方业务。备注布局来自已有静态记录研究，本次以合成协议/公开接口验证，不将它表述为新一次真实经纪商对照。

时间是 MT4 服务器墙钟秒值，不自动转换为 UTC，也不推测服务器时区。活动账户的净值和浮盈随报价变化，对照官方客户端时需要相同买卖报价和相同持仓集合；不能比较不同时刻后声称逐分相等。

## 历史同步

`HistoryRange` 的起止秒值原样发送，满足 `Start < End <= 2147483647`。全历史可用 `Start: 0, End: 2147483647`，避免本机 UTC 终点漏掉服务器时区超前的近期订单。调用方负责具体日期到服务器时间的转换。

`HistoryWindowSeconds` 为0时请求单块，设置为至少2时分窗查询，相邻窗口重叠一秒。结果按订单号归并并排序；重叠重复不增加条数，后窗修订覆盖前窗，同一响应内相互冲突的重复记录报错。历史命令状态1表示本区间为空，与配置表状态1的“未变化”不同；空窗不会清空其他窗口的记录。

`HistoryRead/HistoryComplete=true` 表示请求窗口全部得到完整响应，包括明确空响应。`HistoryCovered` 只包含已成功的窗口；中途失败保留此前记录及覆盖范围，并返回错误。失败或未完成结果不能替换持久化快照。重连会新建会话；调用方可重查原区间，或重查未完成范围并按订单号更新存储。协议模块不落库，不提供业务同步游标。

单块解压及合并结果保留16 MiB上限，超限返回 `resource_limit`，不会把截断结果标记为完成。分窗仍共用20秒期限。这里的完整性限于服务器提供的历史，无法恢复服务器已经删除的数据，也没有未经验证的分页或自动重试承诺。

## 错误分类与版本

错误可通过 `errors.As(err, &failure)` 取得 `*mt4.Error`：`Code`、`Operation`、`ServerBuild`、原始 `Status`。错误文本脱敏；`errors.Is` 仍可检查 `context.Canceled`、超时及底层连接错误。直接展开底层 `net.OpError` 可能包含接入地址，调用方应记录结构化字段。

| Code | 含义 |
| --- | --- |
| `invalid_config` / `read_only_required` | 参数非法 / 未声明投资者密码，均在连接前拒绝 |
| `unsupported_build` | 已知不支持的旧挑战版本 |
| `unsupported_protocol` | 零挑战、未知记录模式、订单/保证金类型或缺少必要能力 |
| `timeout` / `canceled` / `connection` | 超时 / 取消 / 断开、重置或截断；附失败阶段 |
| `server_rejected` | 服务端非成功状态，保留原码，不猜测密码错误或权限原因 |
| `invalid_response` / `resource_limit` | 响应数据、边界或金额非法 / 超出资源上限 |

build 用于诊断及官方协议分支判断，不按券商或逐个 build 白名单适配。当前实现尝试 signed build>1419 的普通非零挑战 profile，并检查后续响应、记录布局和能力；兼容的新 build 无需增加分支。已实测版本不等于保证所有未来版本兼容。

## 结构与测试

`internal/protocol` 实现流状态、摘要、记录封装、特征 VM、认证及响应解析，外部调用方不能导入。`internal/verification` 只供离线工具使用，不在 SDK 运行依赖内。所有测试位于 `tests`，生产文件不混入测试实现。

在本目录运行：

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

公开接口测试使用本机回环上的合成服务端，协议测试保留独立合成向量；不登录真实服务。私有样本回归使用独立工具：

```sh
go run ./tests/cmd/verify --vector /absolute/path/to/private-vector.json < /absolute/path/to/private-config.json
```

工具只输出匹配标志、长度和条数，不打印原始报文或真实数值。命令行联网入口另见[使用说明](../../cmd/mt4probe/README.md)。

## 已验证范围

2026-11-11 本轮在 macOS arm64 对 EC Markets（1492）、DPrime 与 DLSMarkets（1470）完成新的直连和官方客户端对照，见[当前验收记录](../../docs/research/mt4-readonly-acceptance.md)。早先 Linux arm64 的独立运行证据仍保留原日期，见[历史验证报告](../../docs/research/mt4-native-validation.md)，不能视为本次新运行。

投资者字段只保留原值，不推断全部权限。自动发现、OTP、旧版零挑战、实时订阅、业务持久化及服务器保留范围之外的数据仍不在此次实现内。

C28 已含账号和密码派生材料；首段、报文、向量和真实数据都不能作为公开示例。公开应用兼容标识属于所分析协议 profile，不是用户硬件或个人托管账户标识。

本目录可直接迁入独立仓库，再设置正式发布的模块地址；不需要带走主工程 CLI、业务、研究或采集代码。
