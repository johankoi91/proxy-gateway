# Claude Desktop 1.15962.1 启动信息采集分析

分析对象：本机官方签名的 `/Applications/Claude.app`。

```text
Bundle ID: com.anthropic.claudefordesktop
Version:   1.15962.1
Electron:  42.4.0
app.asar:  8f76dd415474db93b19b9e8c02d216269a7e1993da9cda9b140d7ccef2106c83
```

本次只做静态解包、格式化、调用链还原，以及对本机日志和现有连接的只读核对。没有修改、
注入、重签名或替换 Claude.app。

## 结论

Claude Desktop 启动后会发送非必要产品遥测。每个桌面事件都会附带一组公共机器和运行时
字段。当前本机配置中 essential 与 nonessential telemetry 均未关闭。

确定附加到桌面事件的字段包括：

| 类别 | 字段 |
| --- | --- |
| 应用 | `app_version`、`commit_hash`、`desktop_variant`、`deployment_mode` |
| 系统 | `platform`、`arch`、`os_version`、`os_release`、`os_build` |
| CPU/内存 | `cpu_model`、`total_memory`、`free_memory` |
| 进程 | 主进程/Renderer/GPU/Utility 的 RSS、CPU、Node heap |
| 运行态 | `app_session_id`、进程 uptime、窗口数量 |
| 身份 | 登录用户 ID 或 `anonymous`、可能的 organization ID |
| 推理配置 | provider、API host 的分类及不含凭据的 base URL |

这些事件进入：

```text
https://api.anthropic.com/api/event_logging/v2/batch
```

请求头包含 `x-service-name: claude_desktop`。队列最多积累 50 条，或约 60 秒后刷新。

本机日志证实 2026-09-14 15:14:05 启动，15:14:13 生成
`desktop_app_startup_perf`，15:15:06 批量刷新 32 个事件。

## 启动性能事件

主窗口完成加载后会发送 `desktop_app_startup_perf`：

```text
main_imports_loaded_ms
app_ready_ms
window_created_ms
window_did_finish_load_ms
main_view_dom_ready_ms
is_first_run
```

该事件也会附带上一节的公共机器字段。

## 每个 Anthropic 请求附带的桌面标识

发往允许的 Anthropic 域名时，主进程会增加：

```text
anthropic-client-platform: desktop_app
anthropic-client-app: com.anthropic.claudefordesktop
anthropic-client-version: 1.15962.1
anthropic-client-os-platform: darwin
anthropic-client-os-version: <macOS version>
anthropic-desktop-topbar: 1
```

这些字段表明请求来自桌面 App，而不是普通浏览器。

## 设备 ID

GrowthBook 实验曝光事件会读取 Claude Web 会话 Cookie：

```text
anthropic-device-id
ajs_anonymous_id
```

它们是 Web/分析侧的持久标识，不是 Mac 的硬件序列号。代码没有把
`IOPlatformSerialNumber` 或 `hardware UUID` 放入桌面事件公共元数据。

## Mac 型号与 `VirtualMac2,1`

包内确实存在以下能力：

```text
system_profiler SPHardwareDataType -json
  -> SPHardwareDataType[0].machine_model
  -> { manufacturer: "Apple", model: machine_model }
```

因此在虚拟 Mac 上，该函数能得到 `VirtualMac2,1`；在本机得到 `Mac14,7`。

但是该函数属于 Sentry `AdditionalContext` 集成，当前默认配置明确为：

```text
screen: true
deviceModelManufacturer: false
```

静态调用链中没有第二处以 `deviceModelManufacturer:true` 调用它。因此对这个版本可以得出：

- 代码具备读取机器型号的能力。
- 普通启动产品遥测不包含 `machine_model`。
- 默认 Sentry 错误事件包含屏幕密度/分辨率，但默认不执行 `system_profiler` 读取型号。
- 未来版本或远程配置变化仍需重新审计，不能永久推断。

Cowork 的 `machineIdentifier` 是 Claude 自己创建的 Apple Virtualization.framework Guest
标识，用于其内部 Cowork VM，不是宿主硬件 UUID。

