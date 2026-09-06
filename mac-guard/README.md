# LightNode Mac Guard

> 当前开发版本：`0.2.0-alpha.18`。全局日本策略已在专用虚拟 Mac 上部署；当前
> 本机未部署并继续保留原有 Clash 环境。

这是 `proxy-gateway` 下独立开发的 macOS 固定出口工程。目标是把开发 Mac
的公网流量固定到东京 VPS `149.104.78.121`，并在隧道故障时保持断网，而不是
回落到本地公网。

## 架构

```text
macOS App / DNS
       |
       v
sing-box TUN (172.19.0.1/30, DNS 172.19.0.2)
       |
       v
VLESS Reality over TCP 443
       |
       v
Tokyo VPS 149.104.78.121

PF 主规则: quick 放行 TUN、当前管理局域网和 VPS TCP 443
PF Anchor: 最终阻断其余 IPv4/IPv6 出口
Go Guard: 检测、自动恢复、事件记录、通知、本机 API
React: 本机状态面板
launchd: sing-box 与 Guard 开机守护
```

## 当前能力

- 检查 sing-box 进程和 launchd、TUN、PF Anchor、VPS TCP 443、固定出口、
  DNS、IPv6 和配置文件权限。
- 状态区分 `healthy`、`degraded_locked` 和 `unprotected`。隧道坏但 PF 正常时
  属于故障锁定，不会误报成可用。
- PF 已确认启用后，Guard 每 30 秒限频自动修复被关闭、清空或因网卡切换而
  失效的受控 Anchor；修复不会关闭 Kill Switch。
- 当前网络服务的 DNS 偏离 `172.19.0.2` 时，Guard 会在 TUN 健康的前提下
  自动重新设置，避免切换 Wi-Fi/有线网卡后使用本地 DNS。
- 系统代理、Clash/Mihomo、已连接的系统 VPN 或公网路由不再指向 LightNode TUN
  时，Guard 将其视为必检冲突并进入故障锁定。
- 已提交 Kill Switch 后，已知冲突会自动隔离：关闭当前网络服务的系统代理，
  停止 Clash/Mihomo，然后重启 sing-box、恢复 DNS 并重载 PF。已连接的系统 VPN
  保持锁定告警，避免自动关闭承载 SSH/屏幕共享的管理通道。
- 告警写入 JSONL、系统日志，并通过当前图形登录用户的 macOS 通知中心提示。
- React 面板展示完整连接阶段、每个组件的精确错误、主动告警、测试结果和
  事件时间线。
- 后台提供“安全暂停”和“启动保护”。安全暂停先请求 Claude、ChatGPT 正常退出，
  再停止东京隧道，PF Kill Switch 始终保留；启动保护会恢复 TUN、DNS、PF 并
  验证东京出口，但不会自动打开 AI App。
- 节点配置页直接读写现有 `config.json` 和 `sing-box.json`，不引入数据库。保存
  前使用 macOS 原生管理员确认并校验候选配置；切换失败时恢复旧节点。
- 面板只监听 `127.0.0.1:9180`，不对局域网或公网开放。认证可通过
  `disableAuthentication` 控制；当前远程 Mac 按用户要求关闭认证，并保留
  同源写操作校验。

V2 Alpha 已实现的离线开发能力：

- 从 sing-box loopback Clash API 采集活动连接的进程路径、域名/IP、端口、
  命中规则、出站链和流量计数。
- 唯一的 `global_japan` 档位，整台机器全部公网流量固定日本。
- Claude/ChatGPT 进程树和 AI 域名双重识别。
- V2 后台显示活动连接、实际东京出站和违例告警。
- 后台采用五个任务标签：总览、连接观察、系统检查、节点配置、事件记录。总览只
  显示安全状态、四个关键指标、流量去向和需要关注事项；底层组件不占据首屏。
- 不再识别或标记 Claude/GPT 流量；所有连接只检查是否经过唯一的 `tokyo`
  VLESS 出站。
- 全局日本 sing-box 模板已通过固定版本 `1.14.0` 校验。

AppleScript 只允许用于 GUI App 正常退出、macOS 通知，以及用户明确触发的单次
管理员授权。连接观测、路由、DNS 和 Kill Switch 不依赖 AppleScript；
完整约束见 V2 设计文档的“AppleScript 使用边界”。

V2 PF 已在隔离虚拟机完成故障阻断、恢复和冲突处理验收。

## 安全边界

PF 规则采用“主规则 quick 放行、Anchor 最终全局阻断”：允许 loopback、当前
TUN、当前管理网段、DHCP 以及物理网卡到 VPS 的 TCP 443，然后阻断其余
IPv4/IPv6 出口。TUN 放行使用 `no state`，避免 PF 与用户态 TUN 的双重 TCP
状态跟踪。切换 Wi-Fi 或 TUN 接口变化时，旧规则最多造成暂时断网，不会产生
新网卡直出的窗口。

