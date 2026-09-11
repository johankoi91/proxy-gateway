# Browser Workspace

本工程提供通过广州 WireGuard 私网访问 UTM Ubuntu 中 KasmVNC/Chromium 的完整实现。

```text
browser-workspace/
├── kasmvnc/     Ubuntu Docker 工作区
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

当前 Chromium 出口仍是 Ubuntu 普通网络。Linux Controller 完成前，不用于风险敏感账号。
