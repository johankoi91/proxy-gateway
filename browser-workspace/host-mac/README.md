# Browser Workspace Host

本目录只部署到运行 UTM Ubuntu 的宿主 Mac。

宿主组件：

- WireGuard `10.88.0.2`。
- PF 将 `10.88.0.2:8443` 转发到 Ubuntu `10.211.55.7:8443`。
- `com.lightnode.browser-host-wireguard` LaunchDaemon。
- `com.lightnode.browser-route` LaunchDaemon。

构建 Apple Silicon PKG：

```bash
./scripts/package/build-host-pkg.sh
```

输出：

```text
dist/BrowserWorkspaceHost-<version>-arm64.pkg
```

命令：

```bash
sudo lightnode-browser-host configure /path/to/host.conf [guest-ip]
sudo lightnode-browser-host doctor
sudo lightnode-browser-host uninstall
sudo lightnode-browser-host uninstall --purge
```

默认 Guest 地址为 `10.211.55.7`。宿主身份 `10.88.0.2` 只能同时运行在一台 Mac。
