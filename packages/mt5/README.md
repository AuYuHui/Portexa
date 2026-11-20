# MT5 Web 终端只读客户端

`portexa/mt5` 是独立 Go 模块。调用 `Fetch(ctx, config)` 获取账户资金、当前持仓与挂单。调用 `FetchHistory(ctx, config, window)` 获取半开服务器墙钟窗口内的成交，保留成交编号、所属订单和持仓、成交方向、毫秒时间及费用字段。客户端通过 WSS 连接经纪商为对应交易服务器提供的 `/terminal` 端点。TCP 连接固定到登记的 IP 和端口，TLS 使用 Web 终端域名校验系统信任的证书。读取持仓前检查服务器返回的投资者权限和交易服务器名称。

协议布局参考了上游 Web 终端客户端的 v0.2.0 版本。本模块的验证采用本地合成 WSS 服务。真实经纪商的兼容性需要用该经纪商的只读账户核验。[上游协议说明](https://github.com/n0wsh/metatrader5-go/blob/v0.2.0/README.md)

```sh
GOWORK=off go -C packages/mt5 test ./...
```
