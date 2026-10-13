# Claude Browser API 设计

## 目标

把现有的远程 Linux Chrome 工作区封装成一个标准 HTTPS API，让任意设备可以通过统一接口提交问题、接收回复和管理会话。

```text
任意设备
  → HTTPS
  → Claude Browser API
  → 浏览器工作区
  → Claude Web
```

浏览器工作区继续使用现有链路：

```text
Linux Chrome
  → sing-box TUN
  → VLESS Reality TCP 443
  → 东京 VPS
  → Claude
```

API 服务只负责调度和读取浏览器页面，不直接绕过 Claude Web 的登录、验证码或访问控制。

## 后端模式

### Claude Web Browser 模式

使用现有 Claude Web 登录态，通过 Playwright/CDP 控制页面：

```text
API 请求
  → 任务队列
  → 浏览器页面输入问题
  → 等待 Claude 页面生成完成
  → 读取消息 DOM
  → API 返回
```

这个模式可以复用 Claude Web 订阅，但依赖页面结构、登录 Cookie 和验证流程。Claude 页面升级、重新登录或出现验证码时，API 可能暂时不可用。项目固定使用浏览器模式，不接入官方密钥接口。

## 推荐 API

### 发送聊天请求（OpenAI-compatible）

```http
POST /v1/chat/completions
Authorization: Bearer <api-token>
Content-Type: application/json

{
  "model": "claude-browser",
  "messages": [
    {"role": "user", "content": "请总结这份文档"}
  ],
  "stream": true
}
```

返回 OpenAI Chat Completions 兼容结果。`/v1/models` 返回唯一模型 `claude-browser`。请求会在服务端串行排队，再由 Playwright/CDP 输入到 Claude Web。这里的兼容性只覆盖聊天文本和 SSE，不包含工具调用、代码执行、函数调用或审批。

由于远程 Chrome 固定停留在同一个 Claude 对话框，服务端只取请求中最后一条 `user` 消息，不再重复拼接聊天历史、系统提示或工具说明；最终提交内容限制为 8000 字符，可通过 `API_MAX_PROMPT_CHARS` 调整。Claude 网页会继续使用当前对话框自身已有的上下文。

Browser 模式通过 DOM 变化和消息节点读取回复；不能依赖 TLS 抓包读取 Claude 请求正文。

## 浏览器工作区模型

一个浏览器 Profile 同时只能处理一个生成任务：

```text
profile-A
  → 一个 Chrome 页面
  → 一个 Claude 登录态
  → 一个当前生成任务
```

多个用户必须使用多个 Profile 或多个浏览器容器：

```text
user-a → browser-a → profile-a
user-b → browser-b → profile-b
```

不能让两个 API 用户共享同一个 Chrome 页面，否则会出现：

- 对话串线。
- Cookie 和账号混用。
- 一个用户中断另一个用户的生成。
- 文件和剪贴板互相污染。

## 内部组件

建议新增独立目录：

```text
network-controller/remote-chrome/claude-browser-api/
  cmd/server/
  internal/auth/
  internal/api/
  internal/browser/
  internal/jobs/
  internal/store/
  Dockerfile
  compose.yaml
```

服务职责：

| 组件 | 职责 |
| --- | --- |
| HTTP API | 认证、参数校验、SSE 输出 |
| Session Manager | 分配 Profile 和浏览器页面 |
| Browser Worker | Playwright/CDP 输入、读取页面状态 |
| Job Queue | 串行化同一会话的消息任务 |
| Watchdog | 检测页面卡死、浏览器退出并重建 |
| Audit Log | 只记录任务 ID、状态和耗时，不记录问题正文和回复正文 |

## 浏览器控制方式

Chrome 需要只绑定本机回环的 CDP 端口：

```text
127.0.0.1:9222
```

API 容器通过受控的本地网络访问 CDP。CDP 端口不能发布到公网，也不能通过 0.0.0.0 监听。

Browser Worker 只允许执行固定操作：

1. 打开已知 Claude 页面。
2. 找到消息输入框。
3. 写入文本或粘贴附件。
4. 点击发送。
5. 观察生成状态。
6. 读取当前回复节点。

不允许让 API 用户提交任意 JavaScript、任意 CDP 命令或任意系统命令。

## 认证和权限

API 不复用 Selkies 页面 Basic Auth，也不把 Claude Cookie 当作 API Token。

建议：

- API 使用独立随机 Bearer Token。
- Token 只保存在 root-only 环境文件或 Secret 文件。
- 不在 URL 查询参数中传 Token。
- 每个 Token 绑定允许使用的 Profile。
- 支持撤销和轮换 Token。
- 默认限制每个会话一个并发生成任务。
- 对创建会话、发送消息、文件上传分别限流。

