# LightNode Mac Controller 管理后台

## 1. 定位

后台用于回答四个问题：

1. 这台 Mac 当前是否只能从东京 VPS 出口？
2. 如果线路异常，本地公网是否已被阻断？
3. 哪个组件故障、系统采取了什么恢复动作？
4. 当前节点如何配置，切换后是否通过出口验证？

后台只监听 `127.0.0.1:9180`。React 静态资源嵌入 `lightnode-controller`，不需要 Node.js
或独立前端进程。

## 2. 数据流

```text
macOS / sing-box / PF / 网络探针
              │
              ▼
       Controller checker（10 秒）
              │
              ▼
       内存 Snapshot + events.jsonl
              │
              ▼
        GET /api/snapshot
              │
              ▼
        React（3 秒刷新）
```

连接观察单独从 sing-box 的 loopback Clash API `127.0.0.1:9090` 每秒读取活动连接。
API Secret 只存在本机配置中，不发送给浏览器。

## 3. 顶层状态

| 状态 | 页面文案 | 含义 |
| --- | --- | --- |
| `boot_locked` | 正在启动 | 等待首次检查，PF 状态尚未确认 |
| `healthy` | 网络保护正常 | PF、TUN、DNS、出口和观测器全部符合预期 |
| `degraded_locked` | 已安全断网 | 线路异常，但 PF 正在阻止本地回落 |
| `unprotected` | 尚未启用断网保护 | PF 未确认，存在回落风险 |
| `recovering` | 正在恢复 | 正在执行受控恢复命令 |
| `maintenance` | 已安全暂停 | 隧道主动停止，PF 继续阻断公网 |

两个布尔值必须分别理解：

```text
protected = PF 防回落边界是否生效
connected = 东京业务链路是否完整可用
```

典型组合：

| protected | connected | 含义 |
| --- | --- | --- |
| `true` | `true` | 正常工作 |
| `true` | `false` | 安全断网或主动暂停，没有泄漏 |
| `false` | 任意值 | 防回落边界不可信，必须处理 |

## 4. 总览

总览第一屏把状态、四项健康指标和运行概况合并在同一信息带中，显示：

- 当前状态及更新时间。
- 实际东京出口。
- TUN 接口。
- DNS 是否经隧道。
- PF 断网保护是否启用。
- 整机公网流量的统一去向。
- 当前告警、活动连接数和策略违例数。

总览只显示“整机全部公网流量 -> 东京 VPS”，不再按 App 或域名拆分类别。

## 5. 连接观察

每条活动连接展示：

| 字段 | 来源 | 含义 |
| --- | --- | --- |
| 进程路径 | sing-box Clash API | 发起连接的可执行文件，用户名会脱敏为 `*` |
| Host / IP | sniff 与连接元数据 | 目标域名或目标地址 |
| 协议 / 端口 | 连接元数据 | TCP/UDP 和目标端口 |
| 实际链路 | sing-box chains | 必须包含 `tokyo` |
| 结果 | Controller 比对 | 实际链路是否符合策略 |
| 流量 | sing-box | 当前连接上传与下载字节数 |

策略违例条件只有一个：活动连接的实际链路不包含 `tokyo`。不存在进程/域名分类、
候选队列、人工审批、人工选择出口或未知连接回退。

后台不采集消息正文、HTTP 参数、Token、Cookie 或数据包载荷。

## 6. 系统检查

系统检查使用纵向列表展示连接阶段，并把四项主动探针直接嵌入对应行：VPS TCP
443 位于“VPS 传输”，出口 IP 位于“固定出口”，DNS 与 IPv6 位于“DNS / IPv6”。
页面不使用链路卡片，也不保留第二块独立测试列表。随后展示经过脱敏的实际配置，
最后将底层组件按网络边界与防回落、TUN 与核心服务、DNS 与 IPv6、东京传输与出口
分组。三个章节均遵循相同顺序：

```text
网络入口 -> PF 防回落 -> TUN -> sing-box -> DNS/IPv6 -> 东京 VPS -> 固定出口
```

“组件启动与配置”栏目从实际 sing-box JSON 和 launchd plist 读取 PF、TUN、
sing-box、DNS、东京出站和 Controller 的启动参数及配置值，并关联当前组件检查。
解析模型不会声明 VLESS UUID、Reality 公钥、short ID、Clash API Secret 或后台
密码，因此这些字段不会进入 Snapshot 或页面。

