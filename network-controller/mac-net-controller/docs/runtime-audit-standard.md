# LightNode Mac Controller 运行态走查标准

## 1. 目的

本文件是 LightNode Mac Controller 的运行态验收与巡检标准。后续新装、升级、节点
切换、虚拟机重启、网络切换和故障处理后的走查，默认按本文顺序执行。

走查要回答以下问题：

1. 当前安装的二进制和 PKG 是否为预期版本。
2. Mac 是否只有一个公网业务出口 `tokyo`。
3. sing-box、DNS 或 VPS 失败时，PF 是否继续阻止物理网络回落。
4. DNS 和 IPv6 是否可能绕开隧道。
5. 其他代理、VPN 或系统设置是否抢占网络路径。
6. 后台展示是否对应真实运行状态。
7. 自动检测、恢复、日志、备份和外围工具是否正常。

## 2. 当前设计基线

```text
macOS 全部公网流量
        -> sing-box TUN 172.19.0.1/30
        -> DNS 172.19.0.2 / hijack-dns
        -> 唯一 VLESS Reality 出站 tokyo
        -> VPS 149.104.78.121:443
        -> Internet

物理网卡
        -> 只允许管理网段、DHCP 和 VPS Endpoint
        -> PF Anchor 阻断其余 IPv4/IPv6
```

当前后台采用六个检查阶段：

```text
网络入口
  -> PF 防回落
  -> TUN
  -> sing-box
  -> DNS / IPv6
  -> VPS 传输与固定出口
```

本系统不提供国内直连、第二 VPN、DIRECT 出站、按应用分流或线路失败后的本地回退。

## 3. 走查类型

### 3.1 标准只读走查

允许在日常运行时执行，包括读取状态、运行主动探针、检查文件、端口、路由、PF、日志
和资源占用。默认执行本类型。

### 3.2 变更后验收

在下列操作后执行完整标准走查：

- 安装或升级 PKG。
- 修改 VPS、VLESS 或 Reality 配置。
- 修改 PF、DNS、TUN 或 launchd。
- UTM、macOS 或宿主机重启。
- 网络接口、路由器或接入网络变化。
- 修复剪贴板、显示或 Guest Agent。

### 3.3 故障注入走查

会主动停止 sing-box、破坏 DNS、切换 VPS 端口或关闭网络。只有同时满足以下条件时
才能执行：

- 已创建虚拟机快照。
- 存在不依赖业务隧道的带外管理连接。
- 用户明确同意当前故障注入。
- PF Kill Switch 已提交。

标准只读走查不得自动执行 `regression run`、关闭网络或停止服务。

## 4. 安全与取证规则

- 不输出 VLESS UUID、Reality 公钥/short ID、API Secret、后台密码或完整配置文件。
- 不采集 HTTP 正文、Cookie、Token、提示词或回复。
- 连接观察只读取进程路径、目标、端口、出站链和字节数。
- 不将真实凭据写入 Git、截图、日志或走查报告。
- 所有结论必须附来源：Snapshot、系统命令、文件权限、socket、日志或主动探针。
- 当前正常不代表历史一直正常；必须同时检查最近事件和日志。

## 5. 严重级别

| 等级 | 判定 |
| --- | --- |
| P0 | 已确认公网、DNS 或 IPv6 从非东京出口泄漏 |
| P1 | PF 不可信、存在直接回落路径、凭据泄漏或状态被错误报告为安全 |
| P2 | 自动恢复、探针准确性、可用性或运维能力存在明显缺口，但 PF 仍 fail-closed |
| P3 | 日志、备份、展示、性能或文档问题，不直接改变网络安全边界 |

报告必须先列问题，再列通过项。不能因为当前状态为 `healthy` 而省略历史异常和残余
风险。

## 6. 标准走查顺序

### 6.1 版本与交付物一致性

检查：

