# LightNode Linux Controller 设计与运维入门

本文面向刚接触 Linux 网络、容器和后端服务的运维或开发人员，解释北京 Browser
Workspace 中 Linux Controller 的设计目标、实现机制、检查逻辑和排障方法。

本文不记录任何真实密码、VLESS UUID、Reality 公钥或 Short ID。

## 推荐学习路线

这份文档既是系统说明，也是 Linux 网络学习材料。初学者不建议从头背命令，可以按下面
的顺序建立知识框架：

1. 先读第 1～4 节，理解业务目标、TUN 和 Endpoint 递归问题。
2. 再读第 28～34 节，理解 namespace、veth、bridge、netfilter、路由表和策略路由。
3. 然后回到第 5～12 节，把这些 Linux 机制映射到 Controller 的真实配置。
4. 阅读第 40～44 节，学习 DNS、抓包和分层排障。
5. 最后用第 45～48 节的只读实验验证自己的理解。

学习 Linux 网络时，每看到一个数据包，都先回答四个问题：

```text
1. 包由哪个 network namespace 中的进程产生？
2. 它当前要经过 INPUT、OUTPUT 还是 FORWARD？
3. 哪条 policy rule 和哪张 route table 决定输出接口？
4. 在哪个 netfilter hook 上发生了 mark、NAT、accept 或 drop？
```

能回答这四个问题，就不再需要靠“多重启几次试试”来排查网络。

## 1. Controller 解决什么问题

北京服务器同时承担两个职责：

1. 运行 Selkies 和 Chrome，向远端用户传输浏览器画面。
2. 保证 Chrome 访问互联网时只能使用东京 VPS 的固定公网出口。

两个职责对应两条不同的数据流：

```text
远程画面与输入

远端浏览器
  -> HTTPS TCP 8445
  -> WebRTC UDP 59000
  -> 北京 Selkies


Chrome 访问互联网

北京 Chrome
  -> Docker bridge
  -> lightnode-tun
  -> VLESS Reality TCP 443
  -> 东京 VPS 149.104.78.121
  -> Internet
```

Selkies 的 WebRTC 视频不会绕东京。Controller 只负责第二条链路，也就是网络接管、
固定出口和防泄漏。

### 1.1 核心安全目标

正常情况：

```text
北京主机和 Docker 业务
  -> 东京 VPS
  -> Internet
```

东京线路故障：

```text
北京主机和 Docker 业务
  -> TUN 失败
  -> nftables 拒绝物理回落
  -> 断网
```

系统宁可断网，也不能自动改走北京服务器自己的公网出口。这种策略称为
`fail-closed`，即“失败时关闭”。

## 2. 系统由哪些部分组成

```text
systemd
  |- lightnode-lockdown.service
  |- lightnode-container-dns.timer
  |- lightnode-container-refresh.path
  `- docker.service

Docker
  |- lightnode-linux-controller
  |    |- Go Controller
  |    `- sing-box
  `- beijing-selkies-chromium

Linux kernel
  |- nftables
  |- policy routing
  |- conntrack
  |- lightnode-tun
  `- systemd-resolved
```

### 2.1 Go Controller

Go Controller 有两个运行位置：

- Host CLI：安装在 `/usr/local/libexec/lightnode-linux-controller/`，供 systemd 在宿主机
  上加载防火墙、修复 DNS 和执行真实检查。
- Controller 容器：管理 sing-box 子进程、自动恢复状态机和 HTTPS 管理后台。

两边使用同一份编译后的二进制，减少逻辑分叉。

### 2.2 sing-box

sing-box 是实际的数据面组件，负责：

- 创建 `lightnode-tun`。
- 接管主机和 Docker 的公网流量。
- 劫持 DNS 请求。
- 使用 VLESS Reality 连接东京 VPS。

Controller 本身不转发业务流量。Controller 管理和检查 sing-box，但真正的数据包由
Linux 内核和 sing-box 处理。

### 2.3 nftables

nftables 是 Linux 内核防火墙。它是最后一道防泄漏边界，即使 Controller 或 sing-box
停止，已经加载的规则仍留在内核中。

### 2.4 systemd

systemd 负责开机顺序、周期检查和服务守护。它保证 Kill Switch 在 Docker 启动前加载，
避免“容器先联网、防火墙后启动”的窗口。

## 3. TUN 是什么

TUN 是 Linux 内核提供的三层虚拟网络接口。应用以为自己把 IP 包交给普通网卡，内核
实际上把包送进 TUN，再由用户态程序读取和处理。

本系统中的地址：

```text
接口：lightnode-tun
本地：172.19.0.1/30
对端与 DNS：172.19.0.2
MTU：1280
```

MTU 是单个 IP 包的最大尺寸。设置为 1280 是为了给外层 TCP、TLS、VLESS 和 Reality
封装留出空间，减少路径 MTU 不一致造成的分片或黑洞。

sing-box 使用：

- `auto_route`：自动创建到 TUN 的路由。
- `auto_redirect`：通过 nftables 自动重定向 TCP/UDP。
- `strict_route`：减少未被规则覆盖的旁路。
- `dns_mode: hijack`：把 DNS 请求交给隧道 DNS。

## 4. 为什么必须排除东京 Endpoint

如果所有流量都先进入 TUN，连接 TUN 服务端本身的流量也会再次进入 TUN，形成递归：

```text
连接东京 VPS
  -> 进入 TUN
  -> TUN 又需要连接东京 VPS
  -> 再次进入 TUN
  -> 死循环
```

因此必须把以下目标排除：

