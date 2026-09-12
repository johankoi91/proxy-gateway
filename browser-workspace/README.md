# Browser Workspace

本工程提供通过广州 WireGuard 私网访问 UTM Ubuntu 中 KasmVNC/Chromium 的完整实现。

```text
browser-workspace/
├── kasmvnc/     Ubuntu Docker 工作区
├── linux-controller/ Ubuntu 东京出口、DNS 与 nftables Kill Switch
├── host-mac/    宿主 WireGuard、PF 转发和 PKG
├── remote-mac/  远端 Mac 使用说明
├── vps/         广州 WireGuard Hub、hairpin 和 Peer 后台
├── docs/        运维说明
└── CURRENT-IMPLEMENTATION.md
```

统一入口：

```text
https://10.88.0.254:8443/
```

只读下载入口：

```text
https://10.88.0.254:8444/
```

Linux Controller 已启用。Ubuntu 主机和 Chromium 容器的公网流量均固定经东京
`149.104.78.121`，隧道故障时由 nftables 阻止回落。