- `/api/snapshot.version`。
- `pkgutil --pkg-info com.lightnode.mac-controller`。
- 本地 PKG SHA-256 与 `.sha256` 文件。
- 远端 `lightnode-controller` 与本地构建产物 SHA-256。
- 远端 sing-box 与构建缓存中固定版本二进制 SHA-256。
- 二进制必须为 `darwin/arm64`。

正常条件：

```text
Snapshot 版本 = PKG 收据版本 = 计划部署版本
远端 Controller hash = 本地 Controller hash
远端 sing-box hash = 固定版本 sing-box hash
PKG checksum = declared checksum
```

任何版本或哈希不一致至少按 P1 报告。

### 6.2 顶层状态

执行一次后台主动检查：

```bash
curl -fsS -X POST \
  -H 'Origin: http://127.0.0.1:9180' \
  http://127.0.0.1:9180/api/tests/run >/dev/null

curl -fsS http://127.0.0.1:9180/api/snapshot
```

必须检查：

```text
state
protected
connected
expectedEgress
observedEgress
physicalInterface
tunnelInterface
lastCheckAt
components
tests
stages
alerts
connections
action
```

正常条件：

```text
state=healthy
protected=true
connected=true
expectedEgress=observedEgress=当前 VPS 公网 IP
physicalInterface=当前物理接口
tunnelInterface=持有 172.19.0.1 的 utunN
alerts=[]
```

`protected=true, connected=false` 表示安全断网，不是泄漏；必须继续定位不可用组件。
`protected=false` 必须按 P1 处理。

`maintenance` 必须同时满足暂停标记存在、PF 正常、sing-box/TUN/东京出口停止。
标记与真实运行态冲突时必须是 `inconsistent_locked`，并存在
`lifecycle_inconsistent` 告警；不能显示为 maintenance。

maintenance 下执行“运行检查”应在约 4 秒完成快速防回落验证：

```text
state=maintenance
protected=true
connected=false
observedEgress=""
公网访问=blocked
DNS=172.19.0.2
IPv6=unavailable
异常组件=0
```

暂停检查不得等待正常公网、DNS 或 IPv6 请求成功，也不得依赖
`launchctl print-disabled` 的辅助结果判断隧道是否仍活跃。

### 6.3 六阶段与组件完整性

阶段顺序必须是：

```text
network, firewall, tunnel, provider, dns, vps
```

组件必须完整且只出现一次：

```text
network_conflict
firewall
firewall_config
firewall_binding
pf_bootstrap_service
tunnel
provider
provider_service
config
controller_service
dns
ipv6
transport
egress
```

正常运行和确认完成的安全暂停状态下，14 个组件均应为 `healthy`。maintenance 中的
provider、provider_service 和 tunnel 表示“已按计划停止”，不代表进程正在运行。
四项主动测试为：

```text
vps_tcp
egress
dns
ipv6
```

### 6.4 launchd 与常驻进程

检查服务：

```bash
sudo launchctl print system/com.lightnode.pf-bootstrap
sudo launchctl print system/com.lightnode.sing-box
sudo launchctl print system/com.lightnode.controller
```

正常条件：

- `com.lightnode.controller` 为 `running`，只有一个 PID。
- healthy 状态下 `com.lightnode.sing-box` 为 `running`，只有一个 PID；maintenance
  状态下它应被 disable/bootout，且不存在 sing-box 进程。
- `com.lightnode.pf-bootstrap` 是一次性任务，可以是 `not running`，但最后退出码必须为
  `0`。
- launchd plist 均通过 `plutil -lint`。
- 不存在旧 Guard、旧 Policy 或重复 sing-box 进程。

### 6.5 PF fail-closed 边界

检查：

```bash
sudo pfctl -s info
sudo pfctl -a com.lightnode.vless-killswitch -sr
sudo sed -n \
  '/BEGIN LIGHTNODE VLESS KILL SWITCH/,/END LIGHTNODE VLESS KILL SWITCH/p' \
  /etc/pf.conf
```