自动隔离只会停止明确识别的 Clash/Mihomo 和 `scutil --nc` 报告为已连接的系统
VPN。未知 Network Extension 不会被盲目杀进程，但其非 LightNode TUN 流量会被
PF 最终阻断，后台持续显示冲突并通知。需要保留其他 VPN 时，应将它作为 V2 的
sing-box 第二出站接入，而不是同时运行独立 VPN App。

配置和日志不得写入控制台、Git 或截图。VLESS UUID、Reality 公钥、short ID
只保存在权限为 `0600` 的 `sing-box.json` 中。

完整文档按用途整理为：

- [`docs/architecture.md`](docs/architecture.md)：设计原则、网络架构、业务与 DNS
  数据流、PF/IPv6 和冲突处理。
- [`docs/operations.md`](docs/operations.md)：安装升级、控制命令、安全暂停、故障
  行为、检查与卸载。
- [`docs/dashboard.md`](docs/dashboard.md)：后台状态、组件数据来源、连接观察、告警、
  API 和操作边界。

## 开发与测试

```bash
cd mac-guard
make test
make pkg
```

构建固定使用并校验 sing-box `1.14.0` 官方 darwin/arm64 包。模板会使用一次性
测试凭据执行 `sing-box check`，不会向工程写入真实凭据。

本地启动面板：

```bash
LIGHTNODE_GUARD_ADMIN_PASSWORD=local-test \
  ./dist/lightnode-guard-darwin-arm64 \
  -config packaging/config.example.json
```

打开 `http://127.0.0.1:9180`，用户名 `admin`，密码 `local-test`。开发机没有安装
PF/sing-box 时显示 `unprotected` 是预期行为。

运行时网络逻辑已全部进入 Go。工程只保留三个外围脚本：

```text
scripts/package/build-pkg.sh       调用 Apple pkgbuild 的构建入口
scripts/package/pkg-postinstall.sh PKG 要求的薄安装钩子，只调用 Go
scripts/test/visual-check.cjs      Playwright 前端视觉检查
```

## 离线安装包

```bash
make pkg
```

输出：

```text
dist/LightNode-Mac-Guard-0.2.0-alpha.18.pkg
```

安装包内只有 `lightnode-guard`、固定版本 sing-box、launchd 配置、模板和文档。
目标 Mac 安装时不需要再下载依赖。

原生安装包可双击安装，或使用：

```bash
sudo installer -pkg dist/LightNode-Mac-Guard-0.2.0-alpha.18.pkg -target /
```

默认构建为未签名开发包；设置 `PKG_SIGN_IDENTITY` 后，`make pkg` 会使用对应的
`Developer ID Installer` 证书签名。安装包升级会保留现有 `config.json`、
`sing-box.json`、`v2-policy.json`、PF 提交状态和安全暂停状态。

## 安装顺序

PKG 新装时只启动管理服务，不会直接启用 PF；配置仍为占位值时也不会启动
sing-box：

```bash
shasum -a 256 -c dist/LightNode-Mac-Guard-0.2.0-alpha.18.pkg.sha256
sudo installer -pkg dist/LightNode-Mac-Guard-0.2.0-alpha.18.pkg -target /
```

然后：

1. 把真实 VLESS/Reality 参数填入
   `/Library/Application Support/LightNodeGuard/sing-box.json`。

2. 执行 `sudo /usr/local/libexec/sing-box check -c '/Library/Application Support/LightNodeGuard/sing-box.json'`。
3. 执行 `sudo /usr/local/libexec/lightnode-guard tunnel start`。Go 管理器会加载
   sing-box、设置隧道 DNS，并验证 IPv4 出口为 `149.104.78.121`。
4. 执行 `sudo /usr/local/libexec/lightnode-guard firewall enable`。Go 管理器动态识别物理
   接口、TUN 和管理网段，并启动默认 180 秒的自动回滚。
5. 在另一条已建立的 SSH/屏幕共享会话中验证管理连接、DNS、IPv6 和隧道故障
   场景。
6. 验证完成后执行 `sudo /usr/local/libexec/lightnode-guard firewall confirm`。只有这
   一步会创建 `firewall.committed` 并启用 Guard 自动修复。

未在 180 秒内确认时，launchd 回滚任务会恢复原 `/etc/pf.conf` 和 Anchor。
不要在只有一条远程连接、没有屏幕共享回退路径时确认规则。

明确允许本地公网回落时，使用：

```bash
sudo /usr/local/libexec/lightnode-guard firewall disable --yes
```

卸载器在 Kill Switch 已确认时会拒绝继续，避免先停 sing-box 后把机器留在不可
恢复的断网状态。

## API

- `GET /healthz`
- `GET /api/snapshot`
- `POST /api/tests/run`
- `POST /api/actions/restart`
- `POST /api/actions/start`
- `POST /api/actions/pause`
- `GET /api/v2/policy`

启用认证时，除 `/healthz` 外均需要本机管理账号认证；关闭认证时仍要求 POST
请求同源。安全暂停、启动和恢复动作都不会关闭 PF；只有显式执行
`lightnode-guard firewall disable --yes` 才会允许恢复本地公网。
