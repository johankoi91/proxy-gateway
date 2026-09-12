# LightNode Linux Controller for Beijing

Linux Controller provides the fail-closed Tokyo egress boundary for the
Beijing Selkies browser.

```text
Selkies Chrome and host traffic
  -> lightnode-tun
  -> sing-box VLESS Reality TCP 443
  -> Tokyo VPS 149.104.78.121
  -> Internet
```

The nftables boundary permits public ingress only for Selkies HTTPS TCP 8445,
Controller HTTPS TCP 9180, and WebRTC UDP/TCP 59000. These are inbound
exceptions and never physical egress exceptions. A non-loopback dashboard
refuses to start without both TLS and Basic Auth credentials.

## Build and install

```bash
./scripts/build-linux-amd64.sh
sudo ./scripts/install.sh --vps-config /path/to/beijing-browser.vps-config.json
```

The package pins sing-box `1.14.0`, its release hashes, and the CA bundle.
Installation discovers the physical interface, generates strict configuration,
loads the boot Kill Switch, starts the container, applies tunnel DNS, and
requires a healthy Tokyo egress before succeeding.

## Operations

```bash
sudo lightnode-linux-controller doctor
docker compose -f /opt/lightnode/beijing-browser/controller/compose.yaml ps
docker compose -f /opt/lightnode/beijing-browser/controller/compose.yaml logs --tail=100 controller
```

Use the dashboard's safe pause for normal maintenance. It stops sing-box while
leaving nftables fail-closed and the authenticated dashboard reachable. Unexpected
sing-box exit is restarted after two seconds; repeated egress failure enters
bounded automatic recovery without enabling direct fallback.

The 10-second host refresh also validates the active nftables boundary. If the
Controller table is missing or incomplete, the root host service validates the
persistent file and reloads it before publishing the refreshed status.

## Managed files

```text
/etc/lightnode-linux-controller/controller.json
/etc/lightnode-linux-controller/sing-box.json
/etc/lightnode-linux-controller/lockdown.nft
/etc/systemd/system/lightnode-lockdown.service
/etc/systemd/system/lightnode-container-dns.timer
/etc/chrony/conf.d/lightnode-controller.conf
/opt/lightnode/beijing-browser/controller/compose.yaml
```

Node credentials are mode `0600` and diagnostics do not print them.

The public dashboard is `https://154.8.225.152:9180/`. Its self-signed
certificate requires a one-time browser exception. Credentials are stored only
in the server-side `.env` file with mode `0600`.
