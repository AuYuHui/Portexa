# 历史实验

这些目录保留协议实验与验证依据。应用的运行模块见 [MT4 SDK](../packages/mt4/README.md) 和 [MT5 SDK](../packages/mt5/README.md)。

- [mt4-wire](mt4-wire/README.md)：固定上游版本的文本 TCP 行为实验。它只有回环测试，不能作为原生 MT4 登录实现；依赖由单独的研究模块管理。
- [mt4-replay](mt4-replay/README.md)：早期首段重放工具，不生成完整认证，也不作为协议模块的依赖。
- [mt5-handshake](mt5-handshake/probe.py) 可复测原生 TCP 扩展握手并验证双向证明。合成测试覆盖握手、滚动状态和冷缓存同步请求。运行 `python3 -m unittest discover -s research/mt5-handshake/tests -v`。线上同步的验证情况见 [MT5 原生握手研究](../docs/research/mt5-native-handshake.md)。

研究实验中的真实凭据、报文和账户明细仅保存在被忽略的本地目录。
