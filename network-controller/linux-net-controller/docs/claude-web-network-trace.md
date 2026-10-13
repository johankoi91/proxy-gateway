# Claude Web Network 过程

本机通过 Chrome CDP 观察远程 Chrome 的 Network 生命周期。记录会脱敏，不保存 Cookie、Authorization、Token 或请求正文。

## 页面初始化

典型请求顺序如下：

```text
GET  /manifest.json
POST /edge-api/bootstrap/{organization_id}/app_start
GET  /api/organizations/{organization_id}
GET  /api/organizations/{organization_id}/chat_conversations/{conversation_id}
GET  /api/organizations/{organization_id}/chat_conversations_v2
GET  /api/organizations/{organization_id}/user_settings
GET  /api/organizations/{organization_id}/memory/settings
GET  /api/organizations/{organization_id}/projects
GET  /api/organizations/{organization_id}/projects_v2
GET  /api/organizations/{organization_id}/skills/list-skills
GET  /api/organizations/{organization_id}/plugins/list-plugins
GET  /api/organizations/{organization_id}/mcp/v2/bootstrap
GET  /api/organizations/{organization_id}/chat_conversations/{conversation_id}/composer_notices
GET  /api/event_logging/v2/batch
GET  /edge-api/client-health/check
```

其中，当前对话由 `conversation_id` 标识，侧栏对话列表来自 `chat_conversations_v2`，单个对话正文来自 `chat_conversations/{conversation_id}`。

## 发送消息

发送一条消息时，核心请求是：

```text
POST /api/organizations/{organization_id}/chat_conversations/{conversation_id}/completion
Content-Type: text/event-stream
```

这是一个流式响应。Claude Web 在这个响应里逐步发送生成事件，页面同时更新当前对话。完成后通常会再次请求：

```text
GET /api/organizations/{organization_id}/chat_conversations/{conversation_id}
Content-Type: application/json
```

该 JSON 的关键字段是：

```text
uuid
name
model
current_leaf_message_uuid
chat_messages[]
```

`chat_messages[]` 中包含 `uuid`、`text`、`content`、`sender`、`parent_message_uuid` 和时间字段。当前 API 优先从这里提取最新 assistant 消息，DOM 作为回退。

## 当前 Browser API 的处理

```text
React Web
  → /v1/chat/completions
  → Playwright/CDP 控制 Claude 输入框
  → Chrome 监听 completion Network 请求
  → 等待 completion 结束
  → 读取 conversation JSON
  → 提取最新 assistant 文本
  → 返回前端
```

Network 监听只记录请求方法、资源类型、状态码、开始/结束时间和数量。不会读取或保存请求正文、响应正文、Cookie、Authorization 或 Token。

## 已观察到的异常

- Claude 额度耗尽时，页面没有输入框，API 返回 `Claude composer not found`。
- completion 请求可能是长连接，Network 的 `requestfinished` 晚于页面的“已完成回复”状态，因此不能单独用连接关闭判断完成。
- 页面切换到其他对话后，必须重新读取 conversation JSON；旧页面的 DOM 不能作为当前会话结果。
- 页面初始化和发送请求之间可能触发多次 `chat_conversations` 请求，必须用当前 URL 中的 conversation UUID 过滤。

## 诊断字段

每个 API 请求应关联：

```text
request_id
conversation_id
completion_started_at
completion_finished_at
network_api_request_count
network_completed_count
response_source: network_json / dom
error_code
```

日志只保留元数据，正文仍由前端正常显示，不写入服务日志。
