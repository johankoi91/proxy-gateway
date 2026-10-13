# LightNode Mac Controller

> 当前候选版本：`0.2.0-alpha.32`。

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
Go Controller: 检测、自动恢复、配置生命周期、事件、通知、本机 API
React: 本机状态面板
launchd: PF 早期引导、sing-box 与 Controller 开机守护
```

## 当前能力

- 检查 sing-box 进程和 launchd、TUN、PF Anchor、物理接口到 VPS TCP 443、固定出口、
  DNS、IPv6 和配置文件权限。
- 状态区分 `healthy`、`degraded_locked` 和 `unprotected`。隧道坏但 PF 正常时
  属于故障锁定，不会误报成可用。
- PF 已确认启用后，Controller 每 30 秒限频自动修复被关闭、清空或因网卡切换而
  失效的受控 Anchor；修复不会关闭 Kill Switch。
- 当前网络服务的 DNS 偏离 `172.19.0.2` 时，Controller 会在 TUN 健康的前提下
  自动重新设置，避免切换 Wi-Fi/有线网卡后使用本地 DNS。
- 系统代理、Clash/Mihomo、已连接的系统 VPN 或公网路由不再指向 LightNode TUN
  时，Controller 将其视为必检冲突并进入故障锁定。
- 已提交 Kill Switch 后，已知冲突会自动隔离：关闭当前网络服务的系统代理，
  停止 Clash/Mihomo，然后重启 sing-box、恢复 DNS 并重载 PF。已连接的系统 VPN
  保持锁定告警，避免自动关闭承载带外管理的通道。
- 告警写入 JSONL、系统日志，并通过当前图形登录用户的 macOS 通知中心提示。
- React 面板展示完整连接阶段、每个组件的精确错误、主动告警、测试结果和
  事件时间线。
- 后台提供“安全暂停”和“启动保护”。安全暂停先请求 Claude、ChatGPT 正常退出，
  再停止东京隧道，PF Kill Switch 始终保留；启动保护会恢复 TUN、DNS、PF 并
  验证东京出口，但不会自动打开 AI App。
- 启动、暂停和重启使用持久化异步 Action；浏览器刷新、关闭或 SSH 转发中断不会
  取消系统操作。生命周期和节点切换统一互斥，Controller 重启后会继续未完成 Action。
- Controller 同时核对暂停标记与真实进程/TUN/出口。发现半启动状态时进入
  `inconsistent_locked`，保留 PF 并自动重新执行暂停，不会误报为安全暂停。
- 节点配置页直接读写现有 `config.json` 和 `sing-box.json`，不引入数据库。保存
  前使用 macOS 原生管理员确认并校验候选配置；切换失败时恢复旧节点。
- 支持导入 VPS 后台生成的 `*.vps-config.json`。导入或手工修改前自动创建 root-only
  配置备份；后台可创建和恢复备份，最多保留 10 份。
- 日志达到 5 MiB 后使用 copy-truncate 轮转，默认保留 3 份。
- 面板只监听 `127.0.0.1:9180`，不对局域网或公网开放。认证可通过
  `disableAuthentication` 控制；当前远程 Mac 按用户要求关闭认证，并保留
  同源写操作校验。

连接观察与后台能力：

- 从 sing-box loopback Clash API 采集活动连接的进程路径、域名/IP、端口、
  命中规则、出站链和流量计数。
- 整台机器全部公网流量固定到唯一的 `tokyo` 出站。
- 后台显示活动连接、实际东京出站和违例告警。
- 后台采用四个任务标签：总览、系统检查、节点配置、事件记录。连接观察已经并入
  总览；系统检查将链路阶段、底层组件和主动验证合并为一条架构顺序列表。
- 不再识别或标记 Claude/GPT 流量；所有连接只检查是否经过唯一的 `tokyo`
  VLESS 出站。
- 全局日本 sing-box 模板已通过固定版本 `1.14.0` 校验。

AppleScript 只允许用于 GUI App 正常退出、macOS 通知，以及用户明确触发的单次
管理员授权。连接观测、路由、DNS 和 Kill Switch 不依赖 AppleScript；
相关约束见架构文档的“AppleScript 边界”。PF 已在隔离虚拟机完成故障阻断、恢复和
冲突处理验收。

已提交 Kill Switch 后，`com.lightnode.pf-bootstrap` 会在系统启动阶段独立校验、加载
并启用上一份 fail-closed PF 配置，不依赖 Controller、sing-box、DNS 或用户登录。

## 安全边界

PF 规则采用“主规则 quick 放行、Anchor 最终全局阻断”：允许 loopback、当前
TUN、当前管理网段、DHCP 以及物理网卡到 VPS 的 TCP 443，然后阻断其余
IPv4/IPv6 出口。TUN 放行使用 `no state`，避免 PF 与用户态 TUN 的双重 TCP
状态跟踪。切换 Wi-Fi 或 TUN 接口变化时，旧规则最多造成暂时断网，不会产生
新网卡直出的窗口。

自动隔离只会停止明确识别的 Clash/Mihomo。未知 Network Extension 和系统 VPN
不会被盲目杀进程，但其非 LightNode TUN 流量会被 PF 最终阻断，后台持续显示冲突
并通知。本工程不支持第二 VPN 或第二出站。

配置和日志不得写入控制台、Git 或截图。VLESS UUID、Reality 公钥、short ID
只保存在权限为 `0600` 的 `sing-box.json` 中。

完整文档按用途整理为：

- [`docs/architecture.md`](docs/architecture.md)：设计原则、网络架构、业务与 DNS
  数据流、PF/IPv6 和冲突处理。
- [`docs/operations.md`](docs/operations.md)：安装升级、控制命令、安全暂停、故障
  行为、检查与卸载。
- [`docs/runtime-audit-standard.md`](docs/runtime-audit-standard.md)：安装、升级、重启和
  日常运行后的标准走查项、通过条件、严重级别及已知缺口。

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
LIGHTNODE_CONTROLLER_ADMIN_PASSWORD=local-test \
  ./dist/lightnode-controller-darwin-arm64 \
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
dist/LightNode-Mac-Controller-0.2.0-alpha.32.pkg
```