```text
腾讯管理私网：10.2.0.0/22
东京 Endpoint：149.104.78.121/32
```

排除不等于允许任意直出。nftables 只允许物理网卡连接精确的
`149.104.78.121:443/TCP`。

## 5. 策略路由与 mark

Linux 普通路由表只根据目标地址决定出口。透明代理需要额外区分“应该进 TUN”和
“必须绕过 TUN”的包，因此使用 packet mark 和 policy routing。

当前关键规则：

```text
mark 0x2023 -> lookup table 2022
mark 0x2024 -> goto 9002
```

- `0x2023` 表示业务包已经由 sing-box 选中，应查询 TUN 路由表 2022。
- `0x2024` 用于 sing-box 自己的物理连接，避免再次进入 TUN。
- priority 1 的动态规则用于 sing-box 本地地址处理，正常只允许一条。

`route_rule_hygiene` 检查同时验证：

- priority 1 动态规则正好一条。
- `0x2023` 确实指向 table 2022。
- 没有容器重建后遗留的重复规则。

## 6. nftables Kill Switch

Controller 管理独立的 `inet lightnode_controller` 表。`inet` 同时处理 IPv4 和 IPv6。

### 6.1 input chain

input 表示“发给北京主机自身”的流量。

当前 input policy 为 accept，只丢弃 invalid 状态。公网边界由腾讯安全组和具体监听服务
共同承担。这样不会因为 Controller 配置错误把 SSH 完全锁死。

### 6.2 output chain

output 表示“北京主机进程主动发出的流量”，policy 为 drop。

只允许：

```text
established/related       已建立连接的返回流量
ct status dnat            已经被透明重定向的本机连接
loopback                  本机进程间通信
DHCP                      获取腾讯私网地址
10.2.0.0/22               腾讯管理私网
149.104.78.121:443/TCP    东京 VLESS Endpoint
lightnode-tun             已选中东京隧道的业务流量
```

其他物理 IPv4/IPv6 出站全部拒绝。

### 6.3 forward chain

forward 表示“穿过北京主机”的包，Docker bridge 流量会经过这里，policy 同样为 drop。

允许：

```text
established/related
TCP 8445                  Selkies HTTPS
TCP 9180                  Controller HTTPS
UDP/TCP 59000             Selkies WebRTC 和 ICE-TCP fallback
oifname lightnode-tun     Docker 业务只能通过 TUN 出网
```

### 6.4 为什么不能只检查必要字符串

旧检查只确认必要规则存在。如果有人额外加入：

```text
oifname "eth0" accept
```

必要规则仍然存在，但物理直出已经被放开，后台可能误报正常。

新版把实际 output/forward chain 规范化后与 Controller 生成的期望策略做精确比较。任何
新增、缺失或改序的规则都会使 `kill_switch=false`。

故障演练已经验证：加入危险规则后立即显示异常，10 秒 host refresh 在 6 秒内删除该
规则。

## 7. conntrack 与 DNAT

conntrack 是 Linux 对连接状态的记录。`established,related` 允许已有连接的返回包，避免
防火墙只允许请求却丢弃响应。

DNAT 会修改包的目标地址。Docker 端口发布和 sing-box 的 transparent redirect 都会使用
DNAT。`ct status dnat accept` 只接受已经被内核确认做过 DNAT 的本机连接，不等同于允许
任意物理出口。

## 8. Docker 网络边界

Selkies 使用 Docker bridge，Controller 使用 host network：

- Selkies 需要容器隔离和端口发布。
- Controller 需要直接创建宿主 TUN、读取宿主路由并管理 sing-box。

Controller 容器安全限制：

```text
privileged: false
capabilities: NET_ADMIN, NET_RAW
device: /dev/net/tun
root filesystem: read-only
no-new-privileges: true
Docker socket: 不挂载
```

不挂载 Docker socket 很重要。Docker socket 基本等价于宿主 root 权限。

Docker 自己管理 bridge、DNAT 和 conntrack 表。Controller 不清空 Docker 的表，只增加
一个独立且执行顺序更晚的 forward 边界。

## 9. DNS 设计

systemd-resolved 的物理接口配置为：

```text
DNS server: 172.19.0.2
route-only domain: ~.
default-route: yes
```

`~.` 表示所有域名都优先交给这个 DNS 路径。sing-box 在 TUN 中劫持 DNS，并通过东京
出站访问远端 DoH。

为什么不能只看 DNS 地址：

- 地址正确不代表服务正在响应。
- TUN 存在不代表 DNS 劫持正常。
- systemd-resolved 可能保留缓存，使偶发检查产生假象。

因此日常检查同时执行：

1. `resolvectl dns` 核对地址。
2. `resolvectl query --cache=no example.com` 执行不使用缓存的真实解析。

DNS 漂移时，10 秒 host refresh 会恢复 `172.19.0.2`。严格演练确认损坏 DNS 后解析失败，
没有使用其他 DNS 回退。

## 10. IPv6 防泄漏

只配置 `0.0.0.0/0` 不能覆盖 IPv6。如果主机获得公网 IPv6 和默认路由，应用可能绕过
IPv4 TUN。

日常检查同时验证：

1. 没有公网 IPv6 default route。
2. Kill Switch 精确策略仍覆盖 inet/IPv6。
3. `curl -6` 主动探针无法获得公网出口。

link-local `fe80::/64` 地址不是公网泄漏。它只在当前二层链路中有效。

## 11. 时间同步为什么属于网络检查

错误时间可能造成 TLS 证书校验失败、日志顺序错乱和认证异常。

