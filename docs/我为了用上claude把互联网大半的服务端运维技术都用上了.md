# 我为了用上claude 把互联网大半的服务端运维技术都用上了

一开始，我只是想在一台电脑上稳定地使用 Claude。

这个目标听起来很小：打开客户端，登录，开始聊天。真正做起来却很快变成了一个完整的网络工程项目：要有固定出口、要防止 VPN 断开后回落到本地网络、要处理 DNS 和 IPv6 泄漏、要让虚拟机里的浏览器能被远程操作、要解决中文输入和剪贴板，还要知道每一层到底有没有工作。

最后得到的已经不是“装一个代理”，而是一套包含 VPS、隧道、TUN、策略路由、nftables、Docker、WebRTC、浏览器沙箱、自动恢复和运行审计的系统。

## 先定义问题：我要保护的是一条完整的数据路径

真正需要稳定的路径是：

```text
浏览器
  → 远程桌面传输
  → Linux Chrome
  → sing-box TUN
  → VLESS Reality
  → 东京 VPS
  → Claude
```

这里有两个完全不同的流量面：

1. 远程画面、鼠标、键盘和剪贴板，需要低延迟地传到用户电脑。
2. Chrome 访问 Claude 的 HTTPS 请求，需要固定从东京出口出去。

一开始把这两件事混在一起，导致排障很痛苦。后来把它们拆开：远程画面留在北京节点，浏览器业务流量再通过 TUN 走东京。

## 第一阶段：先买一台 VPS，把出口固定下来

第一步是准备东京 VPS。它承担最终公网出口，核心要求不是“能翻出去”这么简单，而是出口长期稳定、地址可验证、配置可重新下发。

在东京 VPS 上部署了：

- WireGuard UDP 443，用于移动设备和部分直连客户端。
- VLESS Reality TCP 443，用于 sing-box TUN。
- dnsmasq，为 WireGuard 客户端提供隧道内 DNS。
- Go 管理后台，用来生成 WireGuard 配置、二维码、VLESS 链接和节点 JSON。
- Peer 管理、撤销和配置下载。

这里的第一个技术点是：配置生成和运行数据必须分开。管理后台可以保存 Peer 的状态，但私钥、UUID、Reality 参数不能出现在普通日志和诊断页面里。于是配置下载接口只在认证后返回，文件权限也限制为 root-only。

## 第二阶段：WireGuard 很好，但 UDP 不一定可靠

Android 最开始走的是全局 WireGuard：

```text
Android
  → WireGuard UDP 443
  → 东京 VPS
  → Internet
```

WireGuard 的优点很明显：协议简单、内核支持好、延迟低、配置文件清晰。但实际网络里，UDP 回程可能被路由器、运营商或云网络丢弃。后来遇到过“请求发到了服务器，但服务器回包回不来”的情况。

这件事通过抓包才确认：

```text
北京 UDP 握手 → 到达东京
东京 WireGuard 回包 → 没有回到北京
```

所以不能看到 UDP 端口开放，就认为 WireGuard 可用。必须同时验证握手、回包、隧道内地址和业务访问。

当 UDP 路径不稳定时，VLESS Reality TCP 作为对照方案更容易穿过复杂网络，但也带来了 TCP-over-TCP 的问题：内层 HTTPS 丢包后，外层 TCP 还要重传，延迟会被放大。

## 第三阶段：mac-guard，把“断网保护”做成内核边界

只让流量走隧道还不够。最危险的情况是：隧道挂了，系统悄悄恢复普通默认路由，Claude 继续通过本地 Wi-Fi 访问。

所以设计了 macOS Controller，也就是后来 mac-guard 的核心思路：

```text
正常：业务 → TUN → 东京 VPS
故障：业务 → PF 拒绝 → 整机断网
```

关键原则叫 `fail-closed`：失败时关闭访问，而不是放宽策略。

它包含几层：

- sing-box 创建 TUN 接口。
- policy routing 根据 mark 选择 TUN 路由表。
- 物理网卡只允许连接东京 Endpoint 的外层隧道。
- PF 阻止普通物理网卡直出。
- DNS 固定在隧道内部。
- IPv6 没有完整隧道路由时直接阻断。
- 开机先加载防泄漏规则，再启动业务容器。
- Controller 周期检查并修复被修改的 PF 规则。

这里学到的一个重要经验是：进程存在不等于链路可用。TUN 存在也不等于出口正确。真正有意义的检查必须覆盖：物理 Endpoint、隧道、路由、DNS、出口和 Docker 业务出口。

## 第四阶段：为了远程使用虚拟 Mac，把远程桌面协议轮了一遍

接下来出现了第二个问题：浏览器在另一台机器上，怎么让人远程使用？

先后尝试过：

- macOS 屏幕共享和 VNC。
- Tailscale 私网和 DERP 中继。
- Nginx 转发。
- FRP 和反向 TCP 中转。
- RustDesk。
- Sunshine + Moonlight。

这些方案都能“连上”，但连上和好用是两件事。

VNC 的画面压缩和交互延迟较明显；FRP 把 TCP 再套一层 TCP，滑动时很容易卡；QUIC 在当前路径上反而比 TCP 更差；RustDesk 和 Sunshine/Moonlight 对虚拟 Mac 的硬件编码能力也不理想。

尤其是 UTM macOS 虚拟机，它可能暴露虚拟硬件型号、缺少嵌套虚拟化能力，VideoToolbox 也不能按原生 Mac 的方式工作。继续在“远程控制完整 macOS 虚拟机”上堆组件，开发量越来越大，收益却越来越小。

## 第五阶段：换个思路，远程的是浏览器，不是整台 Mac

最终采用了 Linux 浏览器工作区：

