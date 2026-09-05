# DNS 防泄漏设计

## 目标

DNS 层的目标是防止 DNS 泄漏、DNS 污染，以及隧道断开后自动回落到本地运营商
DNS。DNS 保护不是独立的安全边界，它与 sing-box 全局 TUN、PF Kill Switch、
IPv6 阻断和固定出口检测共同工作。

## 当前链路

```text
App / macOS
    |
    v
系统 DNS：172.19.0.2
    |
    v
sing-box TUN：172.19.0.1/30
    |
    v
DNS 协议劫持：hijack-dns
    |
    v
Cloudflare DoH：104.16.249.249:443
TLS SNI：cloudflare-dns.com
    |
    v
VLESS Reality
    |
    v
日本 VPS：149.104.78.121
```

DoH 上游显式指定 `detour: ai-fixed`，因此 DNS 查询从日本 VPS 发出。本地运营商
只能看到 Mac 与 `149.104.78.121:443` 的加密连接，看不到查询的具体域名。

## 系统 DNS

macOS 当前网络服务的 DNS 固定为 `172.19.0.2`。这是 sing-box 根据
`172.19.0.1/30` TUN 地址提供的内部 DNS 地址，不是本地路由器或运营商 DNS。

远端 Mac 上可用以下命令检查：

```bash
scutil --dns
```

正常状态应出现：

```text
nameserver[0] : 172.19.0.2
```

## 普通 DNS 劫持

sing-box 路由包含显式的 `hijack-dns` 动作。即使应用写死以下服务器：

```text
8.8.8.8:53
114.114.114.114:53
本地路由器:53
```

其 UDP/TCP 53 请求进入全局 TUN 后，仍会交给 sing-box DNS 模块处理，不会直接
发送到目标 DNS 服务器。

PF 同时禁止未经允许的物理网卡公网连接，所以应用无法通过绕过系统 DNS 设置来
直接访问公网 53、853 或其他 DNS 服务。

## 应用自带 DoH

GPT、Claude 或浏览器可能内置 DoH。加密后的 DoH 对 sing-box 来说是普通 HTTPS
流量，不能依赖 `hijack-dns` 读取或改写其查询内容。

当前采用全局 TUN，所以这类 HTTPS 连接本身仍会经过 VLESS 和日本 VPS。DoH
提供商看到的是 VPS 出口，而不是 Mac 的本地公网地址。

只有在应用被排除出 TUN、另一个 VPN 抢占路由，或 Kill Switch 被关闭时，应用
自带 DoH 才可能从本地网络发出。

## IPv6

sing-box DNS 使用 `ipv4_only` 策略，尽量只向应用提供 IPv4 结果。PF 在物理网卡
上阻断其他 IPv6 公网出口，防止出现以下双出口状态：

```text
IPv4 -> 日本 VPS
IPv6 -> 本地运营商
```

Guard 的 IPv6 探针允许两种安全结果：

- IPv6 不可用。
- 使用 `curl -6` 发起的探针被 TUN 接管，最终返回日本 VPS 的 IPv4。

只有获得真实公网 IPv6 地址时才判定为 IPv6 泄漏。

## 网络切换与自动修复

Guard 检查以下状态：

- 域名是否能正常解析。
- `scutil --dns` 是否包含 `172.19.0.2`。
- sing-box 进程和 launchd 是否运行。
- TUN 是否持有 `172.19.0.1`。
- 固定出口是否为 `149.104.78.121`。

切换 Wi-Fi 或有线网卡后，如果 macOS 将 DNS 恢复为路由器或运营商地址，Guard
会在 sing-box 与 TUN 健康的前提下调用 `lightnode-guard dns refresh`：

1. 使用 `scutil --nwi` 识别当前物理接口。
2. 使用 `networksetup -listnetworkserviceorder` 找到对应网络服务。
3. 将该服务的 DNS 重新设置为 `172.19.0.2`。
4. 重新执行 DNS 与出口检查并记录事件。

DNS 自动修复使用 30 秒限频，避免网络抖动时反复修改系统设置。

## 故障行为

正常状态：

```text
DNS 查询 -> sing-box -> VLESS -> 日本 VPS -> DoH
```

sing-box、VPS 或 DoH 故障时：

```text
172.19.0.2 不可用
       +
PF 阻断其他公网出口
       =
DNS 失败、网络失败，不回落到本地 DNS
```

这是有意的 fail-closed 行为。对 Claude/GPT 而言，连接失败比短暂使用本地公网和
本地 DNS 更安全。

卸载 LightNode Mac Guard 时，卸载器会在停止 sing-box 前把当前网络服务恢复为
自动 DNS。已提交 Kill Switch 时卸载器会拒绝继续，必须先显式关闭 Kill Switch。

## 验证方法

```bash
# 系统 DNS 路径
scutil --dns

# 直接查询 TUN DNS
dig @172.19.0.2 A www.youtube.com

# IPv4 固定出口
curl --noproxy '*' -4 https://api.ipify.org

# IPv6 防泄漏
curl --noproxy '*' -6 https://api64.ipify.org

# Guard 综合状态
curl -u admin http://127.0.0.1:9180/api/snapshot
```

预期公网出口为 `149.104.78.121`，YouTube 应解析到 Google 地址。IPv6 探针应
失败或返回 `149.104.78.121`，不能返回本地公网 IPv6。

## 边界与限制

- DoH 提供商仍能看到从 VPS 发出的 DNS 查询；本设计防止的是本地 ISP 泄漏，
  不是让 DNS 对所有上游完全不可见。
- DNS 正常不等于所有业务流量都使用正确出口，因此 Guard 还必须独立验证公网
  出口 IP。
- 当前是固定的全局隧道方案，所有公网业务和 DNS 都经过日本 VPS，不提供国内
  GeoIP/域名分流。