chrony 使用：

```text
bindacqdevice lightnode-tun
```

这保证 NTP 获取也经过东京隧道。日常检查同时要求：

- `chronyc tracking` 显示 `Leap status: Normal`。
- chrony 配置仍绑定 `lightnode-tun`。

仅检查系统时间“看起来正确”不够，因为它可能是之前同步后留下的旧值。

## 12. 东京 Endpoint 与出口检查

这两个概念不能混为一项。

### 12.1 物理 Endpoint

```text
北京 eth0 -> 149.104.78.121:443
```

使用 `nc` 测试 TCP 连接并记录耗时。由于 Endpoint 在 TUN 排除列表中，这项验证的是
建立隧道所依赖的底层物理路径。

### 12.2 主机公网出口

宿主执行外部 IP 查询，必须返回 `149.104.78.121`。这验证北京主机进程经过 TUN。

### 12.3 Docker 业务出口

在 Selkies 容器内执行相同查询，也必须返回 `149.104.78.121`。这验证 Docker FORWARD
和 TUN 的组合路径。

`connected=true` 要求主机出口和 Docker 业务出口同时正确。

## 13. 12 项日常检查

后台按数据流分成三组。

### 13.1 物理边界与开机保护

| ID | 检查内容 | 通过条件 |
| --- | --- | --- |
| `physical_interface` | 物理网卡和私网地址 | `eth0` 存在且包含 `10.2.0.12` |
| `kill_switch` | 实际 nftables 与期望策略 | output/forward 完全一致 |
| `boot_services` | 开机和周期保护 | lockdown 与 DNS timer 均 enabled/active |
| `tokyo_endpoint` | 东京底层连接 | `149.104.78.121:443` TCP 成功 |

### 13.2 东京隧道与防泄漏

| ID | 检查内容 | 通过条件 |
| --- | --- | --- |
| `sing_box_process` | sing-box 进程 | 正好一个受控进程 |
| `tunnel` | TUN 接口和地址 | 接口存在且地址正确 |
| `route_rule_hygiene` | mark 和策略路由 | 单一动态规则，mark/table 正确 |
| `dns` | DNS 配置和解析 | 地址正确且无缓存查询成功 |
| `time_sync` | 时间与路径 | chrony 正常且绑定 TUN |
| `ipv6_leak` | IPv6 路由与探针 | 无公网默认路由，主动探针被阻断 |

### 13.3 端到端出口

| ID | 检查内容 | 通过条件 |
| --- | --- | --- |
| `tokyo_egress` | 北京主机出口 | 返回东京固定 IP |
| `docker_egress` | Docker 业务出口 | 容器返回东京固定 IP |

总览只有满足以下条件才显示“网络保护正常”：

```text
protected = true
connected = true
12 项检查全部 healthy
```

## 14. 日常检查没有包含什么

Controller 的实时绿灯只表达网络所有权，不表达整个产品所有状态。

以下内容属于 Selkies 或严格审计，不进入日常网络绿灯：

- WebRTC UDP/TCP 选择、FPS、码率、分辨率和画质。
- Selkies queue overflow、GOP reset 和编码 CPU。
- Controller 证书治理、登录限速和密码强度。
- 二进制哈希、日志轮转、磁盘和内存容量。
- 腾讯安全组的云端实际配置。

把这些全部混进一个“网络正常”结论，会导致职责不清和误报警。

## 15. Host refresh 如何工作

`lightnode-container-dns.timer` 每 10 秒启动一次 host refresh：

```text
删除 refresh-request 标记
  -> 检查并修复 DNS 漂移
  -> 执行 12 项网络检查
  -> 如果 Kill Switch 不精确，校验并重载持久化规则
  -> 再次检查
  -> 原子写入 host-status.json
```

状态使用临时文件、`fsync` 和 rename 原子替换。后台不会读取到只写了一半的 JSON。

一次完整检查通常耗时约 4 秒，但实际 CPU 时间很低，主要时间用于网络探针等待。

## 16. Controller 容器如何获得宿主状态

Controller 容器不挂载 Docker socket，也不直接授予任意宿主命令执行能力。

共享目录：

```text
/run/lightnode-linux-controller
```

宿主写入：

```text
host-status.json
```

容器可以创建固定名称的：

```text
refresh-request
```

systemd path unit 只把这个固定标记转换为预定义 host refresh，不读取标记中的命令或
参数。因此后台不能借此执行任意宿主命令。

## 17. 自动恢复状态机

### 17.1 sing-box 意外退出

Controller 持有 sing-box 子进程对象。子进程退出且期望状态仍是 running 时，等待约
2 秒后重新启动。

### 17.2 出口连续失败

```text
检查间隔：15 秒
连续失败阈值：3 次
恢复冷却：2 分钟
最多尝试：3 次
```

达到阈值后，Controller 重启 sing-box。达到最大次数后保持 fail-closed，不会改用北京
出口。

### 17.3 DNS 漂移

TUN 存在但 DNS 地址、`~.` 或 default-route 偏离时，host refresh 恢复配置。

### 17.4 nftables 丢失或被修改

host refresh 先做精确比较。异常时：

1. 使用 `nft --check` 验证持久化文件。
2. 验证成功后重新提交完整表。
3. 再次运行网络检查。
4. 仍失败则保持异常状态，不生成宽松规则。

严格演练结果：整表删除后 7 秒恢复；额外物理 allow 规则在 6 秒内被删除。

## 18. 管理动作

### 18.1 运行检查

