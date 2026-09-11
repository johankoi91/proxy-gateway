# Browser Workspace 当前实现技术路线

## 1. 文档范围

本文记录当前已经部署并验证的远程 Chromium 技术路线。

当前状态：

- UTM Ubuntu 已运行 Docker、KasmVNC 和 Chromium。
- Ubuntu 内不安装 WireGuard。
- 广州 WireGuard 由宿主 Mac 持有。
- 宿主 Mac 使用 PF 将 KasmVNC 流量转发到 Ubuntu。
- 广州 VPS 只提供 WireGuard Hub 和 `10.88.0.254` hairpin。
- Linux Controller、东京 TUN、DNS 防泄漏和 nftables Kill Switch 尚未部署。

因此，本方案当前可以验证远程 Chromium 的连接与交互，但还不能用于
Claude、ChatGPT 等要求固定东京出口和严格防泄漏的账号。

## 2. 总体架构

```text
远端 Mac / 宿主 Mac 模拟端
        |
        | WireGuard, UDP 443
        v
广州 VPS 106.53.243.63
        |
        | hairpin 10.88.0.254:8443
        | DNAT -> 10.88.0.2:8443
        v
宿主 Mac
  WireGuard: 10.88.0.2
        |
        | PF rdr + nat
        | 10.88.0.2:8443 -> 10.211.55.7:8443
        v
UTM Ubuntu 10.211.55.7
        |
        | Docker port publish
        v
KasmVNC 1.5.0 + Openbox + Chromium
```

浏览器访问互联网是另一条链路：

```text
Chromium container
        |
        v
Docker bridge
        |
        v
Ubuntu enp0s1
        |
        v
UTM Shared Network / 10.211.55.1
        |
        v
宿主 Mac 当前默认网络
        |
        v
Internet
```

当前 Chromium 出口不是广州 VPS，也不是东京 VPS。

## 3. 地址与端口

| 角色 | 地址或端口 | 用途 |
| --- | --- | --- |
| 广州 VPS 公网 | `106.53.243.63:443/UDP` | WireGuard Endpoint |
| 广州 WireGuard Hub | `10.88.0.1` | Peer Hub |
| 宿主 Mac WireGuard | `10.88.0.2` | VPS hairpin 的内部转发目标 |
| 统一用户入口 | `10.88.0.254:8443` | 本机模拟端和远端 Mac 唯一入口 |
| UTM 网关 | `10.211.55.1` | Ubuntu 的网关及 PF SNAT 地址 |
| UTM Ubuntu | `10.211.55.7` | KasmVNC 的实际监听主机 |
| 远端 Mac 示例 | `10.88.0.10` | 已批准的 WireGuard Peer |
| KasmVNC | `8443/TCP`, `8443/UDP` | 页面、WebSocket 和 WebUDP |

`10.88.0.2:8443` 是内部后端目标，不再作为用户入口。广州 VPS 的
FORWARD 规则要求连接的原始目标为 `10.88.0.254`。

## 4. 客户端数据流

### 4.1 远端 Mac

```text
远端浏览器
  -> 10.88.0.254:8443
  -> 远端 WireGuard
  -> 106.53.243.63:443/UDP
  -> VPS wg0
  -> DNAT 10.88.0.254 -> 10.88.0.2
  -> 宿主 WireGuard
  -> 宿主 PF rdr 10.88.0.2 -> 10.211.55.7
  -> 宿主 PF SNAT -> 10.211.55.1
  -> Ubuntu Docker
  -> KasmVNC
```

### 4.2 宿主 Mac 模拟远端

宿主也访问 `10.88.0.254:8443`。流量先去广州 VPS，再返回宿主，路径与
真正远端相同的后半段一致：

```text
宿主浏览器
  -> 宿主 WireGuard
  -> 广州 VPS
  -> hairpin 回宿主 WireGuard
  -> PF
  -> Ubuntu
  -> KasmVNC
```

这是真实绕广州的测试，不是访问 Ubuntu 私网的本地捷径。

### 4.3 宿主直连诊断

仅在排查问题时，可访问：

```text
https://10.211.55.7:8443/
```

这条路径不经过广州，不代表远端体验。

## 5. 各节点职责

### 5.1 UTM Ubuntu

虚拟机规格：

- Ubuntu Server 24.04.5 LTS ARM64
- Apple Virtualization
- 6 vCPU
- 8 GB 内存
- 64 GB 稀疏磁盘
- 根文件系统约 60 GB
- UTM Shared Network
- 主机名 `ubuntu-server`
- 私网地址 `10.211.55.7`

系统组件：

- Docker Engine 29.1
- Docker Compose 2.40
- OpenSSH Server
- KasmVNC/Chromium Docker 容器
- `tty1` 和 UTM `hvc0` 控制台自动登录 root；SSH root 仍只允许公钥

明确不包含：

- WireGuard
- `relay0`
- Colima
- Linux Controller
- sing-box TUN
- nftables Kill Switch

Ubuntu 的部署目录：