正常条件：

- PF 为 `Enabled`。
- Anchor 同时包含：

  ```text
  block drop out quick inet all
  block drop out quick inet6 all
  ```

- 主规则只允许 loopback、当前 TUN、当前管理网段、DHCP 和物理网卡到当前 VPS TCP
  Endpoint。
- 主规则引用正确 Anchor 文件。
- `firewall.committed` 存在且权限为 `0600 root:wheel`。
- PF 主规则中的物理接口、TUN、管理 CIDR、VPS IP 和端口与当前环境一致。
- `pf-bootstrap.log` 在最近一次开机后出现 `PF boot rules are enabled`。

缺少 IPv4 或 IPv6 最终阻断规则按 P1 处理。

### 6.6 路由与真实 socket

检查普通公网路由和 VPS 路由：

```bash
route -n get 8.8.8.8
route -n get 149.104.78.121
```

healthy 状态下普通公网路由应指向 `utunN`。maintenance 状态下 TUN 不存在、普通公网
访问被 PF 阻断。仅看路由表不能判断 sing-box 的 Endpoint socket，必须
继续检查：

```bash
sudo lsof -nP -a -c sing-box -iTCP
sudo netstat -anv -p tcp | grep '149.104.78.121.443'
sudo pfctl -ss | grep '149.104.78.121:443'
```

正常的 VLESS 物理连接应表现为：

```text
虚拟机物理 IPv4:临时端口 -> VPS IPv4:443
进程 PID = sing-box
PF state = ESTABLISHED
```

不能只根据 `route get VPS_IP` 判定回环或泄漏；sing-box 使用受保护 socket 绕过自身
TUN 路由。

### 6.7 sing-box 配置不变量

只以脱敏方式解析以下字段：

```text
outbound 数量
outbound 类型和 tag
route.final
route.rules
TUN 地址、MTU、auto_route、strict_route、dns_mode
DNS final、strategy 和 detour
Clash API 监听地址
```

正常条件：

```text
outboundCount=1
vlessCount=1
outboundTag=tokyo
route.final=tokyo
route.rules 只有 hijack-dns 和 tun-in sniff
TUN=172.19.0.1/30
MTU=1280
auto_route=true
strict_route=true
dns_mode=hijack
DNS final=dns-tunnel
DNS strategy=ipv4_only
所有 DNS server detour=tokyo
Clash API=127.0.0.1:9090
```

必须搜索并确认运行目录没有 `REPLACE_` 占位符。不得在报告中打印 secret 字段。

### 6.8 DNS 与 IPv6

检查：

```bash
networksetup -getdnsservers Ethernet
scutil --dns
networksetup -getinfo Ethernet
ifconfig en0
curl --noproxy '*' -6 -fsS https://api64.ipify.org
```

正常条件：

- 当前网络服务 DNS 为 `172.19.0.2`。
- 默认非 Supplemental resolver 首项为 `172.19.0.2`。
- 物理网络服务显示 `IPv6: Off`。
- 物理接口没有可用公网 IPv6。
- `curl -6` 失败，或被 TUN 接管后返回 VPS 的 IPv4。
- 不能返回本地运营商或宿主网络的公网 IPv6。

发现非东京公网 IPv6 按 P0 处理。

### 6.9 固定出口与 VPS 传输

检查：

```bash
curl --noproxy '*' -4 -fsS https://api.ipify.org
nc -G 5 -z 149.104.78.121 443
```

正常条件：

```text
公网出口 = expectedEgress
VPS TCP Endpoint 可连接
VPS 传输与固定出口阶段 = healthy
```

`vps_tcp` 主动探针必须使用 Darwin `IP_BOUND_IF` 绑定 Snapshot 中的当前物理接口。
返回值应同时包含物理接口名和本地物理 IPv4，不能是 `utunN` 或 `172.19.0.1`。
第 6.6 节的 sing-box 物理 socket 检查仍需保留，用于证明业务隧道与独立探针都使用
正确 underlay。

