# Relay Admin

广州 VPS 上的 Go API 与 React 管理后台，只管理远端 WireGuard Peer。

```text
GET    /api/status
GET    /api/clients
POST   /api/clients
GET    /api/clients/{name}/client.conf
DELETE /api/clients/{name}
GET    /api/host/config
GET    /api/host/bundle.zip
```

默认监听 `:9185`。状态页显示 WireGuard 和宿主 Peer `10.88.0.2` 的在线状态。
宿主 ZIP 包含 `BrowserWorkspaceHost.pkg`、`host.conf` 和安装命令。

构建：

```bash
./build-linux-arm64.sh
```
