# Deployment

## 部署顺序

```text
1. 广州 VPS Relay
2. 广州管理后台
3. 宿主 Mac PKG
4. Guest Sunshine
5. 远端 Mac WireGuard + Moonlight
```

## 1. 广州 VPS Relay

VPS 安装发行版提供的 `wireguard-tools`、`iptables` 和 `flock`，然后在 `vps/relay/`
执行：

```bash
sudo ./install-relay.sh --host-public-key '<HOST_PUBLIC_KEY>'
```

这会创建 `/etc/wireguard/wg0.conf`，并启用 `wg-quick@wg0.service`。生产环境已有配置时
不要用示例公钥重复执行，应先备份 `/etc/wireguard`。

## 2. 广州管理后台

在开发机从仓库根目录构建 ARM64 Linux 二进制：

```bash
./vps/admin/build-linux-arm64.sh
```

输出为：

```text
vps/admin/dist/lightnode-relay-admin-linux-arm64
```

上传到 VPS 后安装：

```bash
sudo install -m 0755 lightnode-relay-admin-linux-arm64 /usr/local/sbin/lightnode-relay-admin
sudo install -m 0644 vps/admin/relay-admin.service /etc/systemd/system/lightnode-relay-admin.service
sudo systemctl daemon-reload
sudo systemctl enable --now lightnode-relay-admin
```

宿主下载功能还需要：

```text
/etc/wireguard/host/host.conf                 0600 root:root
/opt/lightnode-relay-admin/HostScreenRelay.pkg 0644 root:root
```

## 3. 宿主 Mac

开发者构建 PKG：

```bash
./host-mac/scripts/package/build-host-pkg.sh
```

普通安装者直接访问广州后台，点击“下载安装包”，解压 ZIP 并双击
`安装 Host Screen Relay.command`。不要只双击 PKG；PKG 只落盘文件，`.command` 才会
导入 `host.conf`、识别 UTM bridge、启动服务并执行诊断。

安装时需要满足：

- Apple Silicon Mac；
- UTM Guest 已启动；
- 宿主能访问 Guest 当前地址，默认 `10.211.55.4`；
- 旧宿主已经停用，避免复用 `10.88.0.2` 身份。

## 4. Guest Sunshine

Guest 只安装官方 Sunshine，并授予屏幕录制、辅助功能和系统要求的输入监控权限。
Sunshine 保持原生证书与 PIN 配对，不暴露管理端口到公网。

## 5. 远端 Mac

严格按照 [`../remote-mac/README.md`](../remote-mac/README.md) 操作。远端 Mac 不安装
宿主 PKG，只从后台生成并导入自己的 `client.conf`。

## 发布前缺口

当前 PKG 未签名，后台使用 HTTP 和弱口令，只适用于当前自用环境。正式交付前需要：

- Developer ID Installer 签名；
- Apple 公证；
- HTTPS；
- 更换后台密码；
- 增加 Intel 包时单独准备并验证 `amd64` WireGuard 组件。
