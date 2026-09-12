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
beijing-vps/
```

The local Mac does not run a relay, WireGuard peer, PF forwarding rule,
browser container, or Linux Controller for this architecture.
