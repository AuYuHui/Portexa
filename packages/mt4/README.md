# MT4 原生协议模块

模块名为 `portexa/mt4`，只依赖 Go 标准库和固定版本的 MIT 许可纯 Go LZO 解码器。它可脱离 Portexa 主工程独立编译、测试，运行时不依赖 MT4、Wine、EA、手机或第三方账户服务。第三方许可见[声明](THIRD_PARTY_NOTICES.md)。

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

`Config` 包含 `Address`、`Server`、`Login`、`Password`，由调用方在本地提供；地址必须是已独立核验的 IP 和端口。密码限制为1–16个 UTF-8 字节。代码不读取环境变量、标准输入、配置文件或数据库，不打印结果。业务方自行决定凭据管理、数据使用及持久化。

每次调用重新生成安装随机材料和认证。连接在调用结束时关闭，上限20秒，调用方更早的 deadline 或取消会中断连接。历史区间使用递增的 Unix 秒值，单个解压体最大16 MiB；没有自动重试、交易命令、实时订阅或后台连接。

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

已有 macOS arm64、Linux arm64 的独立直连验证，详情与原日期见[验证报告](../../docs/research/mt4-native-validation.md)。当前只覆盖移动协议 build1478、已观测服务端 build1470的非零挑战分支。

投资者字段只作为原始指示保留，不推断全部权限。`Trade.Time20/Time40` 暂保留线格式命名，样本支持开/平仓解释，但 UI 逐项核验尚未完成。净值、币种、保证金全字段、历史完整性、增量恢复、自动发现、OTP和其他版本均未验收。

C28 已含账号和密码派生材料；首段、报文、向量和真实数据都不能作为公开示例。公开应用兼容标识属于所分析协议 profile，不是用户硬件或个人托管账户标识。

本目录可直接迁入独立仓库，再设置正式发布的模块地址；不需要带走主工程 CLI、业务、研究或采集代码。