```text
/opt/lightnode/browser-workspace
```

容器端口只发布到 Ubuntu 私网地址：

```text
10.211.55.7:8443/tcp
10.211.55.7:8443/udp
```

### 5.2 宿主 Mac

宿主负责：

- 运行 UTM。
- 提供 `10.211.55.0/24` Shared Network。
- 持有广州 WireGuard Peer `10.88.0.2`。
- 使用 PF 将 KasmVNC TCP/UDP 转发到 Ubuntu。
- 对转入 Ubuntu 的流量做 SNAT，使 Ubuntu 看到来源 `10.211.55.1`。

PF 的关键行为：

```text
rdr: 10.88.0.2:8443 -> 10.211.55.7:8443
nat: 10.88.0.0/24 -> 10.211.55.7:8443, source -> 10.211.55.1
```

宿主不运行：

- KasmVNC
- Chromium 容器
- Colima
- Chromium 代理桥

PF 源文件：

```text
browser-workspace/host-mac/packaging/pf.browser-route.anchor
browser-workspace/host-mac/packaging/pf.internet-sharing-parent.with-browser
```

实际安装位置：

```text
/etc/pf.anchors/com.lightnode.browser-route
/etc/pf.anchors/com.lightnode.browser-internet-sharing-parent
```

PF 维护服务：

```text
com.lightnode.browser-route
```

### 5.3 广州 VPS

广州 VPS 负责：

- 在公网 `106.53.243.63:443/UDP` 接收 WireGuard。
- 为远端 Peer 和宿主 Peer 提供 `10.88.0.0/24` Hub。
- 将 `10.88.0.254:8443` DNAT 到宿主 `10.88.0.2:8443`。
- 对 hairpin 流量 SNAT 为 `10.88.0.1`。
- 只允许原始目标为 `10.88.0.254` 的 KasmVNC 转发。

广州 VPS 不运行 KasmVNC 或 Chromium，也不承担 Chromium 的互联网出口。

规则源文件：

```text
browser-workspace/vps/relay/lightnode-browser-relay-rules
```

实际安装位置：

```text
/usr/local/sbin/lightnode-browser-relay-rules
```

### 5.4 远端 Mac

远端 Mac 只需要：

- 官方 WireGuard App。
- 一个由广州后台批准并生成的 Peer 配置。
- 浏览器。

客户端配置必须覆盖：

```ini
AllowedIPs = 10.88.0.0/24
```

WireGuard 公网 Endpoint 保持：

```ini
Endpoint = 106.53.243.63:443
```

## 6. KasmVNC 和 Chromium 实现

源码目录：

```text
browser-workspace/kasmvnc
```

主要文件：

| 文件 | 作用 |
| --- | --- |
| `Dockerfile` | 构建 Debian ARM64、Chromium、Openbox 和 KasmVNC |
| `compose.yaml` | 容器、端口、volume、健康检查与重启策略 |
| `entrypoint.sh` | 初始化账号、刷新托管配置并启动 KasmVNC |
| `kasmvnc.yaml` | 分辨率、TLS、UDP 和编码参数 |
| `xstartup` | 启动 D-Bus 与 Openbox |
| `openbox-autostart` | 启动并自动拉起 Chromium |
| `vendor-kasmvnc.deb` | 官方 KasmVNC 1.5.0 ARM64 安装包 |

镜像和容器：

```text
image: lightnode/kasmvnc-chromium:1.5.0
container: lightnode-kasmvnc-chromium
volume: browser-workspace_chromium-home
```

Chromium profile、Cookie 和书签保存在 named volume。关闭 Chromium 后，
Openbox autostart 循环会在约 2 秒后重新启动浏览器。

健康检查同时验证：

- Xvnc 存活。
- 至少一个非 zombie Chromium 进程存活。

## 7. 当前性能参数

已从 Colima POC 同步的参数：

```text
resolution: 1440x900
max frame rate: 45
dynamic quality: 6-7
vertical scroll detection: enabled
horizontal scroll detection: enabled
scroll threshold: 10%
UDP public address: 10.88.0.254
UDP payload: 1200 bytes
```

运行验证：

- WebUDP 可以完成 `requesting upgrade to udp` 和 `WebUdp: client join`。
- WebP benchmark 约 4 ms。
- 滑动采样时容器 CPU 通常约 1% 到 4%。
- 容器内存约 560 MB。
- UTM Ubuntu 的编码性能不低于此前 Colima POC。

当前主要瓶颈是广州网络，而不是容器编码：

```text
宿主 -> Ubuntu 直连页面总耗时: 约 13-18 ms
宿主 -> 广州 hairpin -> Ubuntu: 通常约 800-900 ms
异常样本: 2.4-2.7 s
宿主 -> 广州 VPS ICMP RTT: 平均约 90 ms, 最大约 170 ms
```

因此，`.254` 用于验证真实中转行为时出现的卡顿，不能通过继续增加 CPU 或
简单提高 FPS 消除。真正有效的方向是降低中转 RTT、减少抖动，或者实现可靠
的点对点路径。