创建 `refresh-request`，等待宿主生成新的状态。它不会停止网络或执行故障注入。

### 18.2 安全暂停

停止 sing-box，但保留 nftables：

```text
TUN 消失
公网断开
北京物理回落被阻断
Controller 管理后台仍可访问
```

### 18.3 启动线路

启动 sing-box，等待 TUN，随后由 host refresh 恢复 DNS并核对出口。

### 18.4 重启线路

顺序停止并启动 sing-box，不卸载 Kill Switch。

## 19. HTTPS 与认证

公网 Controller 使用 TLS 和 HTTP Basic Auth。非 loopback 监听如果缺少任意一项，会
拒绝启动：

- 管理用户名。
- 管理密码。
- TLS 证书。
- TLS 私钥。

Basic Auth 只是认证格式，安全性依赖 TLS；没有 TLS 时用户名和密码只相当于 Base64
明文。

当前自签证书能够加密连接，但不能提供公共 CA 身份保证。首次访问必须核对目标 IP，
不能习惯性信任任意证书警告。

生产建议：

- 使用随机长密码。
- 在腾讯安全组限制 TCP 9180 的来源。
- 增加认证失败限速。
- 有域名时改用公共 CA 证书。

## 20. 配置与文件权限

关键路径：

```text
/etc/lightnode-linux-controller/controller.json
/etc/lightnode-linux-controller/sing-box.json
/etc/lightnode-linux-controller/lockdown.nft
/etc/lightnode-linux-controller/controller.crt
/etc/lightnode-linux-controller/controller.key
/run/lightnode-linux-controller/host-status.json
/var/lib/lightnode-linux-controller/last-audit.json
/opt/lightnode/beijing-browser/controller/.env
/opt/lightnode/beijing-browser/controller/compose.yaml
```

包含凭据或安全策略的文件使用 root:root 和 0600。公开证书可以是 0644。管理 API、日志
和诊断输出不得返回节点秘密。

## 21. 日常运维命令

### 21.1 查看总状态

```bash
lightnode-linux-controller doctor
```

### 21.2 查看容器

```bash
docker ps
docker compose -f /opt/lightnode/beijing-browser/controller/compose.yaml ps
docker compose -f /opt/lightnode/beijing-browser/selkies/compose.yaml ps
```

### 21.3 查看日志

```bash
docker logs --tail=200 lightnode-linux-controller
docker logs --tail=200 beijing-selkies-chromium
```

不要把完整环境变量、`.env` 或 sing-box 配置粘贴到工单。

### 21.4 查看网络

```bash
ip -brief address
ip -d rule show
ip -4 route show table all
ip -6 route show table all
nft -a list table inet lightnode_controller
resolvectl status eth0
ss -lntup
```

### 21.5 查看 systemd

```bash
systemctl status lightnode-lockdown.service
systemctl status lightnode-container-dns.timer
systemctl status lightnode-container-refresh.path
```

## 22. 常见故障如何定位

### 22.1 `physical_interface` 异常

检查网卡是否改名、私网地址是否变化、DHCP 是否正常。不要直接把新接口加入宽松 allow。

### 22.2 `kill_switch` 异常

实际规则与生成策略不一致。host refresh 会尝试恢复。如果持续异常，先执行：

```bash
nft --check --file /etc/lightnode-linux-controller/lockdown.nft
journalctl -u lightnode-container-dns.service --since -10min
```

不要手工添加 `eth0 accept` 作为临时修复。

### 22.3 `tokyo_endpoint` 异常

底层北京到东京 TCP 443 不通。检查：

- 东京 VPS 是否在线。
- 149.104.78.121:443 是否监听。
- 腾讯安全组和运营商路径。
- 北京主机对 Endpoint 的精确 allow 是否存在。

此时重启 sing-box通常不能解决物理网络问题。

### 22.4 `sing_box_process` 或 `tunnel` 异常

检查 Controller 日志和 sing-box 配置：

```bash
docker logs --tail=200 lightnode-linux-controller
docker exec lightnode-linux-controller \
  /usr/local/libexec/lightnode-linux-controller/sing-box \
  check -c /etc/lightnode-linux-controller/sing-box.json
```

### 22.5 `dns` 异常

区分“配置错误”和“地址正确但无法解析”：

```bash
resolvectl dns eth0
resolvectl domain eth0
resolvectl default-route eth0
resolvectl query --cache=no example.com
```

### 22.6 `tokyo_egress` 正常但 `docker_egress` 异常

说明主机 TUN 正常，但 Docker bridge/forward 或业务容器异常。重点检查 Docker 网络、
Selkies 容器和 forward chain。

### 22.7 `protected=true, connected=false`

这是“安全断网”，不是泄漏。说明 Kill Switch 正常，但隧道、DNS、Endpoint 或出口检查
失败。

### 22.8 `protected=false`

这是最高优先级故障。后台不能再声明网络安全。先检查 host refresh 是否自动恢复，必要
时执行：

```bash
sudo lightnode-linux-controller firewall-apply
```

## 23. 严格故障演练

日常检查不能证明所有动态故障行为。交付级验收需要受控注入：

1. 停止 Controller，验证 TUN 消失后没有物理回落。
2. 设置无效 DNS，验证没有 DNS fallback 并能恢复。
3. 阻断东京 Endpoint，验证 `protected=true, connected=false` 和恢复状态机。
4. 删除 nftables 表，验证 10 秒级自动恢复。
5. 加入危险物理 allow，验证精确检查和自动删除。
6. 重启整机，验证 Kill Switch 先于 Docker。

