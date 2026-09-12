# 北京 Browser Workspace 运行态走查报告

走查时间：2026-09-12 22:29-22:44 CST

走查类型：标准只读走查加受控故障注入。已测试停止 Controller、破坏 DNS、阻断东京
Endpoint 和删除 nftables 表；按用户要求未执行整机重启。

## 1. Findings

### P1：公网 Controller 凭据强度不足且没有登录限速

Controller 已通过 HTTPS 和 Basic Auth 保护，未认证访问返回 401，但当前使用用户指定的
`admin` 账号和弱口令。HTTP API 没有失败次数限制或来源 IP 限制。公网攻击者可以持续
尝试口令；成功后可以运行检查以及启动、暂停或重启东京线路，造成业务中断。

建议优先改为随机长密码，并在腾讯安全组将 TCP 9180 限制到可信公网来源。报告不记录
实际密码。

### P2：Controller 使用自签 TLS 证书

TLS 1.2 可正常协商，证书 SAN 包含公网和私网 IPv4，但证书没有公共 CA 信任链。首次
访问需要人工跳过证书警告；若用户误信任被替换的证书，Basic Auth 凭据可能被中间人
获取。正式长期使用建议绑定域名并使用可信证书。

### P3：东京 Endpoint 时延存在波动

五次 TCP 建连耗时为 219、104、224、245、221 ms。全部成功，但波动会影响首次连接和
突发请求延迟。当前未发现持续失败。

### 已修复 P1：nftables 表丢失后原实现只告警、不恢复

首次删除 `inet lightnode_controller` 后，Controller 正确报告 `protected=false`，但未
主动恢复，规则由预设安全回滚在约 19 秒后恢复。若该问题与 TUN 故障同时发生，会产生
北京直出窗口。

本轮已修改 host refresh：发现防火墙缺失或不完整时，先校验持久化规则并重新加载，
再发布状态。部署后重复相同测试，规则在 7 秒内由 10 秒 timer 自动恢复，未触发预设
60 秒安全回滚。修复后重新达到 9/9 healthy。

## 2. Current State

```text
Host: Ubuntu 24.04.4 LTS AMD64
Physical interface: eth0 / 10.2.0.12/22
Controller: lightnode/linux-controller:0.1.0-alpha.3-amd64, healthy
Selkies: lightnode/selkies-chromium:2.0.0rc0-amd64, healthy
protected: true
connected: true
Controller checks: 9/9 healthy
Host egress: 149.104.78.121
Selkies container egress: 149.104.78.121
TUN: lightnode-tun / 172.19.0.1/30
DNS: 172.19.0.2
Public IPv6 default route: none
```

Selkies 当前会话通过公网 UDP 59000 完成 ICE，实际流为 2560x1440、45 FPS、x264
CBR。客户端 DPI 为 192。最近 35 分钟没有 video queue overflow 或 GOP reset。

## 3. Network Boundary

- `inet lightnode_controller` 的 output 和 forward 均为 `filter + 10`、policy drop。
- 物理 output 只放行 DHCP、腾讯私网、东京 `149.104.78.121:443` 和 TUN。
- public forward 只包含 TCP 8445、TCP 9180、UDP/TCP 59000；不存在旧 TCP 8443。
- host input 仍为 accept；当前公网监听为 SSH、Controller 9180 和 Docker 发布端口。
- sing-box 的随机 TCP redirect 端口由其自有 nftables input 规则拒绝非 DNAT 连接；公网
  实测为 connection refused，不是开放代理。
- 使用 `curl --interface eth0` 请求非 Endpoint 地址时，请求仍被透明 TUN 接管；同步
  `eth0` 抓包未发现发往目标 `1.1.1.1:443` 的物理数据包。
- `eth0`、TUN 均无 RX/TX drop；UDP receive/send buffer error 为 0。
- 运行期 nftables 边界由 10 秒 host refresh 持续验证，缺失时自动校验并重载。

当前未发现公网、DNS或 IPv6 泄漏。