### 6.10 代理与 VPN 冲突

检查：

```bash
scutil --proxy
scutil --nc list
pgrep -fl 'clash-verge|verge-mihomo|FlClash'
```

正常条件：

- HTTP、HTTPS、SOCKS、PAC 和自动代理均关闭。
- 没有已连接的系统 VPN。
- 没有 Clash Verge、Mihomo 或 FlClash 进程。
- 公网路由属于 LightNode TUN。

SPICE 创建但处于 `Disconnected` 的 PPP/Modem 服务不视为 VPN 冲突。

### 6.11 连接观察

检查：

- sing-box 在 `127.0.0.1:9090` 监听。
- Controller 使用 Bearer Secret 从 loopback 读取连接。
- `connections.health=healthy`。
- `violationCount=0`。
- 所有活动连接的 `observedOutbound` 精确包含 `tokyo`。
- 用户目录在进程路径中被脱敏。
- 活动连接上限为 200，后台最多展示 100 条。

maintenance 状态允许 `connections.activeConnections` 为 `null` 或空数组；前端必须
规范化为空数组并正常渲染，不得白屏。该状态不要求 Clash API `9090` 可连接。

连接观察不是数据包捕获，不应出现 HTTP 正文、Cookie、Token 或消息内容。

### 6.12 管理后台

检查：

- Controller 只在 `127.0.0.1:9180` 监听。
- 远端查看通过 SSH 转发，不开放局域网或公网管理端口。
- 页面只有总览、系统检查、节点配置和事件记录四个标签。
- 总览包含实时连接，不存在独立连接观察标签。
- 顶部不存在重复的刷新图标；总览自动刷新，系统检查页保留“刷新状态”。
- 系统检查只有六个阶段，VPS 传输和固定出口合并展示。
- 健康阶段默认收起，异常阶段自动展开。
- 系统检查不定时请求 `/api/snapshot`。
- 页面内“刷新状态”每次只读取一次 Snapshot。
- “运行检查”执行 `/api/tests/run`。
- 运行检查、安全暂停、启动保护和重启线路显示全屏任务层，执行期间阻止重复操作；
  生命周期阶段来自持久化 Action，刷新页面后可恢复。
- maintenance 下运行检查显示暂停专用步骤，并在约 4 秒内解除任务层。
- 配置详情按需展开，不重复组件健康状态。
- PF 健康项不重复显示 Anchor、文件路径和 launchd Label；DNS 不展示测试域名 CDN IP。
- 节点配置按 `endpoint -> vless -> tls -> reality` JSON 层级排列，固定字段只读，secret
  只显示配置状态。
- 事件页默认最多显示最近 40 条中文重要记录，可切换完整技术记录。
- 后台只提供桌面布局，最小宽度 `1100px`，没有移动端媒体查询。
- 页面不得回显现有 UUID、Reality secret、API Secret 或密码；节点配置可以显示字段名
  和“已配置”状态，用户主动输入的新值使用 password 控件。
- 生命周期 POST 必须返回 `202 + actionId`，Action 不继承 HTTP 请求取消。
- `GET /api/actions/{id}` 能查询持久化状态；同时只能运行一个网络变更操作。
- 关闭页面后 Action 继续执行，Controller 重启后继续未完成 Action。

桌面浏览器回归必须检查：

```text
1440x1000 无页面级横向溢出
收起态滚动无超过 200ms Long Task
静置系统检查 10.5 秒无 /api/snapshot 请求
点击刷新状态恰好产生 1 次 /api/snapshot 请求
运行检查期间任务层可见，完成后自动解除
maintenance Snapshot 含 null activeConnections 时页面不白屏
展开全部阶段后 14 个组件完整
展开配置后 6 个运行组件及启动命令完整
节点配置 JSON 层级与 lightnode.vps-config v1 一致
```

### 6.13 文件权限与安装内容

必须检查：