每项测试前必须设置独立定时回滚，并在恢复 12/12 后再做下一项。故障演练不能由普通
“运行检查”按钮自动执行。

最近一次结果保存在：

```text
/var/lib/lightnode-linux-controller/last-audit.json
```

后台只展示日期、通过项、修复项和跳过项，不保存秘密或完整命令输出。

## 24. 已验证的故障行为

2026-09-12 的受控测试证明：

- Controller 停止后公网和物理旁路均失败。
- DNS 损坏后不使用本地 DNS，并能恢复到 172.19.0.2。
- Endpoint 阻断后保持 fail-closed，连续三次失败触发 sing-box 自动重启。
- nftables 表丢失后 7 秒内恢复。
- 额外物理 allow 在 6 秒内被识别并删除。
- 整机重启按用户要求未执行。

## 25. 开发与测试

后端测试：

```bash
GOTOOLCHAIN=go1.24.0 go test ./...
```

前端构建：

```bash
cd web
npm ci
npm run build
```

构建结果嵌入 Go 二进制。部署后应核对 host 和容器内 Controller 二进制 SHA-256 一致。

关键单元测试包括：

- 防火墙生成规则包含 fail-closed 不变量。
- 精确策略拒绝额外物理 allow。
- DNS 漂移识别。
- 自动恢复阈值、冷却和最大次数。
- Basic Auth 缺失、错误和成功场景。

## 26. 术语表

| 术语 | 含义 |
| --- | --- |
| Endpoint | 隧道客户端直接连接的服务端 IP 和端口 |
| Egress | 访问互联网时外部观察到的公网出口 |
| TUN | 用户态程序读写 IP 包的三层虚拟网卡 |
| VLESS | 本系统连接东京 VPS 使用的代理协议 |
| Reality | 对 VLESS TLS 握手的服务端身份与伪装机制 |
| Kill Switch | 隧道故障时阻止流量回落的防火墙策略 |
| fail-closed | 失败时拒绝访问，而不是放宽或回退 |
| nftables | Linux 内核防火墙框架 |
| conntrack | Linux 内核连接状态跟踪 |
| DNAT | 修改数据包目标地址的 NAT 操作 |
| policy routing | 根据 mark 等条件选择不同路由表 |
| mark | 附加在数据包上的内核路由标签 |
| Docker bridge | Docker 容器使用的虚拟二层网络 |
| systemd | Linux 服务、启动顺序和定时任务管理器 |
| systemd-resolved | Linux DNS 配置与缓存服务 |
| chrony | 系统时间同步服务 |
| DoH | 通过 HTTPS 发送 DNS 查询 |
| Basic Auth | HTTP 用户名密码认证格式，本身不提供加密 |
| self-signed | 没有公共 CA 背书的自签 TLS 证书 |
| health check | 用明确证据判断组件或链路是否符合期望 |
| fault injection | 主动制造故障以验证系统行为 |
| remediation | 检测到异常后执行的受控修复 |

## 27. 最重要的判断原则

1. 进程存在不等于链路可用。
2. TUN 存在不等于出口正确。
3. DNS 地址正确不等于解析正常。
4. 必要防火墙规则存在不等于没有危险的额外规则。
5. 主机出口正确不等于 Docker 业务出口正确。
6. `connected=false` 不一定是泄漏，`protected=false` 才是最高优先级风险。
7. 日常探针证明当前状态，故障注入证明状态变化时的行为。
8. 自动恢复必须有上限，不能用无限重启掩盖故障。
9. 任何修复都不能通过临时放开物理出口完成。
10. 检查结论必须与实际证据强度一致，不能让绿色文案超出代码真正验证的范围。

---

## 第二篇：Linux 网络技术专题

下面不再以产品功能为主线，而是使用当前系统作为实例，学习 Linux 网络栈本身。

## 28. 从 network namespace 理解宿主和容器

Linux 的 network namespace 为一组进程提供独立的：

- 网络接口。
- 路由表。
- policy rule。
- ARP/邻居表。
- socket 端口空间。
- netfilter 视图。

北京主机至少涉及两个网络空间：

```text
Host namespace
  eth0             10.2.0.12/22
  lightnode-tun    172.19.0.1/30
  Docker bridge    172.20.0.1/16
  Controller       network_mode=host

Selkies namespace
  eth0             172.20.0.2/16
  default gateway  172.20.0.1
```

Controller 使用 host network，因此它看到的是宿主的 `eth0`、TUN、路由和 socket。
Selkies 处于独立 namespace，只看到自己的容器网卡。

查看容器 namespace：

```bash
docker inspect -f '{{.NetworkSettings.SandboxKey}}' beijing-selkies-chromium
docker exec beijing-selkies-chromium ip address
docker exec beijing-selkies-chromium ip route
```

namespace 隔离不等于安全出口隔离。容器包最终仍要经过宿主 FORWARD，所以宿主
nftables 才能阻止 Docker 从 `eth0` 直接出网。

## 29. veth、bridge 与 Docker 包路径

Docker 为容器创建一对 virtual Ethernet：

```text
Selkies eth0
  <-> veth peer
  <-> Docker bridge
  <-> Host routing
```

veth 可以理解为一根虚拟网线的两端。一端在容器 namespace，另一端在宿主。
bridge 类似软件交换机，把多个容器的 veth 接到同一个二层网络。

查看关系：

```bash
ip -brief link
docker network inspect selkies_default
bridge link
```

Selkies 容器访问互联网时，源地址最初是 `172.20.0.2`。经过宿主路由、TUN 和代理后，
目标网站最终看到东京公网地址，而不是容器地址或北京私网地址。

