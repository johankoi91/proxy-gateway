# Host Screen Relay

这套工程只负责通过广州 VPS 远程控制 UTM 内的 `macOS-johankoi`。代码按实际部署
位置分组，不再把 VPS、宿主 Mac 和远端 Mac 的文件混在一起。

## 从哪里开始

| 你正在配置 | 使用目录 | 主要产物 |
| --- | --- | --- |
| 广州 VPS | [`vps/`](vps/) | WireGuard Relay、React 管理后台 |
| 本机宿主 Mac | [`host-mac/`](host-mac/) | `HostScreenRelay.pkg` |
| 远端控制 Mac | [`remote-mac/`](remote-mac/) | 后台下发的唯一 `client.conf` |
| 查看原理与部署顺序 | [`docs/`](docs/) | 架构、部署、运维文档 |
| 上游源码参考 | [`third-party/`](third-party/) | Sunshine 源码压缩包，不参与构建 |

## 当前架构

```text
远端 Mac
  WireGuard App + Moonlight
        │
        ▼
广州 VPS 106.53.243.63
  WireGuard UDP 443 + 管理后台 TCP 9185
        │
        ▼
宿主 Mac
  HostScreenRelay.pkg + PF/SNAT
        │
        ▼
UTM Guest 10.211.55.4
  Sunshine
```

Guest 的公网链路完全独立：

```text
Guest 应用 → mac-guard → 东京 VPS 149.104.78.121
```

本工程不修改 Guest 的 PF、DNS、IPv6、TUN 或 sing-box。详细边界见
[`docs/architecture.md`](docs/architecture.md)。

## 当前入口

- 广州后台：<http://106.53.243.63:9185/>
- 后台账号：`admin`
- 后台密码：`admin`
- 宿主诊断：`sudo /usr/local/bin/lightnode-host-relay doctor`
- 远端 Moonlight 主机：`10.211.55.4`

HTTP 下载包含 WireGuard 私钥，当前仅按自用环境部署。正式对外使用前应启用 HTTPS、
更换后台密码，并为 PKG 完成 Developer ID Installer 签名与 Apple 公证。
