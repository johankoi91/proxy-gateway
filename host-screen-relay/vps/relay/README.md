# WireGuard Relay

本目录只部署到广州 VPS，负责 `wg0` 和远控端口转发。

| 文件 | 用途 |
| --- | --- |
| `install-relay.sh` | 初始化或更新 VPS Relay |
| `wg0.conf.example` | 服务器配置模板 |
| `lightnode-relay-rules` | iptables 转发、限制和 hairpin 规则 |
| `add-remote-peer.sh` | 创建远端客户端 |
| `remove-remote-peer.sh` | 撤销远端客户端 |

后台通过安装到 `/usr/local/sbin` 的新增与撤销脚本管理 Peer。正常使用时优先通过后台
操作，不要同时手工编辑 `/etc/wireguard/wg0.conf`。
