# LightNode Mac Guard V2 全局日本设计

## 1. 定位

这台 Mac 是 Claude、ChatGPT、Google 和 YouTube 专用机器。V2 不提供国内直连、
第二 VPN 或逐连接人工分类。

```text
整台 Mac 的全部公网流量
          |
          v
   sing-box Global TUN
          |
          v
 VLESS Reality / TCP 443
          |
          v
 日本 VPS 149.104.78.121
```

唯一策略档位：

```json
{
  "profile": "global_japan"
}
```

## 2. 安全不变量

1. 整台机器所有公网连接只能通过日本 VPS。
2. Claude/ChatGPT 进程和 AI 域名必须明确标记为 `ai_fixed`。
3. 其他连接标记为 `japan`，实际仍使用同一个日本 VLESS 出站。
4. 日本线路、sing-box、DNS 或 PF 异常时，公网失败关闭。
5. 不允许 Wi-Fi 公网回落。
6. 不允许第二 VPN、Clash、系统代理或未知全局 TUN 抢占。
7. IPv6 在完整代理能力验证前保持阻断。
8. 局域网 SSH 和屏幕共享始终保留。

## 3. 路由顺序

```text
1. DNS                         -> hijack-dns
2. Claude/ChatGPT 进程         -> ai-fixed
3. Claude/Anthropic/OpenAI 域名 -> ai-fixed
4. 管理局域网                  -> direct-local
5. 其他全部                    -> ai-fixed
```

这里没有 DIRECT 公网出站、第二 VPN 出站、selector 或 fallback。

AI 进程识别：

```text
/Applications/Claude.app/**
/Applications/ChatGPT.app/**
/Users/*/Applications/Claude.app/**
/Users/*/Applications/ChatGPT.app/**
```

域名识别：

```text
claude.ai
claude.com
anthropic.com
chatgpt.com
openai.com
oaistatic.com
oaiusercontent.com
```

进程规则用于证明桌面 App 的全部连接属于 AI；即使出现新域名，最终出口仍然是
日本。域名规则用于识别浏览器、OAuth 和 WebView 中的 AI 请求。

## 4. DNS

```text
macOS DNS
    -> 172.19.0.2
    -> sing-box hijack-dns
    -> Cloudflare DoH
    -> ai-fixed
    -> 日本 VPS
```

- 系统 DNS 固定为 `172.19.0.2`。
- 普通 UDP/TCP 53 请求由 TUN 劫持。
- DoH 上游明确绑定日本出站。
- DNS 失败时不回落 DHCP 或 ISP DNS。
- 应用自带 DoH 是普通 HTTPS，也会被全局 TUN 接管。

## 5. IPv6

```text
DNS strategy: ipv4_only
physical public IPv6: block
IPv6 leak probe: required
```

`curl -6` 探针只能失败或返回 `149.104.78.121`，不能返回本地公网 IPv6。

## 6. PF Kill Switch

PF 主规则允许：

```text
loopback
当前 LightNode TUN
当前管理局域网
DHCP
物理网卡 -> 149.104.78.121:443/TCP
```

独立 Anchor 阻断：

```text
其他所有物理 IPv4 公网出口
所有物理 IPv6 公网出口
```

sing-box 崩溃后 TUN 路由消失，但 PF 继续阻断 Wi-Fi 公网。应用不能在 launchd
重启 sing-box 的间隔中回落。

## 7. 启动与恢复

```text
macOS boot
  -> PF fail-closed
  -> Guard
  -> sing-box
  -> TUN
  -> DNS 172.19.0.2
  -> 验证出口 149.104.78.121
  -> HEALTHY
```

Guard 检查并恢复：

- sing-box 和 launchd。
- TUN 地址与接口。
- PF 主规则和 Anchor。
- DNS 路径。
- 东京公网出口。
- IPv6 防泄漏。
- 系统代理、Clash/Mihomo、FlClash、系统 VPN 和路由冲突。

已知代理冲突会被关闭；未知系统 VPN 保持 PF 锁定并告警，避免自动切断管理通道。

## 8. 连接观测

Guard 从 sing-box loopback Clash API 读取：

```text
进程路径
目标域名/IP
端口和协议
自动标签
实际出站链
流量计数
策略结果
```

只保留实时连接，不存在候选队列、状态审批或人工出口按钮。

自动标签只有：

| 标签 | 含义 | 实际出口 |
| --- | --- | --- |
| `ai_fixed` | Claude/GPT 进程或 AI 域名 | 日本 VPS |
| `japan` | 系统和其他应用 | 日本 VPS |

实际链路不是 `ai-fixed` 时产生最高级策略违例。

不采集消息正文、Token、Cookie、HTTP 参数或数据包载荷。

## 9. 后台

- 总览：当前状态、东京出口、TUN、DNS、PF和全局流量去向。
- 连接观察：实时连接、自动标签、实际链路和违例。
- 系统检查：底层组件和网络测试。
- 事件记录：故障、恢复和策略违例。

后台只监听 `127.0.0.1:9180`，通过 SSH 转发访问。

## 10. 故障矩阵

| 故障 | 结果 |
| --- | --- |
| 日本 VPS 故障 | 全部公网阻断 |
| sing-box 崩溃 | 全部公网阻断，局域网管理保留 |
| DNS 故障 | 域名连接失败，不回落本地 DNS |
| PF 被关闭 | Guard 高优先级告警并恢复 |
| 系统代理启动 | 关闭代理并恢复 LightNode |
| Clash/Mihomo 启动 | 关闭冲突进程并恢复 LightNode |
| 系统 VPN 启动 | PF 阻断、后台锁定告警 |
| 原生 IPv6 出现 | 阻断并告警 |

## 11. 验收

正常状态：

```text
ChatGPT.app                  -> ai_fixed -> 149.104.78.121
Claude.app                   -> ai_fixed -> 149.104.78.121
browser AI domain            -> ai_fixed -> 149.104.78.121
Google / YouTube             -> japan    -> 149.104.78.121
system services              -> japan    -> 149.104.78.121
DNS                          -> 172.19.0.2
native public IPv6           -> unavailable
```

故障测试：

```text
stop sing-box                -> public Internet fails
block 149.104.78.121:443     -> public Internet fails
change system DNS            -> Guard restores 172.19.0.2
start Clash                  -> conflict detected and closed
switch network               -> no local-public fallback
local SSH / screen sharing   -> remains available
```

## 12. 当前状态

虚拟测试 Mac `johankoi@10.211.55.4`：

```text
version: 0.2.0-alpha.7
profile: global_japan
mode: enforce
TUN: enabled
Tokyo egress: enabled
connection observer: enabled
PF: enabled and committed
```

PF 故障阻断、恢复和代理冲突恢复测试均已通过；只有全部组件健康时，后台才显示
“网络保护正常”。

后台的“安全暂停”先通过 AppleScript 请求 Claude、ChatGPT 正常退出，再卸载并
禁用 sing-box launchd 服务，同时保留已提交的 PF 规则。App 拒绝退出时不强杀，
而是记录告警并继续安全暂停。此时状态为 `maintenance`，公网不可用但局域网管理
保留；“启动保护”重新加载隧道，刷新 DNS/PF，并且只在出口验证为
`149.104.78.121` 后退出暂停状态，不会自动打开 AI App。

## 13. AppleScript 边界

AppleScript 只允许：

- 让 GUI App 正常退出。
- 发送 macOS 通知。
- 用户明确触发操作时显示一次管理员授权弹窗。

AppleScript 不参与连接采集、进程识别、路由、DNS、PF、保活或故障状态机。