## 内容与隐私

默认日志只记录：

```text
request_id
session_id
message_id
status
queue_time
generation_time
error_code
```

默认不记录：

- 问题正文。
- Claude 回复正文。
- Cookie、OAuth Token、Authorization Header。
- 截图和剪贴板内容。

如果用户明确开启调试，正文也应设置最大保留时间，并在日志中脱敏。

## 故障状态

建议对外提供稳定的错误码：

| 错误码 | 含义 |
| --- | --- |
| `session_not_ready` | 浏览器尚未完成登录或页面初始化 |
| `browser_unavailable` | Chrome 或 CDP 不可用 |
| `claude_auth_required` | Claude 需要重新登录 |
| `challenge_required` | Claude 或验证码要求人工验证 |
| `generation_timeout` | 回复超过最大等待时间 |
| `browser_layout_changed` | 页面结构变化，自动化选择器失效 |
| `rate_limited` | API 网关或上游限流 |
| `egress_unhealthy` | 东京出口或 Controller 未通过检查 |
| `attachment_rejected` | 文件大小或类型不允许 |

如果 `egress_unhealthy`，API 应拒绝新任务并保持浏览器工作区停止提交，避免请求回落到北京公网出口。

## 文件上传

文件建议通过独立接口上传：

```http
POST /v1/files
Content-Type: multipart/form-data
Authorization: Bearer <api-token>
```

文件先保存到短期临时目录，上传到 Claude 完成后删除。默认限制：

- 单文件 30 MB。
- 总文件数 20 个。
- 只允许明确的文档和图片类型。
- 不把文件内容写入普通日志。

## 运行检查

API 后台应显示：

```text
API service: healthy
Browser worker: healthy
Chrome CDP: reachable
Claude page: ready / auth_required / challenge_required
Controller: protected
Tokyo egress: 149.104.78.121
Active sessions: N
Queued jobs: N
```

API 的绿色状态不能只代表 HTTP 端口能访问，必须同时确认浏览器、Claude 页面和东京出口。

## 当前部署形态

北京 VPS 上由同一个 Compose 项目运行两个服务：

```text
beijing-selkies-chromium
  ├─ Selkies 浏览器工作区
  ├─ Chrome CDP（仅容器回环 127.0.0.1:9222）
  └─ OpenAI-compatible Claude Browser API 对外 HTTPS 9186
```

API 使用 Controller 的自签名证书，外部地址是：

```text
https://154.8.225.152:9186
```

调用方需要使用独立 Bearer Token。Token 只保存在北京 VPS 的
`/opt/lightnode/beijing-browser/selkies/.env`（权限 0600），不会写入镜像、日志或 URL。首次使用前，必须通过 Selkies 登录 Claude Web；API 不代替登录，也不自动处理验证码。

## VS Code 插件配置

在支持自定义 OpenAI-compatible Base URL 的聊天插件中填写：

```text
Base URL: https://154.8.225.152:9186/v1
Model: claude-browser
API Key: 北京 VPS 上 .env 中的 CLAUDE_BROWSER_API_TOKEN
```

插件必须允许 Chat Completions 请求，并关闭工具调用、函数调用和代码执行选项。当前接口返回的是一次完整回复或一个完整回复的 SSE chunk，不提供 Claude 原生事件流。

API 使用 Controller 自签名证书。正式使用时应把 Controller CA 加入客户端信任链；不要在系统范围关闭 TLS 证书校验。

## 分阶段实现

### 第一阶段：单用户单会话

- 一个 API Token。
- 一个 Chrome Profile。
- 创建会话、发送消息、查询结果、SSE。
- 只支持文本，不支持附件。

### 第二阶段：多会话隔离

- Profile 池。
- 每个会话独立页面和 Cookie。
- 崩溃重建。
- 登录状态检查。

### 第三阶段：附件和多会话

- 文件上传。
- 多个浏览器 Profile 隔离。
- 按用户或租户配置 Profile。

### 第四阶段：运维和交付

- Controller 健康状态接入 API。
- 速率、并发、配额和审计。
- Docker 离线镜像。
- 轮换 API Token 和备份 Profile。

## 重要限制

浏览器模式依赖网页结构、登录态、验证码、服务条款和页面升级。不要实现验证码自动绕过、设备指纹伪造或隐藏真实使用者的逻辑。API 只复用已经登录的 Claude Web 工作区，并在东京出口异常时拒绝提交新任务。
