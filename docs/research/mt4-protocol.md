# MT4 原生协议验证

核验日期：2026-11-10 至 2026-11-11。目标：完全开源、无需运行 MT4 终端，最终由 Go 在 Linux 使用普通投资者只读凭据读取账户、持仓和历史。

## 当前判断

**原生 Go 路线的最小 PoC 已通过。** 本仓库新增的独立实现已在 macOS arm64 和 Linux arm64 上，对 DLSMarkets、DPrime 完成新生成的握手/认证、会话配置、账户及历史同步。Linux 实测使用只有静态 Go 文件的 `scratch` 容器，没有 MT4、Wine、EA、手机或第三方账户服务。两家断开后重新登录及同步也通过。见[当前验证结果和限制](mt4-native-validation.md)、[运行说明](../../packages/mt4/README.md)。

这不是完整产品验收：ECMarkets 尚未完成 Go 独立验证，UI 逐项数值、DLS 的投资者指示、净值/保证金全字段、实时订阅及增量恢复仍有缺口。原先筛查的 `digineat/go-metatrader4` 与 `MetaRPC/GoMT4` 的边界没有改变；当前通过的是本仓库新增实现，不是这两个上游项目。[Go 客户端源码][d-client]、[GoMT4 架构源码][m-arch]

### 早期真机证据与首段重放（历史阶段）

本轮通过已配对 iPhone 的 `pcapd` 服务分两次采集并保存了 1,903 和 2,808 个 MT4 相关包，筛选逻辑覆盖应用自身及其有效进程归属，范围检查的 3 个用例通过。用户确认手机端投资者账户登录成功，并说明期间切换了多个服务器，故样本按多服务器对照处理；此为官方客户端的基准结果，不是 Go 客户端认证结果。原始流量和服务器地址仅保留在本地忽略目录，不记录账户、密码、设备标识或完整 payload。

对操作期间新建连接重组 TCP 数据后，确认首次服务器数据到达前共发送 28 字节，无序列缺口。随后用独立 Go TCP 探针发送这段已捕获字节，收到 856 字节响应，`go vet ./...` 通过。与手机首次服务器响应相比，两次均为 856 字节，其中 20 个字节相同；该差异提示存在连接相关的数据，尚不能单凭它确定随机字段或加密算法。初始数据未匹配 HTTP 或标准 TLS 记录头。

进一步对两个接入点分别运行当前 Go 探针，均发送 28 字节，分别获得 832 和 856 字节初始响应。同一接入点的两次手机首段在本次样本中 28 字节全部相同，服务器首个 TCP 数据段均为 832 字节；两个接入点之间的客户端首段只有偏移 0 相同。由此只能确认相似的初始交换形状，不能认定首段是可跨服务器硬编码的公共常量，也未区分服务器、账户或客户端缓存材料的影响。

这一阶段仅复现了捕获首段的交换，不能作为当时认证通过的依据。后续静态与新客户端核验已确认：**首段包含账号与密码派生材料，并非匿名公共握手**；原始首段和响应只保留在本地。新的验证已补齐生成、挑战、认证及读取，不依靠重放，见当前报告。TCP 分段大小仍不能作为应用层消息边界。

工具与边界见 [真机采集](../../tools/mt4-capture/README.md)、[Go 首段探针](../../research/mt4-replay/README.md)。以上为本次本地实验结果，不依赖第三方账户服务。

### 旧 Wine 参考环境核验（历史阶段）

