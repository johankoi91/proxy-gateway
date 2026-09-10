# Host Screen Relay

本工程只负责 `macOS-johankoi` 外部的远程屏幕传输。它不修改 Guest 内的 mac-guard、
PF、DNS、TUN、sing-box 或东京 VPS 配置。

## 唯一架构

```text
远端实体 Mac
  官方 WireGuard App + Moonlight
        -> WireGuard UDP 106.53.243.63:443
        -> 广州 VPS 标准 wg-quick@wg0
        -> 宿主 Mac 标准 wg-quick + wireguard-go
        -> 宿主局部 PF/SNAT
        -> UTM Guest 10.211.55.4
        -> 官方 Sunshine

宿主 Mac 自测
  Moonlight -> 10.88.0.254
        -> 同一广州链路返回宿主
        -> PF/SNAT -> Guest Sunshine
```

远端实体 Mac 在 Moonlight 中连接 `10.211.55.4`。宿主 Mac 因为能通过 `bridge100`
直达 Guest，使用虚拟入口 `10.88.0.254` 强制绕广州。Sunshine、Moonlight和 WireGuard
均使用上游成熟实现；本工程不包含 RustDesk、FRP、Nginx、Tailscale 或自研 WireGuard
协议控制器。

## 组件职责

| 位置 | 组件 | 职责 |
| --- | --- | --- |
| 广州 VPS | `wg-quick@wg0` | WireGuard Hub、Peer 间转发、宿主 hairpin NAT |
| 宿主 Mac | Homebrew `wg-quick`/`wireguard-go` | 连接广州，仅路由远控私网 |
| 宿主 Mac | `lightnode-sunshine-route` + PF | 把授权 Sunshine 端口转给 UTM Guest |
| Guest | 官方 Sunshine | 屏幕采集、编码、输入控制和原生证书配对 |
| 远端 Mac | 官方 WireGuard App | 导入标准 `client.conf` |
| 远端 Mac | 官方 Moonlight | 连接和控制 Sunshine |

Guest 的公网业务仍独立走：

```text
Chrome / Claude -> mac-guard -> sing-box TUN -> 东京 VPS 149.104.78.121
```

广州远控链路终止在宿主，不进入 Guest 的物理公网出口设计。

## 广州 VPS

首次安装前安装发行版提供的 `wireguard-tools` 和 `iptables`。然后执行：

```bash
sudo ./vps/install-relay.sh --host-public-key '<host-public-key>'
```

安装器生成或保留 `/etc/wireguard/server.key`，创建 `/etc/wireguard/wg0.conf`，并启用
`wg-quick@wg0.service`。自定义部分只剩 `/usr/local/sbin/lightnode-relay-rules`：只允许
Peer 到 Guest Sunshine 端口及其回程，并为宿主 `10.88.0.254` 提供 DNAT/SNAT。其他
Peer 间转发继续由 FORWARD 默认策略阻断。

创建远端客户端：

```bash
sudo ./vps/add-remote-peer.sh \
  --name alice-mac \
  --address 10.88.0.10 \
  --endpoint 106.53.243.63:443
```

命令只输出生成文件路径。下载该 `client.conf` 后导入远端 Mac 的官方 WireGuard App。
每台设备必须使用唯一密钥和 `10.88.0.x` 地址。

撤销设备：

```bash
sudo ./vps/remove-remote-peer.sh alice-mac
```

撤销脚本立即删除运行态 Peer，并把客户端目录移动到
`/etc/wireguard/revoked/<name>-<UTC时间>`，便于审计和必要恢复，不使用数据库。

## 宿主 Mac

宿主依赖 Homebrew 官方包：

```bash
brew install wireguard-tools wireguard-go
```

从 `config/host.example.conf` 生成唯一的宿主配置后执行：

```bash
sudo ./scripts/install-host-relay.sh \
  --wireguard-config /path/to/host.conf \
  --host-user "$(id -un)" \
  --guest-ip 10.211.55.4
```

安装结果：