```text
/usr/local/libexec/lightnode-controller            0755 root:wheel
/usr/local/libexec/sing-box                        0755 root:wheel
/Library/Application Support/LightNodeController/config.json    0600 root:wheel
/Library/Application Support/LightNodeController/sing-box.json  0600 root:wheel
/Library/LaunchDaemons/com.lightnode.*.plist        0644 root:wheel
/Library/Logs/LightNodeController/*                 0600 root:wheel
```

不存在旧 `LightNodeGuard` 目录、旧 Guard 二进制、旧 Policy 文件或 Host Boundary
组件。

### 6.14 日志与历史事件

检查：

- `controller.error.log` 最近的状态变化。
- `sing-box.error.log` 最近的 `FATAL|ERROR|WARN`。
- `pf-bootstrap.log` 最近一次开机结果。
- 日志大小和 `.1` 到 `.3` 轮转文件。
- `events.jsonl` 权限和增长情况。

正常条件：

- 当前启动后没有持续 FATAL 或重复连接错误。
- 历史安装和重启造成的错误必须按时间区分，不能误报为当前故障。
- 单个日志达到 5 MiB 后轮转，最多保留 3 份。
- 日志不含凭据和请求正文。

### 6.15 资源与性能

检查：

```bash
uptime
vm_stat
sysctl vm.swapusage
df -h /
top -l 1 -n 15 -o cpu
iostat -d -w 1 -c 3
```

重点观察：

- Controller 和 sing-box 的 CPU、RSS 和线程数。
- WindowServer、Chrome Renderer 和宿主 UTM VM 进程。
- Swap、压缩内存、Pageout 和磁盘持续 I/O。
- 后台系统检查页收起前后的 Renderer 占用。

当前参考配置：4 vCPU、8 GiB 内存、`1920x1200 @ 80 PPI`。UTM 当前启用动态分辨率；
走查应核对实际配置与交付基线一致，不应继续套用旧的 `1600x900` 固定分辨率。

### 6.16 UTM 显示与剪贴板

宿主 UTM 配置必须满足：

```text
DynamicResolution=true
WidthPixels=1920
HeightPixels=1200
PixelsPerInch=80
ClipboardSharing=true
```

虚拟机内检查：

```bash
launchctl print system/com.redhat.spice.vdagentd
launchctl print gui/$(id -u)/com.redhat.spice.vdagent
lsof -U | grep spice-vdagent
```

正常条件：

- root `spice-vdagentd` 为 `running`。
- 图形用户 `spice-vdagent` 为 `running`。
- `/var/run/spice-vdagent-sock` 存在且双方已连接。
- 宿主到虚拟机、虚拟机到宿主的真实 Cmd+C/Cmd+V 均通过。

若 daemon 因 `Address already in use` 失败：先卸载用户 agent 和 root daemon，删除
确认无进程占用的 stale socket，再 bootstrap 两个 plist。仍不同步时正常重启虚拟机，
让 UTM 与 virtio 通道重新握手。没有图形用户登录时，用户 agent 不会加载，不能判定
剪贴板正常。

### 6.17 备份、恢复与凭据轮换

检查：

- 备份目录权限为 `0700 root:wheel`。
- 备份文件权限为 `0600 root:wheel`。
- 备份数量不超过配置上限。
- 后台不返回备份原始 JSON 或 secret。
- 当前至少有一份已验证节点的基线备份。
- 节点导入、手工修改和恢复前会创建安全备份。
- 配置候选经过 sing-box 校验并使用原子替换。

备份数量为 0 按 P3 报告。

### 6.18 自动恢复能力

检查 Controller 是否针对以下情况执行恢复：

| 故障 | 期望行为 |
| --- | --- |
| PF 被关闭或 Anchor 丢失 | 限频重载 PF |
| DNS 偏离 | 恢复 `172.19.0.2` |
| Clash/Mihomo 或系统代理 | 隔离已知冲突并恢复 PF/DNS |
| sing-box 进程崩溃 | launchd 重启 |
| VPS/出口连续失败但进程仍存活 | Controller 重启隧道并重新验证出口 |