## 30. 云公网 IP 为什么不出现在 `ip address`

北京系统内部只有：

```text
eth0 = 10.2.0.12
```

公网地址 `154.8.225.152` 通常由腾讯云边界做静态 NAT：

```text
Internet 154.8.225.152
  <-> Tencent Cloud NAT
  <-> VM 10.2.0.12
```

所以：

- `ip address` 看不到公网 IP 是正常的。
- 服务应监听 `10.2.0.12` 或 `0.0.0.0`，不能绑定不存在的公网地址。
- 安全组位于虚拟机外层，Guest 内的 nftables 无法读取它的真实配置。
- 抓包在 `eth0` 上看到的目标通常已经是私网地址。

## 31. Linux 收包和发包的基本路径

外部进入的数据包：

```text
NIC
 -> PREROUTING
 -> route decision
 -> INPUT       目标是宿主进程
    或
 -> FORWARD     目标是其他 namespace/容器
 -> POSTROUTING
```

宿主进程主动发包：

```text
local socket
 -> OUTPUT
 -> route decision
 -> POSTROUTING
 -> NIC 或 TUN
```

几个 hook 的含义：

| Hook | 典型用途 |
| --- | --- |
| PREROUTING | DNAT、透明代理、入站标记 |
| INPUT | 保护宿主监听端口 |
| FORWARD | 控制容器、路由器和 namespace 之间的流量 |
| OUTPUT | 控制宿主进程主动流量和本机透明代理 |
| POSTROUTING | SNAT、MASQUERADE、发出前最终处理 |

`drop` 是最终裁决；某个较早 base chain 的 `accept` 不代表后续更晚优先级的 base chain
不能继续检查。因此 Controller 使用独立 table 也能在 Docker/sing-box 规则之后实施
最终拒绝。

## 32. 三条真实数据包路径

### 32.1 远端访问 Controller

```text
Remote Mac
 -> 154.8.225.152:9180
 -> Tencent DNAT
 -> eth0 10.2.0.12:9180
 -> INPUT
 -> Go HTTPS server
```

这是宿主本地 socket，因此走 INPUT，不经过 Docker FORWARD。

### 32.2 远端访问 Selkies

```text
Remote Mac UDP 59000
 -> Tencent DNAT
 -> eth0
 -> Docker PREROUTING/DNAT
 -> FORWARD
 -> Docker bridge/veth
 -> Selkies 172.20.0.2:59000
```

Controller forward chain 只允许配置中的公开端口。WebRTC 返回包由 conntrack 识别为
established，再从北京直接返回远端用户，不经过东京。

### 32.3 Selkies Chrome 访问网站

```text
Chrome socket
 -> container eth0
 -> veth
 -> Docker bridge
 -> Host PREROUTING/FORWARD
 -> sing-box auto_redirect / mark
 -> policy route table 2022
 -> lightnode-tun
 -> sing-box VLESS TCP 443
 -> eth0
 -> Tokyo VPS
```

这是 `docker_egress` 要验证的完整路径。

## 33. 路由表到底做什么

路由表回答的是：给定目标 IP，下一跳和输出接口是什么。

常用查询：

```bash
ip route get 8.8.8.8
ip route get 149.104.78.121
ip route show table main
ip route show table 2022
```

`main` 是普通主路由表。table 2022 是 sing-box 创建的 TUN 路由表。

当前 table 2022 使用大量 CIDR 分段覆盖公网，而不是简单显示一条 default。这样可以
从覆盖集合中排除腾讯管理网段和东京 Endpoint，同时接管其余地址。

### 33.1 最长前缀匹配

Linux 优先选择掩码更长、更具体的路由：

```text
0.0.0.0/0          最宽泛
149.104.78.0/24    更具体
149.104.78.121/32  单个地址，最具体
```

这就是为什么一个 `/32` Endpoint 排除可以覆盖更宽泛的公网 TUN 路由。

## 34. policy rule 与 route table 的区别

普通 route 只在一张表中选路；policy rule 决定先查询哪张表。

查看：

```bash
ip -d rule show
```

处理顺序按 priority 从小到大。当前重要规则：

```text
priority 9001 fwmark 0x2023 lookup 2022
priority 32766 lookup main
```

有 `0x2023` 的包先查 TUN 表；普通包最终查 main。

可以只做路由计算、不真正发包：

```bash
ip route get 8.8.8.8 mark 0x2023
ip route get 149.104.78.121 mark 0x2024
```

这种命令比直接发送测试流量更安全。

## 35. 为什么 `curl --interface eth0` 不是旁路证明

`--interface eth0` 只把 socket 绑定到接口，它不能跳过 netfilter OUTPUT、透明 DNAT、
packet mark 或 policy routing。

因此以下命令成功：

```bash
curl --interface eth0 https://1.1.1.1
```

不代表数据真的从 `eth0` 直达 1.1.1.1。它仍可能被 sing-box 在 OUTPUT 接管。

正确方法是在请求同时抓物理网卡：

```bash
tcpdump -ni eth0 'host 1.1.1.1 and tcp port 443'
```

如果请求成功但物理网卡没有发往 1.1.1.1 的包，说明请求被 TUN 接管。严格走查已经用
这个方法验证过。

## 36. conntrack 如何让返回包通过

TCP/UDP 防火墙不能只看单个包，还要理解连接方向。conntrack 保存五元组：

```text
源 IP、源端口、目标 IP、目标端口、协议
```

状态常见为：