## 屏幕、时区、语言和 IP

### 屏幕

Sentry AdditionalContext 默认读取主显示器：

```text
screen_density
screen_resolution
```

只有产生 Sentry 会话/错误/事务时才随 Sentry 上下文发送，不属于
`desktop_app_startup_perf` 的公共字段。

### 时区

代码会用本地时区执行定时任务，并把时区传给部分内部任务环境。没有证据表明主进程的
桌面启动事件公共元数据包含时区。Claude Web 页面及验证码仍可通过浏览器标准 API 获取
时区，这属于网页侧行为，不能仅凭主进程 bundle 排除。

### 语言

App 启动时读取 `getPreferredSystemLanguages()` 和 `getLocale()` 选择 UI 语言。当前日志中的
hCaptcha URL 明确带 `hl=zh-CN`，所以对服务端而言中文语言环境是可见的。

### IP

桌面事件代码没有把本机网卡地址加入启动元数据；但服务端必然能观察连接的公网出口 IP。
当前 Claude 的网络进程连接本机 `127.0.0.1:7897` 代理，真正对外 IP 由该代理决定。

`os.networkInterfaces()` 另有两个用途：

1. 每 3 秒检测接口变化，用于恢复远程 SSH Controller。
2. 用户手工生成诊断报告时统计外部 IPv4 接口数量。

未发现它在普通启动事件中上传本地 IP/MAC 的调用链。

## Sentry essential telemetry

Sentry 在加载主业务 bundle 之前初始化，目标为 Anthropic 的 Sentry ingest 项目。

确认配置：

```text
sendDefaultPii: false
initialScope.user.id: 持久匿名/用户标识
deployment_mode: 1p 或 3p
```

错误信息、异常栈、breadcrumbs 和 transaction 会先经过清洗函数。HTTP breadcrumbs 会移除
query 与 fragment。`sendDefaultPii:false` 表示 SDK 不主动附加默认 PII，但接收服务器仍能
在网络层看到来源公网 IP。

## 只在功能触发时读取的信息

| 场景 | 信息 |
| --- | --- |
| 设置页 `getSystemInfo` | App 版本、OS 版本、CPU 型号、架构、总内存、安装类型 |
| 用户手工导出诊断 | 路径、磁盘、CPU/内存、显示器、窗口、WebContents origin、接口数量 |
| 用户提交 Cowork feedback | 可选日志、可选 transcript、会话和 inference log ID |
| Cowork VM 已连接 | Guest 内存、balloon 状态、宿主可用内存 |
| 屏幕截图/Computer Use | 需要权限和明确功能调用，不是普通启动采集 |
| 远程 SSH 功能 | 本地接口 IP/MAC 用于检测网络变化，未见普通遥测上传 |

## 不应误判的字符串

ASAR 内包含大量 SDK、证书、SSH、Office、MCP 与 Claude Code 依赖。以下字符串的出现不等于
启动时采集：

- OpenTelemetry 的 `device.id` 常量。
- TLS/X.509 的 `serialNumber`。
- Cowork Guest 的 `machineIdentifier`。
- SSH 远端设备的 hostname/networkInterfaces。
- 手工诊断报告的磁盘、窗口和网络字段。

判断标准必须是：字段构造、调用点、触发条件和网络发送函数四者能连成完整调用链。

## 当前实例的实际开关

运行中的 Renderer 参数显示：

```json
{
  "disableEssentialTelemetry": false,
  "disableNonessentialTelemetry": false
}
```

这意味着 Sentry essential telemetry 和 Anthropic/Segment 非必要产品遥测均处于启用状态。

## 证据位置

原始代码位于 `app.asar/.vite/build/index.pre.js` 和 `index.js`。关键可读还原见
[`recovered-startup-telemetry.js`](./recovered-startup-telemetry.js)。

源文件无 source map，函数名来自对 Vite bundle 的控制流还原，不代表 Anthropic 原始
TypeScript 命名。
