# Relay Admin

这是部署在广州 VPS 的管理后台源码，不安装到任何 Mac。

## 技术结构

```text
cmd/relay-admin/   Go API、嵌入式 React 构建产物和安装命令模板
web/               React + TypeScript 前端源码
relay-admin.service systemd 服务
```

VPS 运行时只有一个 Go 进程。React 构建产物嵌入二进制，不需要在 VPS 运行 Node。

## 构建

从仓库根目录执行：

```bash
./vps/admin/build-linux-arm64.sh
```

脚本依次执行前端构建、Go 测试和 Linux ARM64 交叉编译，输出到：

```text
vps/admin/dist/lightnode-relay-admin-linux-arm64
```

## API

```text
GET    /api/status
GET    /api/clients
POST   /api/clients
GET    /api/clients/{name}/client.conf
DELETE /api/clients/{name}
GET    /api/host/config
GET    /api/host/bundle.zip
```

默认读取：

```text
/etc/wireguard/clients
/etc/wireguard/host/host.conf
/opt/lightnode-relay-admin/HostScreenRelay.pkg
```

默认监听 `:9185`，默认 Basic Auth 为 `admin/admin`。当前 HTTP 与弱口令只适用于自用
环境；服务响应使用 `Cache-Control: no-store`，列表和日志不会输出私钥。
