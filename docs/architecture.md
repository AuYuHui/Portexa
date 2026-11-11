# 工程结构

Portexa 使用一个主工程和一个可独立提取的 MT4 Go 模块。模块路径按项目命名，不绑定个人托管账户。`go.work` 方便编辑器同时加载模块；主工程的本地 `replace` 让关闭 workspace 后也能构建。

```text
Portexa/
├── cmd/mt4probe/                 # 联网验证命令入口
├── internal/cli/                 # 本地输入、参数和脱敏输出
├── packages/mt4/                 # 独立模块 portexa/mt4
│   ├── client.go                 # Probe / Fetch，连接生命周期与上下文
│   ├── types.go                  # 对外协议数据类型
│   ├── internal/protocol/        # 编码、握手、认证、同步和解码
│   ├── internal/verification/    # 离线样本核对，不进入 SDK 运行依赖
│   └── tests/                    # 协议测试、公开接口测试和合成服务端
│       └── cmd/verify/           # 离线验证命令
├── tests/                        # 主工程 CLI 与采集范围测试
├── tools/mt4-capture/            # 本地采集脚本
├── research/                     # 早期实验，隔离于运行代码
├── docs/research/                # 历史研究、版本依据和验证边界
├── scripts/check_structure.py    # 目录、模块名及1000行限制
├── go.mod                       # 主工程 portexa
├── go.work                      # 本地多模块开发入口
└── Makefile                     # 统一构建与检查入口
```

## 依赖方向

`cmd/mt4probe → internal/cli → portexa/mt4 → internal/protocol`。协议模块不依赖主工程或业务代码，不读配置文件，不打印日志，也不落库。调用方传入凭据和上下文，取得类型化结果；命令行层只输出脱敏检查结果。

离线验证路径是 `packages/mt4/tests/cmd/verify → internal/verification → internal/protocol`。它读取私有样本且不联网，不能被当作运行中的连接器。`research/mt4-wire` 保留早期上游行为实验，有独立模块及测试，不作为原生协议依赖。

## 后续业务与独立发布

业务调用 `mt4.Fetch` 取得协议快照，再在主工程完成多平台聚合、存储和调度。这些职责不加入协议模块。无需提前创建空的业务目录。

`packages/mt4` 自带 `go.mod/go.sum`、说明、第三方许可和测试，关闭 workspace 后可独立检查。未来迁入独立仓库时，保留这个目录的内容、设定正式发布模块地址并更新调用方即可；协议本身没有主工程耦合。

## 测试与约束

`make check` 运行各模块 Go 测试、Python 范围测试、`go vet` 和结构检查。所有测试文件集中在 `tests` 目录下。合成服务端只监听本机回环地址；普通回归不会连接真实交易平台。

源码、测试、脚本和文档单文件上限均为1000行；本地数据、依赖缓存及生成物由 Git 忽略，不进入该检查。真实材料只留在私有目录，历史线上证据保留原日期和未验证项，不能因目录调整改写成新的线上结果。
