# 协议研究索引

这里保留协议实现依据与历史验证范围。当前客户端入口为 [MT4 SDK](../../packages/mt4/README.md) 和 [MT5 SDK](../../packages/mt5/README.md)。

| 文档 | 验证范围 |
| --- | --- |
| [MT4 只读验收](mt4-readonly-acceptance.md) | 三家真实会话、字段对照、历史与错误分类 |
| [MT4 早期原生验证](mt4-native-validation.md) | 两家 PoC、Linux 证据与当时的待验收项 |
| [MT4 移动客户端静态分析](mt4-mobile-auth-static.md) | 握手、认证、流状态与字段依据 |
| [MT4 候选方案筛查](mt4-protocol.md) | 早期上游项目、Web 路径与参考环境 |
| [MT5 原生握手研究](mt5-native-handshake.md) | 官方客户端样本、独立探针的历史握手和首次同步对照。当前 Go SDK 的快照与成交读取另有合成 TCP 测试 |

历史验证保留原日期、版本和快照条数；目录重构后的单元/接口/离线回归不会被表述成新的线上登录结果。原始数据和真实标识不存放在本文档目录。
