# Remote Chrome

北京 VPS 上的远程 Claude 浏览器交付单元，由三个部分组成：

- `web/`：React + Ant Design 页面和 Go Gateway，统一提供 `:5190` 入口。
- `claude-browser-api/`：通过 Playwright/CDP 驱动远程 Chrome，并提供对话、WebSocket 和文件下载接口。
- `selkies/`：Chrome/Selkies 运行时、输入法修复和 Docker Compose 部署入口。

在 `selkies/` 目录创建 `.env` 后运行：

```bash
docker compose build
docker compose up -d
docker compose ps
```

网络控制器由同级的 `../linux-net-controller` 单独维护。它负责东京出口和 DNS/防泄漏策略，不与远程浏览器业务代码混合。
