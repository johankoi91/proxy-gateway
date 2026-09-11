# Guangzhou VPS

本目录只部署到广州 VPS `106.53.243.63`。

```text
vps/
├── relay/  WireGuard Hub、10.88.0.254:8443 hairpin、Peer 脚本
└── admin/  Go API + React Peer 管理后台
```

VPS 不运行 KasmVNC，也不承担 Chromium 的互联网出口。