| 状态 | 含义 |
| --- | --- |
| NEW | 新连接的首批包 |
| ESTABLISHED | 已确认的双向连接 |
| RELATED | 与现有连接有关的辅助连接或错误 |
| INVALID | 无法归入有效连接 |

规则中的：

```text
ct state established,related accept
```

允许已经批准连接的响应包返回。它不会允许任意外部主机凭空创建新连接。

查看连接元数据需要安装 `conntrack` 工具。它只显示网络元数据，不显示 TLS 正文：

```bash
conntrack -L -p tcp
```

## 37. DNAT、SNAT 与 MASQUERADE

### DNAT

修改目标地址，常用于端口转发：

```text
10.2.0.12:8445 -> 172.20.0.2:8445
```

### SNAT

修改源地址，使内部地址可以从外部网络返回。

### MASQUERADE

动态形式的 SNAT，常用于出口地址可能变化的网络。Docker 通常使用它把容器源地址改成
宿主地址。

在本系统中，最终网站看到的是东京 VPS 地址，因为 Docker NAT 之后流量还进入 TUN 和
VLESS，而不是直接离开北京。

## 38. TCP 与 UDP 在本系统中的不同职责

```text
VLESS Reality: TCP 443
Controller HTTPS: TCP 9180
Selkies 页面/信令: TCP 8445
WebRTC 媒体: UDP 59000
ICE-TCP fallback: TCP 59000
```

TCP 提供有序、可靠重传。丢一个包时，后续数据可能等待它重传，称为 head-of-line
blocking。

UDP 不保证到达和顺序，实时视频可以丢弃过期帧，通常比 TCP 更适合低延迟画面。WebRTC
通过 RTP/RTCP、NACK、PLI、拥塞控制和关键帧机制在 UDP 上补充实时媒体需要的可靠性。

## 39. MTU、MSS 与分片

以太网常见 MTU 为 1500。业务包进入 VLESS/TLS/TCP 后会增加外层头部。如果内层仍使用
1500，外层包可能超过路径限制。

风险表现：

- 小请求正常，大页面卡住。
- TLS 握手正常，上传或视频请求超时。
- 某些网站能开，某些网站一直转圈。

TUN MTU 1280 是保守值。TCP 通常根据接口 MTU计算 MSS，减少分片。

查看：

```bash
ip link show eth0
ip link show lightnode-tun
```

不要只因为吞吐低就盲目把 TUN MTU 改到 1500。应先使用受控的 packet size/DF 测试确认
真实路径 MTU。

## 40. DNS 包在网络栈中的路径

应用通常不直接读取 `/etc/resolv.conf` 里的服务器，而是调用系统 resolver：

```text
Application
 -> glibc resolver
 -> systemd-resolved 127.0.0.53
 -> configured DNS 172.19.0.2
 -> TUN DNS hijack
 -> Tokyo outbound
 -> remote DoH
```

因此 `ss` 中看到 `127.0.0.53:53` 是本机 stub resolver，不是 DNS 泄漏。

排障要分三层：

```bash
resolvectl dns eth0                    # 配置层
resolvectl query --cache=no example.com # 系统解析层
tcpdump -ni eth0 port 53               # 物理泄漏层
```

## 41. 如何选择正确抓包点

同一个请求在不同接口看到的内容不同：

| 接口 | 能看到什么 |
| --- | --- |
| `veth...` | 容器原始业务包 |
| Docker bridge | 容器与宿主之间的包 |
| `lightnode-tun` | 被接管的内层 IP 包 |
| `eth0` | VLESS 外层 TCP、WebRTC 和公网管理流量 |

示例：

```bash
tcpdump -ni eth0 'host 149.104.78.121 and tcp port 443'
tcpdump -ni eth0 'udp port 59000'
tcpdump -ni lightnode-tun
```

不要在共享工单中上传完整 PCAP。即使 TLS 正文加密，PCAP 仍会泄露目标 IP、时间、包长
和通信模式。

## 42. 如何阅读 `ss` 输出

```bash
ss -lntup
```

参数：

```text
l  只显示监听 socket
n  不做 DNS 和服务名解析
t  TCP
u  UDP
p  显示进程
```

常见地址：

```text
127.0.0.1:PORT   只有本机可访问
10.2.0.12:PORT   监听腾讯私网地址，可经公网 NAT 映射
0.0.0.0:PORT     监听所有 IPv4 接口
[::]:PORT        监听所有 IPv6 接口
```

监听存在不等于公网可达，还要同时经过安全组、Guest 防火墙、DNAT 和应用认证。

## 43. nftables 调试方法

日常只读检查：

```bash
nft -a list table inet lightnode_controller
nft --check --file /etc/lightnode-linux-controller/lockdown.nft
nft list ruleset
```

`-a` 显示 handle，便于定位单条规则。`--check` 只验证文件，不提交修改。

`nft monitor trace` 可以显示数据包经过哪些规则，但必须先给目标流量设置 `nftrace`，会
修改运行规则并产生大量日志。生产环境不要把它当普通查看命令，应在限定 IP/端口、设置
自动回滚和明确维护窗口后使用。

## 44. 网络排障的分层思路

不要一看到“网站打不开”就重启所有服务。按照由底到顶的顺序：

```text
1. Link       eth0 是否 UP，地址是否正确
2. Physical   东京 Endpoint TCP 是否可达
3. Firewall   Kill Switch 是否精确
4. Process    Controller/sing-box 是否单实例运行
5. Interface  lightnode-tun 是否存在、地址是否正确
6. Routing    mark、rule、table 2022 是否正确
7. DNS        配置是否正确、真实解析是否成功
8. Egress     主机出口是否东京
9. Forward    Docker 业务出口是否东京
10. App       最后才检查浏览器或具体网站
```

