# Browser Workspace

The active implementation is hosted entirely on the Beijing VPS:

```text
Remote client
  -> Selkies HTTPS/WebRTC on 154.8.225.152
  -> Beijing Chrome container
  -> Linux Controller lightnode-tun
  -> Tokyo fixed exit 149.104.78.121
```

Source and operations documentation:

```text
remote-chrome/
```

The remote Claude browser is one delivery unit:

```text
remote-chrome/web/                 React UI + Go Gateway
remote-chrome/claude-browser-api/  Playwright/CDP Browser API
remote-chrome/selkies/             Chrome/Selkies runtime and Compose entrypoint
```

Build and run it from `remote-chrome/selkies/`:

```bash
docker compose build
docker compose up -d
```

The local Mac does not run a relay, WireGuard peer, PF forwarding rule,
browser container, or Linux Controller for this architecture.