```text
/Library/Application Support/LightNodeHostRelay/host.conf
/Library/Application Support/LightNodeHostRelay/relay.conf
/Library/LaunchDaemons/com.lightnode.host-wireguard.plist
/Library/LaunchDaemons/com.lightnode.sunshine-route.plist
/usr/local/libexec/lightnode-sunshine-route
/etc/pf.anchors/com.lightnode.sunshine-route
```

WireGuard和 PF 配置为 `0600 root:wheel`，日志统一放在
`/Library/Logs/LightNodeHostRelay/`。

安装器根据 Guest IP 从路由表自动识别 UTM bridge 和 bridge IPv4，不固定依赖
`bridge100` 或 `10.211.55.1`。安装时 Guest 必须已启动并可由宿主访问。

宿主 PF 不重载 `/etc/pf.conf`。远控规则安装到：

```text
com.apple.internet-sharing/lightnode-sunshine-route
```

守护进程只在局部父 anchor 缺失、WireGuard接口变化或规则丢失时重载，并保留 Apple
`shared_v4/shared_v6` 规则。这样 UTM 重启后能自动恢复调用关系，不会破坏
`bridge100` 或 Internet Sharing。

检查：

```bash
sudo ./scripts/doctor-host-relay.sh
```

doctor 验证 WireGuard地址、局部 PF 调用和宿主经广州访问 Sunshine 的三个 TCP 端口。

停止运行态：

```bash
sudo ./scripts/uninstall-host-relay.sh --yes
```

卸载脚本先停止服务、清空远控子 anchor，再恢复仅包含 Apple shared network 的父规则。
`--yes` 保留 root-only 配置，`--purge` 同时删除配置与私钥。

## Guest Sunshine

Guest 只安装官方 `Sunshine.app`。需要手工授予 macOS TCC 权限：

```text
屏幕与系统音频录制
辅助功能
输入监控（系统要求时）
```

Sunshine 使用原生 HTTPS、自签服务端证书和客户端证书配对。首次连接时在
`https://localhost:47990/pin` 输入 Moonlight显示的四位 PIN。配对成功后不再需要 PIN。

不要反复配对同一个 Moonlight 证书。Sunshine 要求一个证书只对应一个启用设备；重复
记录会导致 HTTP/XML `401`。

## 远端实体 Mac

1. 从广州 VPS 创建并下载唯一 `client.conf`。
2. 安装官方 WireGuard App 并导入配置。
3. 确认隧道连接。
4. 安装官方 Moonlight。
5. 添加主机 `10.211.55.4`。
6. 完成一次 Sunshine PIN 配对。
7. 点击 `Desktop`。

远端配置必须保持：

```ini
AllowedIPs = 10.88.0.0/24, 10.211.55.4/32
PersistentKeepalive = 25
```

不能配置 `0.0.0.0/0`，因此普通浏览仍使用远端 Mac 自己的网络。

## 验收

```text
广州 VPS: systemctl is-active wg-quick@wg0
广州 VPS: wg show wg0
宿主: sudo scripts/doctor-host-relay.sh
Guest: Sunshine 47984/47989/48010 TCP 监听
Guest: mac-guard state=healthy, protected=true, connected=true
远端: Moonlight Desktop 可连接，WireGuard计数持续增加
```

还必须覆盖广州 VPS 重启、宿主重启、UTM Guest 重启、宿主切换 Wi-Fi、远端 Peer
漫游、Sunshine 重启和重复证书清理。宿主自测必须确认 Moonlight UDP socket 绑定
`10.88.0.2`，VPS WireGuard计数同步增长，不能回落 `bridge100` 直连。

## 当前交付边界

- `Sunshine-master.zip` 是用户提供的源码参考，不参与安装。
- Sunshine 和 Moonlight沿用官方发布包，不在本工程重新签名或打包。
- 宿主当前依赖 Homebrew WireGuard，不再维护自研 Peer.pkg。
- 正式对外分发宿主安装器前，仍需封装签名 PKG、Developer ID Installer 签名与公证。