安装包内只有 `lightnode-controller`、固定版本 sing-box、launchd 配置、模板和文档。
目标 Mac 安装时不需要再下载依赖。

原生安装包可双击安装，或使用：

```bash
sudo installer -pkg dist/LightNode-Mac-Controller-0.2.0-alpha.32.pkg -target /
```

默认构建为未签名开发包；设置 `PKG_SIGN_IDENTITY` 后，`make pkg` 会使用对应的
`Developer ID Installer` 证书签名。安装逻辑只管理当前 Controller、sing-box 和 PF
文件，不包含旧产品迁移或兼容分支。

## 安装顺序

PKG 新装时只启动管理服务，不会直接启用 PF；配置仍为占位值时也不会启动
sing-box：

```bash
shasum -a 256 -c dist/LightNode-Mac-Controller-0.2.0-alpha.32.pkg.sha256
sudo installer -pkg dist/LightNode-Mac-Controller-0.2.0-alpha.32.pkg -target /
```

然后：

1. 把真实 VLESS/Reality 参数填入
   `/Library/Application Support/LightNodeController/sing-box.json`。

2. 执行 `sudo /usr/local/libexec/sing-box check -c '/Library/Application Support/LightNodeController/sing-box.json'`。
3. 执行 `sudo /usr/local/libexec/lightnode-controller tunnel start`。Go 管理器会加载
   sing-box、设置隧道 DNS，并验证 IPv4 出口为 `149.104.78.121`。
4. 执行 `sudo /usr/local/libexec/lightnode-controller firewall enable`。Go 管理器动态识别物理
   接口、TUN 和管理网段，并启动默认 180 秒的自动回滚。
5. 在另一条已建立的带外管理会话中验证管理连接、DNS、IPv6 和隧道故障
   场景。
6. 验证完成后执行 `sudo /usr/local/libexec/lightnode-controller firewall confirm`。只有这
   一步会创建 `firewall.committed`，启用 Controller 自动修复及后续启动时的 PF
   早期引导。

未在 180 秒内确认时，launchd 回滚任务会恢复原 `/etc/pf.conf` 和 Anchor。
不要在只有一条远程连接、没有带外回退路径时确认规则。

明确允许本地公网回落时，使用：

```bash
sudo /usr/local/libexec/lightnode-controller firewall disable --yes
```

完整卸载必须显式确认。卸载器先停止业务 App 和 sing-box，在 PF 仍然阻断公网回落
时恢复自动 DNS 与 IPv6；随后移除受控 PF、launchd 服务、二进制、安装收据、配置、
备份和日志：

```bash
sudo /usr/local/libexec/lightnode-controller uninstall --yes
```

## API

- `GET /healthz`
- `GET /api/snapshot`
- `POST /api/tests/run`
- `POST /api/actions/restart`
- `POST /api/actions/start`
- `POST /api/actions/pause`
- `GET /api/actions/{id}`
- `GET|POST /api/node`
- `GET|POST /api/backups`
- `POST /api/backups/{id}/restore`
- `POST /api/node/import`

启用认证时，除 `/healthz` 外均需要本机管理账号认证；关闭认证时仍要求 POST
请求同源。安全暂停、启动和恢复动作都不会关闭 PF；只有显式执行
`lightnode-controller firewall disable --yes` 才会允许恢复本地公网。