## 8. 使用方式

统一用户入口：

```text
https://10.88.0.254:8443/
```

当前 POC 凭据：

```text
username: admin
password: admin1111
```

首次访问需要接受 KasmVNC 自签名证书警告。正式交付前必须轮换当前 POC
密码，并考虑使用内部 CA 或受控证书分发。

宿主进入 Ubuntu：

```bash
ssh ubuntu-server
```

`ubuntu-server` SSH alias 使用 root 公钥登录。

## 9. 运维命令

### 9.1 容器状态

```bash
ssh ubuntu-server
cd /opt/lightnode/browser-workspace
docker compose ps
docker compose logs --tail=100 chromium
```

### 9.2 重启浏览器工作区

```bash
ssh ubuntu-server
cd /opt/lightnode/browser-workspace
docker compose restart chromium
```

### 9.3 重新构建

```bash
ssh ubuntu-server
cd /opt/lightnode/browser-workspace
docker compose up -d --build --force-recreate
```

### 9.4 宿主 PF

```bash
sudo launchctl print system/com.lightnode.browser-route
sudo pfctl -a com.apple.internet-sharing -sn
sudo pfctl -a com.apple.internet-sharing/lightnode-browser-route -sn
sudo pfctl -a com.apple.internet-sharing/lightnode-browser-route -sr
```

### 9.5 广州 VPS

```bash
ssh root@106.53.243.63
wg show wg0
iptables -nvL FORWARD
iptables -t nat -nvL PREROUTING
iptables -t nat -nvL POSTROUTING
```

## 10. 标准检查顺序

连接失败时按数据流顺序检查：

1. 远端 Mac 的 WireGuard 是否有最新握手。
2. 广州 VPS 是否收到目标为 `10.88.0.254:8443` 的 TCP/UDP。
3. VPS DNAT、SNAT 和 FORWARD 计数是否增加。
4. 宿主 `utun` 是否收到转发后的 `10.88.0.2:8443`。
5. 宿主 PF 是否加载 rdr、nat 和 pass 规则。
6. Ubuntu `10.211.55.7:8443` 是否监听 TCP/UDP。
7. Docker Compose 是否为 `healthy`。
8. KasmVNC 日志是否出现 WebSocket 认证和 WebUDP join。
9. Chromium 进程是否为非 zombie 状态。

快速分层测试：

```bash
# 宿主直接验证 Ubuntu/Kasm，不经过广州
curl -k -u admin:admin1111 https://10.211.55.7:8443/

# 宿主模拟远端，必须经过广州
curl -k -u admin:admin1111 https://10.88.0.254:8443/
```

## 11. 故障影响

| 故障 | 远端 Kasm | Ubuntu 当前出网 |
| --- | --- | --- |
| 广州 VPS 故障 | 不可访问 | 不受影响 |
| 宿主 WireGuard 故障 | 不可访问 | 不受影响 |
| 宿主 PF 故障 | 不可访问 | 不受影响 |
| UTM Shared Network 地址变化 | PF 目标失效 | 取决于新网络状态 |
| Ubuntu 停机 | 不可访问 | 不可用 |
| Docker/Kasm 故障 | 不可访问 | Ubuntu 本身仍可出网 |
| 宿主默认网络变化 | 远端路径可能重连 | Chromium 出口随之变化 |

当前方案尚未 fail-closed。Ubuntu 或 Chromium 在东京线路不存在时仍可能通过
UTM Shared Network 和宿主普通网络访问互联网。

## 12. 未来 Linux Controller 边界

Linux Controller 应在 Ubuntu 内实现：

- sing-box TUN 接管整机默认流量。
- 东京 VPS 作为唯一互联网出口。
- nftables Kill Switch。
- DNS 由隧道接管。
- IPv6 完整接管或阻断。
- 开机先锁网，再启动东京线路。
- 东京线路故障时保持断网，不回落到 UTM 网关。

必须保留的管理例外只有：

```text
inbound: 10.211.55.1 -> 10.211.55.7:8443/tcp
inbound: 10.211.55.1 -> 10.211.55.7:8443/udp
reply: established/related -> enp0s1 -> 10.211.55.1
```

该例外只允许宿主转入 KasmVNC，不允许 Chromium 新建连接绕过东京 TUN。

Linux Controller 不应在 Ubuntu 内重新引入广州 WireGuard。广州入口、hairpin
和远端 Peer 继续归宿主 Mac 与广州 VPS 管理；东京出口归 Ubuntu Linux
Controller 管理。两条职责保持分离。

## 13. 已废弃路线

Colima POC 已删除，不再属于现行实现：

- Colima profile、Docker runtime 和 volume 已删除。
- Colima/Lima 已从宿主卸载。
- Colima SSH 代理桥和 LaunchAgent 已删除。
- Ubuntu 不继承 Colima 的宿主 Clash 代理方案。

保留下来的只有经过验证并迁移到 UTM Ubuntu 的 KasmVNC 编码参数和容器构建
逻辑。