已从 MetaQuotes 官方 macOS 链接取得参考客户端，安装包的开发者签名及 Apple 公证检查通过，同时安装了运行所需的 Rosetta。随后按用户指定的官方指南，将旧应用和整个 Wine prefix 移至独立备份目录，腾空原路径；用户接受许可后，完整运行原生安装向导。系统安装日志确认安装成功，新 prefix 归当前用户所有。此次新环境的启动日志先记录 build 1476，再记录自动更新完成并以 build 1492 启动；不能只根据安装包标记的 4.0.1476 判断当前终端是否过旧。[官方安装说明](https://www.metatrader4.com/zh/trading-platform/help/userguide/install_mac)、[公开版本记录](https://www.metatrader4.com/en/releasenotes)

重装后启动日志仍有 `connect failed [没有连接]`，且默认 `MetaQuotes-Demo` 连接失败，没有明确的旧终端版本拒绝。另一个 `authorization failed` 属于 MQL5.community 服务，不能据此认定经纪商交易密码错误。此次完整安装后的包内 Wine 实测仍为 7.7，低于上述官方安装说明建议的 8.0.1；当前下载包与指南建议存在不一致，但尚未确定这是连接失败的根因。默认服务器失败不等于三个目标经纪商的认证失败。

辅助的 Windows/386 Go 探针在 Wine 下因缺少 `bcryptprimitives.dll` 于 Go 运行时初始化阶段退出，没有执行网络测试；该失败不能用于证明交易服务器或原生协议有问题。界面工具未能绑定 Wine 窗口；经用户允许后，AppleScript 只读确认实际窗口属于 Wine 进程，指定的 MTLReplayer 进程本身没有窗口。尚未完成三个经纪商的桌面只读登录验证。

针对新环境中当前用户的 MT4 主进程，分别使用默认 LLDB 及 Rosetta 下的 x86_64 LLDB 做有时限的附加检查，两次均返回 `attach failed: lost connection`，未读取进程内存或取得函数参数。检查结束后目标进程仍在运行，未处于调试暂停状态。此次结果只说明当前调试附加未成功，不能推断 iOS 或所有 Wine 环境都无法分析。

此后用户删除了旧 Wine 应用和相关目录。上述失败不代表当前参考环境：用户新安装的原生 arm64 移动应用 4.0.1478 已观察到三个目标平台登录成功。两轮 Mac 按进程采集共7,079包，重组后用于私有离线验证。

### 当前移动应用与内存附加

对当前移动版 MT4 附加 LLDB 的真实错误是 `Not allowed to attach to process`，系统日志记录 `task_for_pid` 失败。它是发布签名，缺少 `get-task-allow`；同一 LLDB 能成功附加、分离我们自己的带调试授权合成进程，之后再次附加 MT4 仍被拒绝。证据指向目标的发布签名/调试策略，不是用户没有同意，也不能简单归因于 DevToolsSecurity 关闭。Apple 对发布应用的调试授权有独立限制，用户口头授权不会自动改变应用 entitlement。[Apple 调试授权说明](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.cs.debugger)

未读取当前 MT4 的进程内存，未关闭 SIP/Gatekeeper、修改 DevToolsSecurity 或重签名官方应用。通过静态分析、原算法的合成离线 CPU 仿真及真实流量交叉核对，已完成本轮原生实现；运行时也不需要这些分析工具。最终数值 UI 对照因 Mac 锁屏未完成，详见当前验证报告。

## 一、digineat/go-metatrader4

固定审阅版本：[`5a6b109a920acadda2c1b91cfa69333a71d5964f`][d-tree]。仓库提供 [MIT 授权正文][d-license]。

| 项目 | 源码核验结果 | 证据 |
| --- | --- | --- |
| 连接 | `Connect` 只调用普通 TCP Dial；无 TLS 或终端认证步骤 | [client.go][d-client]、[conn.go][d-conn] |
| 首次请求 | `Execute` 连接后直接构造文本命令，默认尾随 `QUIT` | [client.go][d-client]、[proto.go][d-proto] |
| 参数保护 | 参数排序后拼成 `key=value`，经 Windows-1251 和 Base64 编码；密码可逆，无额外加密 | [proto.go][d-proto] |
| 响应处理 | 把完整响应按 Base64 / Windows-1251 解码为文本，没有认证状态或账户结构验证 | [proto.go][d-proto] |
| 账户能力 | 已审阅的完整源码树未实现普通账户握手、挑战响应、投资者身份确认，以及余额、持仓、历史的专用接口 | [源码树][d-tree] |
| 测试覆盖 | 编码/解码测试和 `net.Pipe` 模拟响应；没有真实 MT4 登录集成测试 | [client_test.go][d-test]、[proto_test.go][d-prototest] |

`WWAPUSER` 只在该项目示例中作为传入的命令字符串出现。本轮未找到 MetaQuotes 一手公开规范来确认其含义、所需服务端配置或是否适用于普通交易终端登录。**不能把它认定为已验证的 MT4 Terminal Protocol，也不能擅自归为某个官方 WebServices 命令。** MetaQuotes 的公开 API 概览只说明 Server、Manager、DataFeed、Report、WebServices 的用途，没有为这个命令提供验证依据。[项目示例][d-readme]、[MetaQuotes API 概览][mq-api]

### 本地复现

本仓库的 [wire PoC](../../research/mt4-wire/README.md) 固定上述上游版本，调用原库 `Execute`，只连接本机回环地址，用虚构凭据观察实际首帧。本轮执行环境为 Go 1.27.3 / macOS arm64，尚未在 Linux 或真实 MT4 服务器验证。执行命令：

```sh
cd research/mt4-wire
go test -count=1 -v ./...
go vet ./...
```

两个用例通过，`go vet` 通过；结果为：

```text
plaintext_command=true
fixture_password_recovered=true
handshake_before_command=false
live_mt4_verified=false
```

正例实际观察到 ASCII `WWAPUSER`、可还原虚构密码的 Base64 参数及 `QUIT`，未经登录的本地 stub 响应也可被解码；反例确认非法 Base64 响应被拒绝。这证明库当前的传输与解析行为，**不证明 stub 是 MT4 服务器，不证明真实服务器接受该命令，也不证明原生协议在技术上不可实现**。复现未向经纪商发送凭据。

另在独立临时 checkout 对该固定上游版本运行 `go test ./...`，编码与 mock 客户端测试通过。此结果同样只是离线单测证据。

## 二、MetaRPC/GoMT4

固定审阅版本：[`6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0`][m-tree]。

| 项目 | 已确认内容 | 对目标的影响 |
| --- | --- | --- |
| 传输 | 客户端使用 gRPC/TLS，证书校验开启 | 保护 Go 到 MetaRPC 的传输，不是 MT4 原生 wire 协议实现 |
| 登录 | `ConnectByServerName` 把账号、密码、服务器名通过 `ConnectEx` 交给远端服务 | 凭据会交给第三方，不能仅凭 SDK 可见就称为自主直连 |
| 终端 | 维护者架构文档明确写明由 MetaRPC 启动、管理已登录的 MT4 terminal | 不满足“任何一端均无需 MT4 终端”的约束 |
| 数据 | SDK 有账户摘要、当前订单、历史查询以及 `is_investor` 字段 | API 契约可参考，本轮没有验证真实服务结果 |
| 授权 | README 有 MIT badge，但该固定源码树未找到 LICENSE/COPYING 正文；仓库未包含云端终端管理服务的完整实现 | 作者声明 MIT；不能由此推断整个服务端或 MT4 协议已开源 |

以上分别见 [TLS 构造与登录源码][m-account]、[云端终端架构][m-arch]、[账户 protobuf 定义][m-summary]、[README][m-readme]、[完整源码树][m-tree]。GitHub Pages 文档与当前仓库架构描述存在版本差异，本报告以固定提交源码为准。

还需注意：该 protobuf 注释把 `is_investor` 解释为 `ACCOUNT_TRADE_ALLOWED` 的取反，而 MetaQuotes 官方列出这一值为 false 的其他原因，包括断连、账户归档和服务器禁用交易。因此单独看到 `is_investor=true` 不足以证明使用了投资者密码，应同时确认连接成功、已知投资者凭据和正确的账户数据；无需尝试下单来证明只读性。[protobuf 注释][m-summary]、[MetaQuotes 交易权限说明][mq-permission]

## 三、MTApi.io 的边界

厂商公开文档列有 `ConnectEx`、`IsInvestor`、`AccountSummary`、`OpenedOrders` 和 `OrderHistory`，并提供 Docker 试用部署。其关联产品网站声明通过自身 .NET 实现模拟终端并直连交易服务器；这些是厂商声明，本轮未运行镜像或验证真实账户。试用镜像和付费源码交付均不能视为公开开源协议实现，因而不作为当前路线的依赖。[厂商 Swagger][a-swagger]、[部署说明][a-readme]、[厂商产品说明][a-site]

## 四、补充的跨语言筛查

按“原生协议、普通账户、认证/加密源码”的条件，对 Go、Python、C#/C++、Java/Scala 相关公开项目作了聚焦检索；读取候选的一手 README，针对看似原生的 `mt4server`、`mt4j` 进一步审阅源码。以下均未提供本轮目标所需的完整实现：

| 候选 | 排除依据 |
| --- | --- |
| [vdemydiuk/mtapi][x-mtapi]（C#/C++） | README 明确说明并非直连 MT 服务器，而是通过终端 EA 执行 MQL 的桥接 |
| [CoeJoder/metatrader4-client-python][x-python]（Python） | 连接 ZeroMQ server；其 [配套 server][x-pythonserver] 需在 MT4 终端运行脚本 |
| [cyberflohr/mt4j][x-java]（Java） | Java VM 运行于 `terminal.exe` 进程，依赖 MQL4 EA 与 DLL；固定审阅版本 `65c849f89ffc5531474c08e770df3ead5282b94c` |
| [amuradyan/mt4server][x-scala]（Scala） | 固定版本 `9b7b3db5553f5b51d287a502b5d842a82e3112a2` 的 [MT4Commands][x-scalacode] 实际发送 RESP 的 orders/balance/ticker 文本命令，未实现 MT4 账户原生认证 |
| [FinaticORG/FinaticMTConnector][x-finatic] | 一手 README 明确要求用户在终端中运行 EA；HMAC 是上报协议，不是 MT4 登录协议 |
| [cloudQuant/pymt5][x-mt5] | 虽有握手、AES、登录与账户读取的源码说明，明确面向 MT5 Web Terminal，不能据此推导 MT4 兼容性 |
| [TradingForge/TFT-releases][x-tft] | 声称可直接连接 MT4，但审阅到的公开仓库是文档与发布物，未提供可审阅的原生协议源码；声明不算验证证据 |

检索词覆盖 `MT4 protocol`、`MetaTrader4 client`、`MT4 handshake authentication`、`MT4 reverse engineered open source` 及对应 GitHub 仓库搜索。未进入登录受限、私有、付费源码或仅有二进制的协议实现；未穷尽全部 fork 与历史版本。因此这里的结论是“本轮未确认可复用实现”，不是“全球不存在开源实现”。

## 五、完整账户聚合验收目标与当前缺口

MetaQuotes 官方确认，投资者凭据可查看账户状态而不能交易；但这只是权限定义，并不提供第三方原生客户端的连接实现。[官方登录说明][mq-auth]

以下保留为完整账户聚合的验收目标；当前最小原生 PoC 的通过范围以开头链接的实测报告为准：

1. 有可审阅的 MT4 握手、认证、传输保护、会话维护和数据解码实现；Linux Go 进程运行时不依赖 MT4 终端、EA、Manager API 或闭源服务。
2. 使用演示账户的专用投资者密码完成认证，失败凭据得到明确拒绝；不能只测端口或接受任意 Base64 响应。
3. 读取余额、净值、币种、保证金、浮动盈亏，和账户基准数据对照一致。
4. 当前持仓与指定时间范围历史的条数、方向、数量、时间及金额可对照。使用已有记录的演示账户；空列表不能证明解析正确。
5. 对两家目标经纪商分别验证，完成断开、重连、再次认证和同步；记录 MT4 服务器/构建版本及字段差异，不公开账号、密码、持仓或原始报文。

当前已有可审阅的原生实现、两家真实认证和数据同步证据，Linux arm64 实测也已通过。仍需完成参考数值逐项对照、完整账户指标、权限状态解释、实时/增量恢复及其他版本兼容，才能进入完整聚合服务验收。已提供的授权和凭据范围持续有效，不需要为同一只读验证反复确认；凭据和完整 payload 不进入 Git 或报告。

[d-tree]: https://github.com/digineat/go-metatrader4/tree/5a6b109a920acadda2c1b91cfa69333a71d5964f
[d-client]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/mt4/client.go#L54-L113
[d-conn]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/internal/conn/conn.go#L18-L25
[d-proto]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/internal/proto/proto.go#L13-L65
[d-test]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/mt4/client_test.go
[d-prototest]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/internal/proto/proto_test.go
[d-license]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/LICENSE.txt
[d-readme]: https://github.com/digineat/go-metatrader4/blob/5a6b109a920acadda2c1b91cfa69333a71d5964f/README.md
[m-tree]: https://github.com/MetaRPC/GoMT4/tree/6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0
[m-account]: https://github.com/MetaRPC/GoMT4/blob/6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0/examples/mt4/MT4Account.go
[m-arch]: https://github.com/MetaRPC/GoMT4/blob/6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0/docs/Architecture_DataFlow.md#L14-L66
[m-summary]: https://github.com/MetaRPC/GoMT4/blob/6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0/package/mt4-term-api-account-helper.pb.go#L577-L611
[m-readme]: https://github.com/MetaRPC/GoMT4/blob/6b35aa9f6ec3751b5ee92dfae91efe1b000edeb0/README.md
[mq-api]: https://www.metatrader4.com/en/brokers/api
[mq-auth]: https://www.metatrader4.com/en/trading-platform/help/userguide/authorization
[mq-permission]: https://docs.mql4.com/runtime/tradepermission
[a-swagger]: https://mt4.mtapi.io/index.html
[a-readme]: https://mt4.mtapi.io/readme
[a-site]: https://mtapi.online/
[x-mtapi]: https://github.com/vdemydiuk/mtapi#introduction
[x-python]: https://github.com/CoeJoder/metatrader4-client-python#usage
[x-pythonserver]: https://github.com/CoeJoder/metatrader4-server#installation
[x-java]: https://github.com/cyberflohr/mt4j/blob/65c849f89ffc5531474c08e770df3ead5282b94c/README.md
[x-scala]: https://github.com/amuradyan/mt4server/tree/9b7b3db5553f5b51d287a502b5d842a82e3112a2
[x-scalacode]: https://github.com/amuradyan/mt4server/blob/9b7b3db5553f5b51d287a502b5d842a82e3112a2/src/main/scala/mt4commands/MT4Commands.scala
[x-finatic]: https://github.com/FinaticORG/FinaticMTConnector#what-this-repository-is
[x-mt5]: https://github.com/cloudQuant/pymt5#pymt5
[x-tft]: https://github.com/TradingForge/TFT-releases


## 六、MetaQuotes WebTrader 公开 JavaScript 静态核验

本节只分析 MetaQuotes 公开发布的 WebTrader 客户端资源，未读取本地真实抓包、账户凭据或业务数据，未运行该脚本，也未向网络端点提交账户参数。来源：[官方 MT4 WebTrader 入口](https://trade.mql5.com/trade?version=4&lang=en)、[公开 JS 原始入口](https://trade.mql5.com/trade/res/js/mt4.en.js)及其实际重定向目标 [metatraderweb.app 的 JS](https://metatraderweb.app/trade/res/js/mt4.en.js)。本次解压后文件为 1,307,139 字节，SHA-256：`791bfcccbf40106e2ba9eb538d0e017eb561e2ba2e3e83aac2a6166d5cab80ae`；以下行号均指该未经重新格式化的版本，共 1,617 行。

| 静态观察 | 定位与含义 |
| --- | --- |
| 网关路由与会话材料 | 第 1173–1177 行：先向 HTTPS `/trade/json` 提交 login、trade_server 等路由参数；响应中的 signal_server、key、token、ssl 决定连接地址、会话密钥、引导 token 和传输。此调用本轮未实际发起。 |
| WebSocket 传输 | 第 1119–1126 行：使用浏览器 WebSocket，二进制 ArrayBuffer，默认 WSS；网关域名另有测速选择逻辑。浏览器代码没有直接打开经纪商原生 TCP 连接。 |
| 消息封装 | 第 1119–1126 行：外层 8 字节包含长度与类型；加密前内层请求头为两个随机字节及 uint16LE 命令号，响应还解析一个状态字节。这只是该 Web 网关格式。 |
| 对称加密 | 第 1114–1116 行：WebCrypto AES-CBC，调用时使用 16 字节零 IV；既有内置引导密钥导入路径，也有从上述 HTTPS 响应导入 key 的路径。未在所追踪登录路径发现可认定为原生 MT4 TCP 的公钥交换实现。 |
| Web 登录字段 | 第 1099 行的登录序列化器将最多 32 个字符写入 64 字节的 UTF-16LE 字段，并可追加 16 字节；第 1447、1450 行可追到密码输入框，第 1173 行将其交给 Web 登录命令。这是 Web 网关认证线索，不能证明手机原生客户端使用相同字段、派生方法或命令号。 |

**结论：公开 JS 能确认一套依赖 MetaQuotes HTTPS 路由、token 和 WebSocket 网关的客户端协议，尚不能提供可直接用于 MT4 原生 TCP 的握手、密码认证或解密实现。** 其“请求 key 后以 AES-CBC 封装消息”的行为，不等于经纪商原生服务器采用相同算法或相同密钥来源。公开资源中可能有部分业务结构可供研究，但本轮没有建立 Web 消息与手机原生 TCP 报文的可靠对应关系。

本节仅是早期 Web 路线的排除证据；后续原生握手/认证的确认来自移动版静态分析与实际 Go 连接，不是套用此 Web 客户端。不能凭报文长度、少量相同字节或 AES 等常见算法名称建立对应，更不能直接套用 Web 命令号、密钥或封装。该 Web 资源未提供本轮已确认的开源复用授权；仅记录静态事实和公开来源，没有把厂商代码复制进产品实现。


## 官方服务器发现与名称核验（补充）

本轮只读取公开官网、下载附件和 MetaQuotes WebTrader 参数目录；未访问本地抓包或应用用户数据，未使用账号密码，也未安装或执行下载包。

MetaQuotes 的 [WebTrader 公开入口](https://trade.mql5.com/trade?lang=en) 可通过 `servers` 与 `trade_server` 参数查询服务器名称。实际页面重定向至 `metatraderweb.app`，内嵌 `window.__webtrader_params` 的 `servers` 字段给出名称、MT版本及公司。以下为本轮分别请求后的结果：

| 查询 | 官方页面返回 | 边界 |
| --- | --- | --- |
| [DLSMarkets-Live3](https://trade.mql5.com/trade?servers=DLSMarkets-Live3&trade_server=DLSMarkets-Live3&lang=en) | `DLSMarkets-Live3`，MT4，DLS Markets Limited | 未返回原生连接地址 |
| [ECMarkets-Live08](https://trade.mql5.com/trade?servers=ECMarkets-Live08&trade_server=ECMarkets-Live08&lang=en) | `ECMarkets-Live08`，MT4，EC Markets Limited | 未返回原生连接地址 |
| [DPrimeVU-Live9](https://trade.mql5.com/trade?servers=DPrimeVU-Live9&trade_server=DPrimeVU-Live9&lang=en) | `servers=null` | 不能据此断言任何历史版本从未用过该名称 |
| [DPrimeVU-Live 9](https://trade.mql5.com/trade?servers=DPrimeVU-Live%209&trade_server=DPrimeVU-Live%209&lang=en) | `DPrimeVU-Live 9`，MT4，D Prime Vanuatu Limited | 本轮官方目录的精确名称含空格 |

这个页面参数机制能核对名称，**不是已确认有兼容性承诺的公开目录 API，也不提供可直接使用的原生服务器 IP 或 `.srv` 文件**。D Prime 自己的[品牌更新公告](https://www.dooprimenews.com/service-updates/system-maintenance/mt4-live-5-demo-5-server-name-update-and-scheduled-maintenance)也使用带空格的 `DPrimeVU-Live 5` 并要求在终端搜索 `DPrimeVU`；该公告只直接证明 Live 5 的命名，Live 9 的精确名称以本轮目录响应为证。

MetaQuotes 的[启动配置官方说明](https://www.metatrader4.com/en/trading-platform/help/service/start_conf_file)确认：`Server` 对应终端 `config` 目录中 `.srv` 文件的名称；`DataServer` 是数据中心地址，可被自动配置机制忽略。因此单独写一个服务器显示名不能替代连接配置。可信下一步是使用参考终端自带的经纪商搜索取得配置，或从对应经纪商官方发布物取得 `.srv`/连接配置，再核对原生连接；本轮未读取终端用户数据，也未取得这三个目标的官方 `.srv`。

另在 [DLSM 帮助中心 MT4 下载说明](https://dlsmarketshelp.zendesk.com/hc/en-us/articles/27606209588377-How-to-download-MT4-app)找到 [Windows 安装附件](https://dlsmarketshelp.zendesk.com/hc/en-us/article_attachments/27606158952089)，只下载作静态检查。附件 4,696,624 字节，SHA-256 为 `3147ee3818b26173c0ccce9814b8139aa6d1d86b6a6d1f63a07ef5c3125f2185`；其 UTF-16 字符串含 MetaQuotes 的 `download.mql5.com/cdn/dns/dns.dat` 及组件下载模板。但直接读取该 `dns.dat` 返回 HTTP 403，本轮未解析安装包内部资源、未提取 `.srv`，更未建立目标服务器与任何 IP 的归属关系。此线索仅用于后续研究官方安装器如何完成服务器发现。