## 4. Runtime Integrity

- Controller host 与容器内二进制 SHA-256 一致：
  `693546cada0fd7c1fd5de4310cf33afc0ee9de8fa71471e3f33c940543afaea9`。
- 容器内 sing-box SHA-256：
  `57b3da14e264b6e05e8f46aee027c02d7dd7f1594d19aa39e2f4d2b9459bbd04`。
- Controller 非 privileged，仅增加 `NET_ADMIN`、`NET_RAW`，root filesystem 只读，
  未挂载 Docker socket，并启用 `no-new-privileges`。
- Controller 只有一个主进程和一个 sing-box 子进程。
- Controller、sing-box、nftables 配置和两份 `.env` 均为 root 所有、模式 0600。
- Controller TLS 私钥为 0600；公开证书为 0644。
- 两个容器都使用 `unless-stopped` 和 `10m x 3` JSON 日志轮转。
- `lightnode-lockdown.service`、DNS timer、refresh path、Docker、resolved 和 chrony 均
  enabled/active。
- chrony 通过 `bindacqdevice lightnode-tun` 获取时间，Leap status 为 Normal。

## 5. Public Services

```text
https://154.8.225.152:9180/  Controller: unauthenticated 401, authenticated 200
https://154.8.225.152:8445/  Selkies:    unauthenticated 401, authenticated 200
https://154.8.225.152:8443/  unreachable
```

Controller 返回 CSP、X-Content-Type-Options、X-Frame-Options 和 Referrer-Policy 安全
响应头。证书有效期为 2026-09-12 至 2028-12-15。

## 6. Resources

```text
Memory: 7.5 GiB total, 5.4 GiB available
Swap: 1.9 GiB total, less than 1 MiB used
Disk: 178 GiB total, 19 GiB used, 151 GiB available
Controller RSS: about 24 MiB container total
Selkies memory: about 1.4 GiB during active session
Container logs: below 1 MiB each at audit time
```

当前没有内存、Swap、磁盘或日志增长风险。Selkies 为 CPU x264 编码，45 FPS 快速滚动
时的历史压力测试峰值约 337% CPU，仍低于 4 vCPU 总容量，但剩余余量有限。

## 7. Fault Injection Results

### Controller/sing-box 停止

- 停止后 TUN 消失，nftables 保持加载。
- 普通公网和显式绑定 `eth0` 的非 Endpoint 请求均失败。
- 未发生北京物理回落；Controller 启动后恢复 9/9。

### DNS 损坏

- 停止 DNS timer 并把 resolver 改为无效地址后，解析失败且没有使用其他 DNS。
- 启动 host refresh 后恢复为 `172.19.0.2`，解析和 9/9 恢复。

### 东京 Endpoint 阻断

- 阻断后 `protected=true`、`connected=false`，公网和物理旁路均失败。
- 连续三次出口失败后记录自动恢复 attempt 1/3，并重启 sing-box。
- 移除人为阻断规则后恢复固定出口和 9/9。

### nftables 表删除

- 原实现只检测、不恢复；该 P1 缺口已在本轮修复。
- 修复版在 7 秒内自动恢复表、默认丢弃策略和精确放行规则。
- 恢复后 `protected=true`、`connected=true`，出口仍为东京。

## 8. Skipped Tests

- 按用户要求未重启整台北京 VPS，因此本轮没有重新验证完整 boot ordering。
- 无法从 Guest 内核读取腾讯安全组的实际规则；公网端口行为通过外部探针验证。

## 9. Residual Risks

- 公网 Controller 的弱口令和无登录限速是本轮最高风险。
- 自签证书不能提供公共 CA 身份保证。
- 如果腾讯安全组保持全端口放行，未来新增 host listener 可能自动暴露；应改为明确端口
  白名单。
- 获得服务器 root 权限的进程可以停止 Controller 或修改 nftables，不在防护模型内。
- 整机重启未执行；开机 Kill Switch 顺序仍依据 unit 依赖和既有运行记录，而不是本轮
  重启证据。