| ID | 页面名称 | 数据来源 / 正常条件 |
| --- | --- | --- |
| `network_conflict` | 代理/VPN 冲突 | 系统代理关闭、无冲突进程、路由属于 LightNode TUN |
| `provider` | sing-box | `pgrep` 找到进程 |
| `provider_service` | sing-box launchd | launchd 服务已加载 |
| `tunnel` | TUN 接口 | `utunN` 持有 `172.19.0.1` |
| `firewall` | PF 实时阻断 | PF 启用且 Anchor 规则已加载 |
| `firewall_config` | PF 持久化配置 | `/etc/pf.conf` 和 Anchor 引用完整 |
| `firewall_binding` | PF 接口白名单 | 当前物理接口、TUN 和 VPS Endpoint 匹配 |
| `config` | sing-box 配置 | 文件存在、权限为 `0600` 且无占位符 |
| `controller_service` | Controller launchd | Controller 服务已加载 |
| `pf_bootstrap_service` | 开机 PF 引导 | 启动期 fail-closed 服务已加载 |
| `transport` | VPS TCP 443 | `149.104.78.121:443` 可连接 |
| `egress` | 公网出口 | 探针结果等于 `149.104.78.121` |
| `dns` | 隧道 DNS | 域名可解析且主 DNS 为 `172.19.0.2` |
| `ipv6` | IPv6 防泄漏 | 原生公网 IPv6 不可用或被 TUN 接管 |

页面同时把组件合并成网络边界、PF、TUN、sing-box、VPS、出口和 DNS/IPv6 七个
阶段，便于定位连接在哪一段中断。

“运行检查”会立即执行通用网络探针：

- VPS TCP 443。
- 公网出口 IP。
- DNS 解析与路径。
- IPv6 旁路。

## 7. 告警与事件

告警表示当前仍需处理的问题；组件恢复后告警会消失。事件是状态变化时间线，会
保留最近记录：

```text
组件健康状态变化
总体状态变化
自动修复成功或失败
代理/VPN 冲突
策略违例
安全暂停与启动结果
```

严重程度：

| 等级 | 含义 |
| --- | --- |
| `critical` | 必检组件失败、出口不可信或策略违例 |
| `warning` | 故障锁定、自动恢复或需关注变化 |
| `info` | 正常启动、恢复、暂停等生命周期事件 |

主动安全暂停不会把预期停止的 sing-box、TUN 和出口记录为严重故障。

## 8. 后台操作

| 操作 | 后端 API | 行为 |
| --- | --- | --- |
| 刷新状态 | `GET /api/snapshot` | 读取当前 Snapshot |
| 运行检查 | `POST /api/tests/run` | 立即执行完整检查 |
| 重启线路 | `POST /api/actions/restart` | 重启 sing-box，PF 不变 |
| 安全暂停 | `POST /api/actions/pause` | 退出 AI App、停止隧道、保留 PF |
| 启动保护 | `POST /api/actions/start` | 恢复链路并验证东京出口 |
| 保存并切换节点 | `POST /api/node` | 原生管理员确认、原子写入、切换与失败回滚 |
| 导入 VPS 配置 | `POST /api/node/import` | 校验版本、自动备份、管理员确认并切换 |
| 创建配置备份 | `POST /api/backups` | 创建 root-only 本机备份 |
| 恢复配置备份 | `POST /api/backups/{id}/restore` | 恢复前再次备份并验证出口 |

后台故意不提供以下按钮：

- 关闭 PF。
- 恢复 Wi-Fi 公网直连。
- 选择第二 VPN 或其他出口。

这些操作会改变安全边界，其中关闭 PF 只能通过本机 root CLI 显式执行。

## 9. API 与访问控制

API：

```text
GET  /healthz
GET  /api/snapshot
GET  /api/node
GET  /api/backups
POST /api/tests/run
POST /api/actions/restart
POST /api/actions/pause
POST /api/actions/start
POST /api/node
POST /api/node/import
POST /api/backups
POST /api/backups/{id}/restore
```

后台支持 Basic Authentication。关闭认证时，POST 仍执行同源检查；但任何本机
进程都可能访问 loopback，因此不应把解除 PF 等高风险能力暴露为 HTTP API。

远程查看使用 SSH 端口转发：

```bash
ssh -N -L 127.0.0.1:9184:127.0.0.1:9180 user@mac
```

## 10. 数据时效与限制

- React 默认每 3 秒刷新 Snapshot。
- 完整组件检查默认每 10 秒运行。
- 连接观察默认每 1 秒采集。
- 冲突轻量巡检默认每 2 秒运行。
- 自动恢复使用限频，避免网络抖动造成重启循环。
- 活动连接是瞬时数据，短连接可能在两次采集之间出现并消失。
- 日志超过 5 MiB 后轮转并保留 3 份；配置备份最多保留 10 份。
- 后台健康状态不能对抗已经取得 root 权限并篡改 Controller、PF 或二进制的软件。
