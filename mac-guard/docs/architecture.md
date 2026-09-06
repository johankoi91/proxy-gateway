# LightNode Mac Controller 架构设计

## 1. 目标与边界

这台 Mac 是 Claude、ChatGPT、Google 和 YouTube 等境外服务的专用机器。系统的
唯一公网出口是东京 VPS `149.104.78.121`：

```text
整台 Mac 的公网流量
        -> sing-box TUN
        -> VLESS Reality / TCP 443
        -> 东京 VPS 149.104.78.121
        -> Internet
```

系统不提供以下能力：

- 国内流量直连。
- 第二 VPN 或 Clash 共存。
- 按连接选择出口。
- 东京线路故障后回落到 Wi-Fi 公网。

局域网带外管理、DHCP 和到 VPS Endpoint 的连接属于维持管理与重连所需的
明确例外，不属于公网回落。

## 2. 安全不变量

1. 所有公网业务和 DNS 只能从 `149.104.78.121` 发出。
2. sing-box、VPS、DNS 或出口验证失败时，公网失败关闭。
3. PF Kill Switch 独立于 sing-box 存活，隧道崩溃时继续阻断物理网卡。
4. IPv6 不能成为绕开 IPv4 隧道的第二出口。
5. 系统代理、其他 VPN 和未知全局 TUN 不能抢占默认公网路径。
6. 只有出口探针确认东京 IP 后，系统才允许从安全暂停恢复为健康状态。
7. 解除 PF 必须由 root 在本机 CLI 显式传入 `--yes`，后台不提供该操作。

## 3. 组件架构

```text
┌──────────────────────────── macOS ─────────────────────────────┐
│                                                               │
│  Claude / ChatGPT / Browser / System                          │
│                 │                                             │
│                 ▼                                             │
│        sing-box TUN 172.19.0.1/30                             │
│          │                    │                                │
│          │ 业务流量           │ DNS 172.19.0.2                 │
│          │                    ▼                                │
│          │             hijack-dns -> Cloudflare DoH           │
│          └──────────────┬───────────────────────┐              │
│                         ▼                       │              │
│               VLESS Reality / TCP 443           │              │
│                         │                       │              │
│  PF Kill Switch         │      Go Controller         │              │
│  启动引导/阻断物理回落    │      检测/恢复/API       │              │
└─────────────────────────┼───────────────────────┼──────────────┘
                          ▼                       │
                149.104.78.121 东京 VPS           │
                          │                       │
                          ▼                       │
                       Internet                  │
```

常驻业务进程只有两个：

| 进程 | 职责 |
| --- | --- |
| `sing-box` | TUN、DNS、VLESS Reality 和连接观测 API |
| `lightnode-controller` | 健康检查、自动恢复、管理后台、事件和 Go 系统命令 |

PF 在 macOS 内核中执行，React 已编译进 Controller，launchd 是系统自带服务管理器，均
不增加项目常驻进程。

## 4. 业务数据流

普通 HTTPS 请求：

```text
Claude.app
  -> macOS 默认路由
  -> TUN utunN
  -> sing-box sniff / route
  -> tokyo 出站
  -> VLESS Reality
  -> 149.104.78.121:443
  -> 目标服务
```

Claude、ChatGPT、Google、YouTube 和系统服务都走同一个 `tokyo` 出站。系统不再
识别 AI 进程或 AI 域名，也不产生连接分类标签。即使目标只有 IP、使用 ECH 或无法
取得进程路径，sing-box 的最终规则仍是 `tokyo`，没有 DIRECT、selector 或
fallback 出站。

## 5. DNS 数据流

```text
App / macOS
  -> 系统 DNS 172.19.0.2
  -> sing-box TUN DNS
  -> hijack-dns
  -> Cloudflare DoH 104.16.249.249:443
  -> tokyo
  -> 东京 VPS
```

`172.19.0.2` 是 TUN 内部 DNS 地址，不是路由器或公共 DNS 地址。它解决三个问题：

- 避免查询发送给本地路由器或运营商。
- 避免切换网络后自动使用 DHCP 下发的 DNS。
- 隧道故障时让 DNS 与业务一起失败，而不是单独回落。

普通 UDP/TCP 53 由 sing-box 劫持。应用自带 DoH 无法被读取，但它与 DoH 服务器
之间的 HTTPS 连接仍由全局 TUN 接管，因此出口仍是东京 VPS。

## 6. PF Kill Switch

PF 主规则只放行：

```text
loopback
当前 LightNode TUN
当前管理局域网
DHCP
物理网卡 -> 149.104.78.121:443/TCP
```

独立 Anchor 最终阻断其余 IPv4 和 IPv6 出口：

```text
block drop out quick inet all
block drop out quick inet6 all
```

PF 与 TUN 的职责不同：TUN 决定正常流量如何到日本，PF 决定 TUN 不可用时哪些流量
绝对不能从物理网卡发出。sing-box 停止后路由表可以恢复到 `en0`，但 PF 仍会阻止
百度、Claude、GPT 和其他公网连接，只有局域网管理和 VPS Endpoint 保留。