```text
远程用户
  → HTTPS 8445：页面和信令
  → WebRTC UDP 59000：画面和输入
  → Selkies + Chrome
  → Linux Controller
  → 东京 VPS
```

北京服务器只运行 Ubuntu/Linux 容器，不再运行完整的 macOS 虚拟机。

这一步的核心取舍是：

- 远程画面用 WebRTC，而不是把桌面协议再套一层代理。
- Chrome 放进 Docker，用户数据卷独立持久化。
- Controller 使用 host network，便于管理宿主 TUN、nftables 和真实路由。
- Selkies 使用独立 network namespace，容器出网必须经过宿主 FORWARD 和 TUN。
- 外部画面入口留在北京，Chrome 的公网访问走东京。

因此形成两条清晰的数据流：

```text
远程画面：远端 → 北京 Selkies
浏览器业务：Chrome → TUN → VLESS → 东京
```

## Linux 网络部分：真正用上的技术点

这套系统最终变成了一个很好的 Linux 网络实验场。

### network namespace、veth 和 bridge

Chrome 位于 Selkies 容器的 network namespace。容器里的 `eth0` 通过 veth 连接到 Docker bridge，再进入宿主的 FORWARD 路径。

容器最初看到的源地址是 Docker 私网地址，最终网站看到的却是东京公网出口。中间经过了 namespace、bridge、策略路由、TUN、VLESS 和远端解封装。

### INPUT、OUTPUT 和 FORWARD

- 访问 Controller 宿主端口，经过 INPUT。
- 宿主进程主动访问外网，经过 OUTPUT。
- Docker Chrome 访问外网，经过 FORWARD。

如果只检查宿主出口而不检查 Docker 出口，可能出现“后台显示正常，但浏览器实际旁路”的假象。

### policy routing 和 packet mark

普通路由表只看目标地址，透明代理还需要区分业务包和隧道自身连接，所以使用 mark：

```text
0x2023 → table 2022 → TUN
0x2024 → 绕过 TUN → 东京 Endpoint 物理连接
```

东京 Endpoint 必须排除，否则连接隧道本身会再次进入隧道，形成递归。

### nftables Kill Switch

当前边界是独立的 `inet lightnode_controller` 表：

- output 默认 drop。
- forward 默认 drop。
- 只允许管理入口、WebRTC 入口、已建立连接、TUN 和精确的东京 Endpoint。
- 额外出现一条 `eth0 accept` 也会被诊断识别并移除。

“需要的 allow 规则还在”不等于安全。必须检查有没有危险的额外 allow。

### DNS 与 IPv6

DNS 不能只看 `/etc/resolv.conf`。系统 resolver、systemd-resolved、TUN DNS、实际外层抓包要分别验证。

IPv6 也不能因为浏览器主要访问 IPv4 就忽略。当前没有完整 IPv6 隧道，所以直接阻断公网 IPv6 默认回落。

## 第六阶段：Claude 风控与链路性能

Claude 页面曾经长时间停在：

```text
Just a moment...
```

后来拆成两个问题：

1. 东京 IP 是机房出口，Cloudflare/hCaptcha 可能要求额外验证。
2. 北京到东京的 VLESS/TCP 链路存在明显抖动。

同一个挑战脚本，优化前加载可能需要 6～21 秒。两端改用 `BBR + fq` 后，降到约 0.6～2.7 秒，Claude 能进入 `New chat`。

这里没有做验证码绕过，也没有伪造设备指纹。能做的只是：

- 保持出口稳定。
- 减少 TCP 重传。
- 使用正常浏览器 sandbox。
- 让网络和浏览器环境保持一致。

Cloudflare/hCaptcha 的最终判定仍由服务端完成，机房 IP 的信誉不能靠客户端代码彻底改变。

## 第七阶段：中文输入和剪贴板终于变成独立问题

最初安装 Fcitx5 拼音后，中文可以输入，但在 Claude 的富文本输入框中会出现吞字：

```text
拼音候选更新
  → 远端退格旧候选
  → 输入新候选
```

如果 Claude `contenteditable` 已经提前提交了候选，下一次退格就会删掉前文。

最终没有继续修补逐键输入，而是把中文输入改成客户端本地编辑：

```text
本机输入法
  → Selkies 本地多行编辑框
  → 分片剪贴板传输
  → 远端一次 Ctrl+V
```

反向复制则采用另一条路径：

```text
远端 Chrome 复制
  → WebRTC 剪贴板消息
  → 页面暂存文本
  → 用户点击“复制到本机”
  → Clipboard API / execCommand
```

之所以增加“复制到本机”按钮，是因为浏览器通常禁止在异步 WebRTC 回调里直接写系统剪贴板；必须由用户点击触发写入。

## 最终结果

现在的整体架构是：

```text
远端用户
  → Selkies WebRTC
  → 北京 Linux Chrome
  → sing-box TUN
  → VLESS Reality TCP
  → 东京 VPS
  → Claude
```

外围还有：

- Docker 容器隔离。
- Chrome sandbox。
- nftables fail-closed。
- DNS 和 IPv6 防泄漏。
- BBR + fq 链路优化。
- Controller 12 项日常检查。
- 故障时自动锁定和有限恢复。
- 可导出的离线镜像。
- 可验证的运行状态和出口地址。

这一路最大的收获不是“终于打开了 Claude”，而是把一个看似简单的访问问题拆成了可验证的系统问题：

```text
先观察
→ 再分类
→ 再执行策略
→ 再验证出口
→ 最后注入故障
```

如果没有这套思路，遇到“页面打不开、中文吞字、偶尔变慢、VPN 断线”时，通常只能反复重启。建立了数据流、边界和证据链之后，每个故障都可以定位到具体的一层。
