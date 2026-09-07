# LightNode Mac Controller 安装与操作

## 1. 运行前提

- Apple Silicon Mac。
- macOS 15 或兼容版本。
- 东京 VPS 上的 VLESS Reality 服务正常。
- 管理员权限。
- 初次启用 PF 时保留第二条带外管理会话作为恢复路径。

正式交付物是离线 PKG，内含 `lightnode-controller`、sing-box、launchd 配置和模板。

## 2. 安装与升级

校验并安装：

```bash
shasum -a 256 -c LightNode-Mac-Controller-0.2.0-alpha.32.pkg.sha256
sudo installer -pkg LightNode-Mac-Controller-0.2.0-alpha.32.pkg -target /
```

新装时配置仍包含占位符，因此只启动 Controller，不启动 sing-box，也不启用 PF。
重复安装当前版本会保留当前节点、保护状态和本机备份，并覆盖二进制、launchd 文件、
模板和文档：

```text
/Library/Application Support/LightNodeController/config.json
/Library/Application Support/LightNodeController/sing-box.json
/Library/Application Support/LightNodeController/firewall.committed
/Library/Application Support/LightNodeController/safely-paused
/Library/Application Support/LightNodeController/backups/
```

不能在尚未确认的 PF 试运行期间升级。必须先确认，或等待自动回滚。

## 3. 首次配置

1. 将真实 VLESS UUID、Reality 公钥、short ID 和 Clash API Secret 写入：

   ```text
   /Library/Application Support/LightNodeController/sing-box.json
   /Library/Application Support/LightNodeController/config.json
   ```

2. 检查 sing-box 配置：

   ```bash
   sudo /usr/local/libexec/sing-box check \
     -c '/Library/Application Support/LightNodeController/sing-box.json'
   ```

3. 启动并验证东京链路：

   ```bash
   sudo /usr/local/libexec/lightnode-controller tunnel start
   ```

4. 启用默认 180 秒 PF 试运行：

   ```bash
   sudo /usr/local/libexec/lightnode-controller firewall enable
   ```

   可调整回滚时间：

   ```bash
   sudo /usr/local/libexec/lightnode-controller firewall enable --rollback-seconds 600
   ```

5. 在另一条管理会话中确认管理连通性、DNS、IPv6 和东京出口。

6. 提交 Kill Switch：

   ```bash
   sudo /usr/local/libexec/lightnode-controller firewall confirm
   ```

未确认时，launchd 使用随机 Token 自动恢复启用 PF 前的配置。

## 4. 日常控制命令

| 命令 | 作用 | 是否允许公网回落 |
| --- | --- | --- |
| `lightnode-controller tunnel start` | 启动 TUN、DNS、PF 并验证东京出口 | 否 |
| `lightnode-controller tunnel pause` | 退出 AI App、停止 TUN，保留 PF | 否 |
| `lightnode-controller tunnel restart` | 重启 sing-box | 否 |
| `lightnode-controller dns refresh` | 将当前网络服务 DNS 设为 `172.19.0.2` | 否 |
| `lightnode-controller dns restore` | 恢复自动 DNS，主要供卸载使用 | 取决于 PF |
| `lightnode-controller firewall refresh` | 重建并加载受控 PF 规则 | 否 |
| `lightnode-controller firewall bootstrap` | 从已提交配置加载并启用启动 PF | 否 |
| `lightnode-controller conflicts remediate` | 关闭已知代理并恢复网络边界 | 否 |
| `lightnode-controller firewall disable --yes` | 明确移除 Kill Switch | **是** |

系统管理命令均应使用完整路径和 `sudo`：

```bash
sudo /usr/local/libexec/lightnode-controller tunnel pause
```

## 5. 安全暂停

后台“安全暂停”和 CLI 使用同一流程：

```text
确认 firewall.committed
  -> 写入 safely-paused
  -> 请求 Claude / ChatGPT 正常退出并停止 ChatGPT helper
  -> 禁用并卸载 sing-box launchd
  -> 确认 sing-box 停止
  -> 保留 PF
```

暂停后：

| 项目 | 结果 |
| --- | --- |
| Wi-Fi 连接 | 保留 |
| 百度、GPT、Claude、YouTube | 无法访问 |
| 本地公网回落 | PF 阻断 |
| 局域网带外管理 | 保留 |
| `149.104.78.121:443` | 保留，供恢复隧道 |
| Controller 管理后台 | 保留 |

Claude/ChatGPT 拒绝退出时不会强杀，后台记录警告后继续暂停。

后台操作先返回 `actionId`，随后由 Controller 的独立 Context 执行。浏览器刷新、关闭
或 SSH 转发中断不会杀死操作。Action 持久化于：

```text
/Library/Application Support/LightNodeController/lifecycle-action.json
```

同一时间只允许一个启动、暂停、重启、节点切换或配置恢复操作。Controller 在 Action
运行期间重启时，会读取记录并继续执行。最近 20 个 Action 会保留在同一原子 JSON
日志中，后续自动修复动作不会覆盖已经返回给浏览器的 actionId。

## 6. 启动保护

```text
检查配置占位符
  -> sing-box check
  -> launchctl enable/bootstrap/kickstart
  -> 等待 TUN 172.19.0.1
  -> DNS refresh
  -> firewall refresh
  -> 最多 3 次验证 149.104.78.121
  -> 删除 safely-paused
```

任何一步失败都不会删除暂停标记。恢复成功后不会自动打开 Claude 或 ChatGPT。

只有暂停标记存在且 sing-box/TUN 确实停止时才显示 `maintenance`。若标记存在但隧道
已经部分启动，状态为 `inconsistent_locked`；Controller 保持 PF、告警并自动重新执行
暂停。暂停状态下 PF 自动修复不会停止。