首次启用 PF 使用限时试运行：Go 先备份 `/etc/pf.conf` 和旧 Anchor，生成随机
Token，再注册 launchd 回滚任务。只有出口和 Anchor 均验证正确后执行确认，才会
写入 `firewall.committed` 并启用自动修复。

### 6.1 启动边界

macOS 自带 `com.apple.pfctl` 会加载 `/etc/pf.conf`，但不会自动启用 PF。Controller
安装独立的 root LaunchDaemon `com.lightnode.pf-bootstrap`：只要检测到已提交的
`firewall.committed`，就在用户登录与业务进程之前校验主配置和 fail-closed Anchor，
执行 `pfctl -f` 并在需要时执行 `pfctl -e`。它不等待 sing-box、TUN 或 DNS。

新装且尚未提交 Kill Switch 时，PF 引导明确跳过，避免用占位配置锁死机器。

## 7. IPv6

sing-box DNS 使用 `ipv4_only`，PF 阻断物理接口的公网 IPv6。Controller 的 IPv6 探针
接受两种安全结果：

- IPv6 不可用。
- `curl -6` 被 TUN 接管，最终观察到东京 VPS 的 IPv4。

如果探针返回原生公网 IPv6，组件立即判定失败。

## 8. 控制与状态流

Go 二进制有两种运行方式：

```text
lightnode-controller                  -> 常驻 Controller / HTTP 服务
lightnode-controller <系统子命令>      -> 短时特权操作
```

后台操作链：

```text
React
  -> POST /api/actions/*
  -> Controller
  -> lightnode-controller tunnel start|pause|restart
  -> Go sysmanage
  -> 固定参数调用 launchctl/pfctl/networksetup/scutil
  -> 重新检查
  -> Snapshot
  -> React
```

Go 使用 `exec.CommandContext` 和完整命令路径，不经过 `sh -c`。超时会终止子进程，
配置文件和 PF 文件采用同目录临时文件加原子替换。

## 9. 冲突处理

Controller 每 10 秒执行完整检查，每 2 秒巡检代理/VPN 冲突。冲突包括：

- macOS HTTP、HTTPS、SOCKS、PAC 或自动代理。
- Clash Verge、Mihomo 或 FlClash 进程。
- `scutil --nc` 报告的已连接系统 VPN。
- 公网探针路由不再指向 LightNode TUN。

PF 是零等待的第一道边界。轮询用于发现、记录和恢复，不承担防止泄漏的首要职责。
已知代理会被关闭；未知系统 VPN 不会被盲目杀死，以免切断 SSH 或企业管理通道，
但其公网连接仍受 PF 阻断，后台保持故障锁定并告警。

## 10. AppleScript 边界

AppleScript 仅用于：

- 安全暂停前请求 Claude、ChatGPT 正常退出。
- 发送 macOS 通知。

AppleScript 不参与路由、DNS、PF、进程观测、保活或故障状态判断。App 拒绝退出时
只记录警告，不强杀；PF 仍负责阻断公网。

## 11. 信任边界

- 获得 root 权限的软件可以修改 PF、停止 Controller 或替换二进制，本系统不对抗同级
  root 攻击者。
- DoH 上游仍能看到由 VPS 发出的 DNS 查询；设计目标是避免本地 ISP 泄漏。
- 管理后台只监听 `127.0.0.1:9180`。远程访问应使用 SSH 转发，不应开放公网端口。
- `protected=true` 表示 PF 防回落生效；`connected=true` 表示完整东京链路可用。
  两者必须分开理解。

## 12. 节点配置持久化

节点配置不使用数据库，以下两个 JSON 是唯一数据源：

```text
/Library/Application Support/LightNodeController/config.json
/Library/Application Support/LightNodeController/sing-box.json
```

后台只返回 Endpoint、flow、TLS SNI 和指纹；UUID、Reality 公钥、short ID 只返回
是否已配置。保存使用候选文件校验和同目录原子替换，然后按新 Endpoint 重建 PF、
启动隧道并验证出口。任一步失败都会恢复旧文件和旧 PF 规则，公网始终失败关闭。

VPS 后台导出的 `lightnode.vps-config` v1 可以直接导入。导入只接受 VLESS TCP、
Reality、`xtls-rprx-vision` 和完整客户端凭据。导入、手工修改和恢复前都会创建
root-only 备份；后台只返回备份时间、原因与 Endpoint，不返回原始 JSON 或凭据。

## 13. 配置与日志生命周期

配置备份位于 `/Library/Application Support/LightNodeController/backups`，权限为
`0700/0600`，默认保留 10 份。Controller、sing-box、PF 引导和事件日志超过 5 MiB
后执行 copy-truncate，默认保留 3 份，避免 launchd 持有旧文件描述符导致轮转失效。
