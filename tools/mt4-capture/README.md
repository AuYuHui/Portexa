# iPhone MT4 流量采集

这是本地协议分析工具，不是账户连接器。需要已配对、已解锁且通过 USB 连接的 iPhone，以及已安装的 MT4。

## 采集

```sh
uv run tools/mt4-capture/capture.py --udid '<本机查询到的硬件 UDID>' --seconds 120
```

脚本使用固定版本的 `pymobiledevice3` 和 iPhone 内置 `pcapd` 服务。根据 MT4 的实际可执行文件名筛选直接流量及由系统代理发出的相关流量，其他应用的数据不写入文件。该机制见[工具官方说明](https://doronz88.github.io/pymobiledevice3/latest/guides/packet-capture/)。

看到 `capture_ready` 后，在手机上登录投资者账户，依次查看交易页和历史页。到时自动停止，也可按 Ctrl+C 停止。零包采集返回失败并删除空文件。

原始文件写入 `.local/mt4-capture/`：目录权限 `0700`，文件权限 `0600`，已由 Git 忽略。终端只输出计数和状态，不输出数据包正文；勿将原始文件放入报告或版本库。

## Mac 上运行的移动应用

`capture_macos.py` 用 macOS `tcpdump` 的进程元数据，仅保存指定 MT4 进程的 TCP 包。需在本机终端完成管理员认证；PID 应先核对为当前目标进程。

```sh
sudo /usr/bin/python3 tools/mt4-capture/capture_macos.py --pid '<当前 MT4 PID>' --seconds 180
```

采集上限为180秒及16 MiB，输出保存在 `.local/mt4-mobile-analysis/`。此次两轮共7,079包，均已自动停止；离线核对的保存包均归属于目标 MT4 PID。此类 pcapng 使用 DLT_NULL 四字节链路头，不能套用 iPhone 文件的链路偏移。

初始28字节也含账号与密码派生数据；不是仅后续认证包敏感。分析时只在私有目录解码，报告保留计数、边界和验证结论。

## 范围检查

```sh
python3 -m unittest discover -s tests/capture -v
```

检查目标进程、截断进程名和有效进程归属，同时确认空进程名、其他应用及相似名称不会通过筛选。

## 结果边界

抓到登录过程可帮助定位连接地址、重组 TCP 流、比较握手及查询阶段；它不能直接提供加密算法或证明独立客户端已登录。TCP 分段边界也不等于 MT4 消息边界。
