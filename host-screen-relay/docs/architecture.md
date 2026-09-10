# Architecture

## 目标

远端 Mac 通过广州 VPS 访问宿主 Mac 后面的 UTM Guest Sunshine。广州链路只承载远程
桌面私网，不接管远端 Mac 的普通互联网流量，也不参与 Guest 的东京出口保护。

## 数据流

```text
Moonlight
  → 远端 WireGuard Peer 10.88.0.10-253
  → 广州 VPS wg0 10.88.0.1
  → 宿主 WireGuard Peer 10.88.0.2
  → 宿主 PF/SNAT
  → UTM bridge
  → Guest Sunshine 10.211.55.4
```

宿主本机使用 Moonlight 时，为防止直接走 UTM bridge，连接虚拟入口 `10.88.0.254`：

```text
宿主 Moonlight → 10.88.0.254 → 广州 VPS hairpin → 宿主 PF/SNAT → Guest
```

Guest 访问互联网仍然是：

```text
Guest → mac-guard/sing-box → 东京 VPS 149.104.78.121
```

## 地址与端口

| 值 | 含义 |
| --- | --- |
| `106.53.243.63` | 广州 VPS 公网地址 |
| `106.53.243.63:443/UDP` | WireGuard Endpoint |
| `106.53.243.63:9185/TCP` | 广州管理后台 |
| `10.88.0.1` | VPS WireGuard 地址 |
| `10.88.0.2` | 唯一宿主 Mac Peer |
| `10.88.0.10-253` | 后台分配的远端 Mac Peer |
| `10.88.0.254` | 宿主 Moonlight 强制绕广州的 hairpin 入口 |
| `10.211.55.4` | 当前 UTM Guest / Sunshine 地址 |

Sunshine 只放行所需端口：TCP `47984`、`47989`、`48010`，UDP `47998`、`47999`、
`48000`、`48002`、`48010`。VPS 和宿主 PF 均不允许远端 Peer 任意访问 UTM 网段。

## 组件边界

| 位置 | 组件 | 负责 | 不负责 |
| --- | --- | --- | --- |
| VPS | `wg-quick@wg0` | Hub、Peer 路由、hairpin | Guest 公网代理 |
| VPS | Relay Admin | Peer 生命周期、配置和 PKG 下载 | 数据转发 |
| 宿主 | WireGuard + PF/SNAT | 广州私网和 Sunshine 端口转发 | Guest 东京出口 |
| Guest | Sunshine | 采集、编码、输入和配对 | WireGuard Hub |
| Guest | mac-guard | 东京出口与防泄漏 | 广州远控链路 |
| 远端 | WireGuard + Moonlight | 发起远控 | 宿主 PF 管理 |

宿主配置 `10.88.0.2` 是单例身份。两台宿主同时使用同一配置会导致 WireGuard Endpoint
漂移。远端客户端必须一机一密钥，可以独立撤销。