## 7. 状态检查

本机后台：

```text
http://127.0.0.1:9180
```

远程访问：

```bash
ssh -N -L 127.0.0.1:9184:127.0.0.1:9180 user@mac
```

然后打开 `http://127.0.0.1:9184`。

API 快速检查：

```bash
curl -fsS http://127.0.0.1:9180/api/snapshot | jq '{
  version, state, protected, connected,
  expectedEgress, observedEgress, alerts
}'
```

底层检查：

```bash
scutil --dns
route -n get 8.8.8.8
scutil --proxy
scutil --nc list
curl --noproxy '*' -4 https://api.ipify.org
curl --noproxy '*' -6 https://api64.ipify.org
```

正常值：

```text
state=healthy
protected=true
connected=true
DNS=172.19.0.2
公网路由=LightNode utunN
出口=149.104.78.121
alerts=[]
```

## 8. 故障行为

| 故障 | 状态 | 公网结果 | 自动动作 |
| --- | --- | --- | --- |
| VPS 不可达 | `degraded_locked` | 阻断 | launchd/Controller 重试 |
| sing-box 崩溃 | `degraded_locked` | 阻断 | 重启 sing-box |
| DNS 偏离 | `degraded_locked` | 不回落 | 恢复 `172.19.0.2` |
| PF 异常 | `unprotected` 或告警 | 视实时 PF 而定 | 高优先级重载 |
| 系统代理启动 | `degraded_locked` | PF 保护 | 关闭代理并恢复 |
| Clash/Mihomo 启动 | `degraded_locked` | PF 保护 | 正常退出后清理残留 |
| 未知系统 VPN | `degraded_locked` | PF 保护 | 不盲目关闭，持续告警 |
| 安全暂停 | `maintenance` | 主动阻断 | 不自动恢复 |

自动修复有 30 秒限频，冲突轻量巡检每 2 秒执行，完整检查默认每 10 秒执行。

## 9. 日志与事件

```text
/Library/Logs/LightNodeController/controller.log
/Library/Logs/LightNodeController/controller.error.log
/Library/Logs/LightNodeController/sing-box.log
/Library/Logs/LightNodeController/sing-box.error.log
/Library/Logs/LightNodeController/pf-bootstrap.log
/Library/Logs/LightNodeController/events.jsonl
```

`events.jsonl` 记录状态变化、自动恢复、策略违例和暂停结果，不记录消息正文、Token、
Cookie 或数据包载荷。上述日志达到 5 MiB 时轮转，默认保留 `.1` 到 `.3`。

## 10. 解除保护与完整卸载

解除 PF 会允许 Wi-Fi 公网直连，只能由 root 显式执行：

```bash
sudo /usr/local/libexec/lightnode-controller firewall disable --yes
```

产品只提供一种完整卸载，不保留旧配置或兼容状态：

```bash
sudo /usr/local/libexec/lightnode-controller uninstall --yes
```

卸载顺序：

```text
拒绝未确认请求
  -> 拒绝正在进行的故障注入测试
  -> 回滚尚未确认的 PF 试运行
  -> 请求 Claude / ChatGPT 正常退出
  -> 停止 sing-box
  -> PF 仍生效时恢复自动 DNS 与 IPv6
  -> 删除受控 PF 规则和 Anchor
  -> 卸载全部 launchd 服务
  -> 删除二进制、plist、配置、备份、日志、模板和文档
  -> pkgutil --forget com.lightnode.mac-controller
```

完成后 macOS 恢复普通网络能力。卸载命令没有“保留配置”模式；需要迁移节点时应先
从 VPS 后台重新下载 `*.vps-config.json`，不要依赖旧 Mac 的运行目录。

## 11. 更换东京节点

后台“节点配置”默认读取当前 Endpoint、出口、flow、TLS SNI 和 uTLS 指纹。UUID、
Reality 公钥和 short ID 不回传浏览器；对应输入框留空表示保留旧值。

可以手工修改，也可以从 VPS 后台下载对应客户端的 `*.vps-config.json`，在“节点配置”
页面选择“导入 VPS 配置”。导入文件中的 UUID、Reality 公钥和 short ID 不会出现在
API 响应或备份列表中。

保存并切换时：

```text
macOS 原生管理员确认
  -> 候选 sing-box 配置校验
  -> 原子覆盖 config.json 与 sing-box.json
  -> PF 改为新 Endpoint 白名单
  -> 启动新节点并验证期望出口
  -> 成功后重启 Controller 重新加载配置
```

如果候选配置、PF 刷新、隧道启动或出口验证失败，系统恢复旧 JSON、旧 PF Endpoint
和旧节点。整个过程不创建数据库，也不把凭据写入事件日志或 API 响应。

每次节点修改前会自动创建 `before-node-update` 备份。后台还支持“立即备份”和按时间
恢复；恢复前再创建 `before-restore` 安全备份。默认最多保留 10 份。

## 12. 自动故障回归

状态机回归随 `go test ./...` 自动运行，覆盖网络切换、VPS 不可达、sing-box 崩溃和
DNS 损坏均进入 `degraded_locked`，且 `protected=true`。

真实故障注入只能在虚拟机快照和可用带外管理通道下执行：

```bash
sudo /usr/local/libexec/lightnode-controller regression run --yes
```

额外测试虚拟网络服务断开与恢复：

```bash
sudo /usr/local/libexec/lightnode-controller regression run --yes --include-network-drop
```

每个场景会先注册 90 秒独立恢复任务。测试进程中断时，恢复任务会启用网络、还原
sing-box、刷新 DNS/PF 并验证东京出口。发现残留状态时可手工执行：

```bash
sudo /usr/local/libexec/lightnode-controller regression recover
```