这种顺序的价值是：每一层只依赖更低层。若 Endpoint 已失败，反复调整 Chrome 没有意义；
若主机出口正常但 Docker 出口失败，问题范围已经缩小到 bridge/forward/容器。

## 45. 适合初学者的安全实验

以下命令只读，不改变网络：

```bash
ip -brief address
ip route get 8.8.8.8
ip route get 8.8.8.8 mark 0x2023
ip -d rule show
nft -a list table inet lightnode_controller
resolvectl status eth0
ss -lntup
docker network inspect selkies_default
```

以下操作会改变网络，不能直接在生产环境练习：

- 删除 nftables table。
- 添加 allow/drop 规则。
- 停止 Controller 或 sing-box。
- 修改 DNS。
- 修改 route/rule/mark。
- 调整 TUN MTU。

受控实验必须满足：

1. 保留独立 SSH 会话。
2. 先设置 systemd 定时回滚。
3. 一次只改变一个变量。
4. 记录修改前状态。
5. 恢复 12/12 后才进行下一项。

## 46. 用四个判定问题分析一次真实连接

以 Selkies 容器内 Chrome 访问 `https://example.com` 为例。

### 46.1 谁创建 socket

Chrome 进程位于 Selkies network namespace。它创建 TCP socket，源地址最初是容器的
`172.20.0.2`，目标端口是 443。

### 46.2 为什么走 FORWARD

包从容器 `eth0` 经 veth 到达宿主。目标既不是容器自身，也不是宿主本地 socket，因此
在宿主 namespace 中属于转发流量，经过 PREROUTING、FORWARD 和 POSTROUTING，而不是
宿主 OUTPUT。

### 46.3 谁决定进入 TUN

sing-box 的透明接管规则为业务包设置 `0x2023` mark。Linux policy rule 看到该 mark 后
查询 table 2022，得到 `lightnode-tun` 输出接口。

### 46.4 外层连接为什么不递归

sing-box 从 TUN 读出内层 IP 包，再建立到 `149.104.78.121:443` 的外层 TCP 连接。这个
socket 使用绕行 mark，并且 Endpoint 有精确路由与 nftables 放行，所以它从 `eth0`
离开，不会再次进入 TUN。

最终形成两层连接：

```text
内层：172.20.0.2 -> example.com:443
外层：10.2.0.12 -> 149.104.78.121:443
```

东京 VPS 解封装后再访问目标网站，所以网站看到东京公网出口。北京 `eth0` 应只能看到
外层东京连接，不能看到直接发往 `example.com` 的业务连接。

## 47. 一次完整的只读追踪练习

下面的命令不会修改网络。执行时不要只看“成功或失败”，而要把输出对应到数据包路径。

### 第一步：识别接口和地址

```bash
ip -brief address
ip -brief link
```

重点找出物理接口、Docker bridge 和 `lightnode-tun`，区分接口是否 UP 与接口是否真的
承载正确流量。

### 第二步：比较普通路由与带 mark 路由

```bash
ip route get 8.8.8.8
ip route get 8.8.8.8 mark 0x2023
ip route get 149.104.78.121 mark 0x2024
ip -d rule show
```

三条查询分别回答普通包、隧道业务包和 sing-box 外层包会如何选路。

### 第三步：确认内核边界

```bash
nft -a list table inet lightnode_controller
```

不要只找 `drop`。需要同时确认默认 policy、允许项、接口名、Endpoint IP/端口，以及是否
存在额外的物理网卡 accept。

### 第四步：同时观察内层与外层

分别在两个终端运行：

```bash
tcpdump -ni lightnode-tun
tcpdump -ni eth0 'host 149.104.78.121 and tcp port 443'
```

然后在 Selkies Chrome 中打开一个新网站。预期 TUN 能看到内层目标，`eth0` 只看到东京
Endpoint 外层连接。只抓一个接口无法证明完整路径。

### 第五步：从业务结果反证出口

```bash
lightnode-linux-controller doctor
```

比较 `tokyo_egress` 与 `docker_egress`。前者证明宿主业务路径，后者证明容器的
namespace、bridge、FORWARD、TUN 和东京出口整条链路。

## 48. 学完后应能解释的技术问题

不要以“会运行命令”作为学习完成标准。应当能用自己的话解释：

1. 为什么 Controller 进程退出后 nftables 仍能阻断回落。
2. 为什么容器流量主要检查 FORWARD，而宿主进程流量主要检查 OUTPUT。
3. 为什么公网 IP 不一定出现在服务器网卡上。
4. 为什么 Endpoint 必须物理直连，但这不等于允许普通业务直连。
5. mark、policy rule 和 route table 分别负责哪一步。
6. 为什么 TUN 存在不能证明东京出口有效。
7. 为什么主机出口正确仍不能替代 Docker 出口检查。
8. 为什么 DNS 配置正确、DNS 可解析和 DNS 不泄漏是三个不同结论。
9. 为什么 `curl --interface eth0` 不能单独证明发生了物理旁路。
10. 为什么抓 `eth0` 与抓 `lightnode-tun` 得到的是同一业务的不同封装层。
11. 为什么 WebRTC UDP 59000 不走东京，而 Chrome 的公网访问必须走东京。
12. 为什么严格系统需要同时验证正常路径和故障时的 fail-closed 行为。
