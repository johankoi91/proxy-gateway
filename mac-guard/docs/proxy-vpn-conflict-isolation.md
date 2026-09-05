# 代理与 VPN 冲突隔离设计

## 目标

当前架构要求所有公网流量只能通过 LightNode sing-box TUN 和东京 VPS
`149.104.78.121`。其他代理或 VPN 不得成为 Claude/GPT 的备用通道。

检测到冲突时，系统采用以下顺序：

```text
PF 保持 fail-closed
      -> Guard 标记故障锁定
      -> 关闭已知冲突
      -> 重启 sing-box
      -> 恢复 DNS 和 PF
      -> 验证东京出口
```

## 检测项

Guard 的完整健康检查每 10 秒运行一次，同时每 2 秒进行一次轻量冲突巡检。

冲突条件包括：

- `scutil --proxy` 显示 HTTP、HTTPS、SOCKS、PAC 或自动代理发现已启用。
- `clash-verge`、`verge-mihomo` 或 `FlClash` 进程运行。
- `scutil --nc list` 显示系统 VPN 为 `Connected`。
- 公共探针地址 `8.8.8.8` 的路由接口不是持有 `172.19.0.1` 的 LightNode TUN。

任一条件成立时，`network_conflict` 组件进入 `failed`，总体状态变为
`degraded_locked`，并生成前端告警、JSONL 事件和 macOS 通知。

## 自动关闭项

只有 PF 已确认提交后才允许自动隔离，防止安装或调试阶段错误修改网络。

自动动作包括：

1. 根据当前物理接口找到对应的 macOS 网络服务。
2. 关闭该服务的 HTTP、HTTPS、SOCKS 和 PAC 代理。
3. 请求 Clash Verge 和 FlClash 正常退出。
4. 停止精确名称为 `clash-verge`、`verge-mihomo` 和 `FlClash` 的残留进程。
5. 重启 `com.lightnode.sing-box`。
6. 恢复 TUN DNS `172.19.0.2`。
7. 根据当前 `enN`、`utunN` 和管理网段重新生成并加载 PF。
8. 重新执行冲突、DNS、IPv6 和出口检查。

动作使用 30 秒限频，避免代理客户端反复自启动时造成重启风暴。

## 不自动关闭项

Guard 不会自动停止未知系统 VPN 或随意杀死未知 Network Extension。原因是其可能
承载当前 SSH、屏幕共享或企业管理通道，无条件关闭可能使远端 Mac 失联。

对这类冲突采用：

```text
保留 PF 阻断
持续 degraded_locked
发送告警
等待管理员明确关闭；专用机器不允许配置第二 VPN
```

`lightnode-guard conflicts remediate` 检测到系统 VPN 仍在线时会返回失败，不会将状态
伪装成已恢复。

## 为什么 AI 不会在隔离前直接走其他出口

PF 主规则只放行：

- LightNode TUN。
- 管理局域网。
- DHCP。
- 当前物理接口到 `149.104.78.121:443/TCP`。

独立 Anchor 最终阻断其他 IPv4 和 IPv6 出口。因此：

- 未知 VPN 的 `utun` 不是 LightNode TUN，不在 PF 放行项中。
- Clash 如果绑定物理接口，只能连接东京 VPS 的 443，其他节点 Endpoint 被阻断。
- 系统代理指向本地 `127.0.0.1` 时，代理进程后续公网连接仍受 TUN/PF 约束。
- sing-box、DNS 或 PF 校验失败时，系统进入失败关闭，而不是使用本地 ISP。

## 管理后台

后台增加 `代理/VPN 冲突` 必检组件，并在“网络边界”阶段合并显示其结果。顶部
“异常组件”同时统计 `failed` 和 `degraded`。

正常状态：

```text
代理/VPN 冲突：正常
未检测到其他代理或 VPN
```

冲突状态会列出非敏感原因，例如：

```text
系统 HTTP/HTTPS/SOCKS/PAC 代理已启用
冲突进程 clash-verge
系统 VPN 已连接: <service name>
公网路由 utun7 未指向 utun4
```

## 验证方法

```bash
# 系统代理
scutil --proxy

# 系统 VPN
scutil --nc list

# 已知冲突进程
pgrep -afil 'clash-verge|verge-mihomo'

# 公网路由
route -n get 8.8.8.8

# 后台状态
curl http://127.0.0.1:9180/api/snapshot
```

故障演练必须验证：启动 Clash 后 2 秒巡检触发、系统代理被关闭、Clash/Mihomo
退出、后台短暂进入故障锁定后恢复，以及出口始终为 `149.104.78.121`。

## 边界

- 用户或 root 权限软件可以修改 PF、停止 Guard 或替换二进制；普通用户态守护
  进程无法对抗已取得同等或更高权限的恶意软件。
- 某些第三方 Network Extension 不一定出现在 `scutil --nc` 中，但其路由变化和
  非 LightNode TUN 流量仍受路由探针和 PF 最终阻断约束。
- 2 秒巡检不是零延迟安全钩子。真正的第一道边界是已加载的 PF，而不是轮询。
- 专用机器不支持第二 VPN；任何额外全局 VPN 都属于冲突。