maintenance 状态仍必须修复 PF。暂停标记与活跃 sing-box/TUN 冲突时，必须进入
`inconsistent_locked` 并自动重新执行暂停。

maintenance 快检还必须覆盖：公网被阻断、DNS 保持 `172.19.0.2`、IPv6 不可用，
并确认检查不会因预期网络超时而卡住或循环执行暂停任务。

最后一项当前尚未实现。走查必须读取历史事件，确认是否出现长时间
`degraded_locked` 且没有恢复动作。

## 7. 当前已知缺口

### AUDIT-001: 出口连续失败不会触发隧道重启

- 严重级别：P2。
- 安全结果：PF 保持 fail-closed，不会回落。
- 可用性结果：可能持续断网直到线路自行恢复或人工重启。
- 待实现：连续失败阈值、冷却时间、自动重启、出口复验和事件记录。

### AUDIT-003: 新安装后可能没有基线备份

- 严重级别：P3。
- 当前结果：备份功能可用，但数量可能为 0。
- 待处理：新节点首次验证成功后自动创建一份基线备份，或在交付前手工创建。

## 8. 接受的残余风险

- 未安装 Host Boundary。Guest PF 已尽量提前加载，但不能提供宿主/Hypervisor 层的
  跨启动绝对边界。
- 后台按用户要求关闭认证，但只监听 loopback；任何取得本机用户执行能力的进程均可
  访问只读 API 和低风险动作。
- 获得 root 权限的软件可以修改 PF、停止 Controller 或替换二进制，本系统不对抗
  同级 root 攻击者。
- 上游 DoH 和 VPS 服务商仍能看到必要的网络元数据。

## 9. 标准报告格式

每次走查报告按以下顺序输出：

1. Findings：按 P0、P1、P2、P3 排序，附证据和代码/配置来源。
2. Current State：版本、state、protected、connected、出口、阶段、组件、告警。
3. Network Boundary：PF、DNS、IPv6、路由、物理 VLESS socket。
4. Runtime Integrity：launchd、配置不变量、端口、哈希、权限。
5. Operations：日志、备份、资源、剪贴板和后台。
6. Skipped Tests：明确列出未执行的故障注入或其他检查。
7. Residual Risks：列出已接受但仍存在的边界。
8. Recommended Next Actions：只提出与发现直接相关的修复，不把建议写成已完成。

若所有必检项通过，也必须写明：

```text
当前未发现泄漏或运行故障。
故障注入是否执行：是/否。
仍存在的测试空白和残余风险：...
```

## 10. 本次基线结果

基线日期：2026-09-10。

```text
版本：0.2.0-alpha.32
state：healthy
protected：true
connected：true
固定出口：149.104.78.121
阶段：6 项全部 healthy
组件：14 项全部 healthy
主动探针：4 项全部 healthy
连接违例：0
告警：0
PF：Enabled，IPv4/IPv6 最终阻断规则存在
DNS：172.19.0.2
物理 IPv6：Off
唯一出站：tokyo
Clipboard：重启 Guest 后双向真实复制通过
显示：1920x1200，Dynamic Resolution 开启
安全暂停：maintenance / protected=true / connected=false
暂停快检：约 4 秒，异常组件 0，任务层自动解除
后台：健康与暂停状态均可渲染，null 集合不白屏
节点配置：与 lightnode.vps-config v1 JSON 层级一致
```

本次验证包含真实 VM 重启、正常启动、安全暂停、暂停快检和重新启动保护；未执行破坏性
VPS/DNS/PF 故障注入。`AUDIT-002` 已通过 Darwin `IP_BOUND_IF` 物理接口探针解决；
仍需持续报告 `AUDIT-001` 和 `AUDIT-003`，直到对应代码与验收测试完成。
