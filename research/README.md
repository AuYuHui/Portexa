# 历史实验

这些代码保留早期验证依据，与原生协议运行模块分开维护。

- [mt4-wire](mt4-wire/README.md)：固定上游版本的文本 TCP 行为实验。它只有回环测试，不能作为原生 MT4 登录实现；依赖由单独的研究模块管理。
- [mt4-replay](mt4-replay/README.md)：早期首段重放工具，不生成完整认证，也不作为协议模块的依赖。

当前实现入口是 [packages/mt4](../packages/mt4/README.md)。研究实验禁止加入真实凭据、报文或账户明细。
