# Host Screen Relay

This host-only Nginx stream relay exposes the UTM virtual Mac's screen-sharing
service to authorized Tailscale peers.

```text
Tailscale peer -> 100.90.42.40:15900 -> 10.211.55.4:5900
```

The listener binds only to the host's Tailscale IPv4 address and accepts only
sources in `100.64.0.0/10`. It does not inspect or store VNC payloads. Tailscale
ACLs must provide the user-specific access boundary.

Runtime files:

```text
~/Library/Application Support/LightNodeHostRelay/nginx.conf
~/Library/LaunchAgents/com.lightnode.vm-screen-relay.plist
~/Library/Logs/LightNodeHostRelay/
```

Remote Macs connect with:

```text
vnc://100.90.42.40:15900
```

## Runtime

Nginx `1.31.5` is installed with Homebrew, but its default HTTP service is not
started. A dedicated user LaunchAgent runs only the stream configuration:

```text
com.lightnode.vm-screen-relay
```

Check or restart it with:

```bash
launchctl print "gui/$(id -u)/com.lightnode.vm-screen-relay"
launchctl kickstart -k "gui/$(id -u)/com.lightnode.vm-screen-relay"
lsof -nP -iTCP@100.90.42.40:15900 -sTCP:LISTEN
```

Validate configuration changes before restarting:

```bash
/opt/homebrew/opt/nginx/bin/nginx -t \
  -c "$HOME/Library/Application Support/LightNodeHostRelay/nginx.conf" \
  -p "$HOME/Library/Application Support/LightNodeHostRelay"
```

The access log contains only the remote Tailscale address, connection duration,
status, and byte totals. It does not contain screen, keyboard, or clipboard data.

## Access control

Before sharing the host, add a Tailscale grant or ACL that allows the intended
user to reach only `100.90.42.40:15900/tcp`. Do not enable an exit node, advertise
the UTM subnet, publish TCP 15900 through the router, or expose guest TCP 5900 to
the public Internet.

The virtual Mac keeps its existing independent path:

```text
Chrome -> Mac Guard -> sing-box TUN -> Tokyo VPS 149.104.78.121
```

Changing or stopping this host relay does not change the guest route, DNS, PF,
or VLESS configuration.
