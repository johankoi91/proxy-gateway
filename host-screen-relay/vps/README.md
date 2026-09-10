# Guangzhou VPS

本目录中的内容只部署到广州 VPS `106.53.243.63`。

## 目录

```text
vps/
├── relay/   WireGuard Hub、转发规则、Peer 管理脚本
└── admin/   Go API + React 管理后台
```

## VPS 运行组件

```text
wg-quick@wg0.service             UDP 443
lightnode-relay-admin.service    TCP 9185
```

`relay/` 安装到：

```text
/etc/wireguard/wg0.conf
/usr/local/sbin/lightnode-relay-rules
/usr/local/sbin/lightnode-add-remote-peer
/usr/local/sbin/lightnode-remove-remote-peer
```

`admin/` 管理远端客户端，提供新增、下载 `client.conf`、撤销以及宿主安装包下载。
它不使用数据库，状态保存在 `/etc/wireguard/clients` 和 `/etc/wireguard/revoked`。

具体部署命令见 [`../docs/deployment.md`](../docs/deployment.md)，日常检查见
[`../docs/operations.md`](../docs/operations.md)。
