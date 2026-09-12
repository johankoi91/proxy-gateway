# LightNode Linux Controller

Linux Controller protects the UTM Ubuntu browser workspace with one fixed
Tokyo egress:

```text
Chromium and Ubuntu system traffic
  -> lightnode-tun
  -> sing-box VLESS Reality TCP 443
  -> Tokyo VPS 149.104.78.121
  -> Internet
```

It is independent from the Guangzhou WireGuard remote-access path. Ubuntu does
not run WireGuard.

## Security boundary

- nftables defaults host `OUTPUT` and Docker `FORWARD` traffic to drop.
- The physical interface can only reach the Tokyo VLESS endpoint.
- IPv6 is covered by the same `inet` table and cannot bypass the tunnel.
- DNS is fixed to the TUN address `172.19.0.2` and resolved through Tokyo DoH.
- The host gateway retains inbound SSH, KasmVNC TCP 8443, and file portal
  TCP 8444 access.
- There is no direct, selector, fallback, or secondary proxy outbound.

## Current status

The network core is active on UTM Ubuntu `10.211.55.7`. The dedicated Tokyo
client is `ubuntu-browser`. Host and Chromium probes both observe
`149.104.78.121`. sing-box and its supervisor run in the
`lightnode-linux-controller` container. Docker stop/start and a full VM reboot
have passed the fail-closed checks.

## Build

```bash
./scripts/build-linux-arm64.sh
```

The package contains official sing-box `1.14.0` for Linux ARM64. The upstream
release archive and extracted binary hashes are pinned in
`SING_BOX_ASSET_SHA256` and `SING_BOX_BINARY_SHA256`.

## Install

Create a dedicated client in the Tokyo VPS console and download its
`*.vps-config.json`, then run on Ubuntu:

```bash
sudo ./scripts/install.sh --vps-config /path/to/linux-browser.vps-config.json
```

Offline image installation:

```bash
sudo ./scripts/install.sh \
  --vps-config /path/to/linux-browser.vps-config.json \
  --offline-image /path/to/lightnode-linux-controller-0.1.0-alpha.2-arm64.tar.gz
```

Installation validates strict JSON, sing-box syntax, nftables syntax, binary
hashes, and the detected physical interface. It builds the zero-download image,
loads the Kill Switch, starts the container, fixes DNS, and only reports success
after Docker health confirms the Tokyo egress. Migration restores the old
provider if the new container fails before becoming healthy.

## Operations

```bash
sudo lightnode-linux-controller doctor
cd /opt/lightnode/linux-controller
docker compose ps
docker compose logs --tail=100 controller
docker compose restart controller
```

Management dashboard:

```text
http://10.211.55.7:9180/
```

The dashboard is bound to the detected UTM IPv4 address. It shows the host
nftables/DNS checks, Docker sing-box process, TUN, IPv6 protection, Tokyo
egress, and sanitized runtime configuration. It has no password because it is
not published by UTM, host PF, or the Guangzhou VPS. Do not add a public port
forward for 9180.

Use the dashboard's **安全暂停** button for normal operation. It stops only the
sing-box child process, so the dashboard stays reachable while nftables and
tunnel DNS remain in place. Ubuntu and Chromium lose Internet access while SSH,
KasmVNC, the dashboard, and the read-only download portal remain reachable.

The DNS timer repairs tunnel DNS every 10 seconds when the TUN exists. The boot
Kill Switch remains enabled during pause and is ordered before Docker.
If sing-box exits unexpectedly, the Controller restarts the child after a
2-second backoff. An explicit dashboard pause disables that automatic restart.

`docker compose stop controller` is an emergency stop, not the normal pause:
the network still fails closed, but the dashboard also becomes unavailable.

## Files

```text
/etc/lightnode-linux-controller/controller.json
/etc/lightnode-linux-controller/sing-box.json
/etc/lightnode-linux-controller/lockdown.nft
/etc/systemd/system/lightnode-lockdown.service
/etc/systemd/system/lightnode-container-dns.service
/etc/systemd/system/lightnode-container-dns.timer
/opt/lightnode/linux-controller/compose.yaml
/run/lightnode-linux-controller/host-status.json
```

Node credentials are mode `0600` and are never printed by diagnostics.
The host does not retain a sing-box executable or service.

## Offline image

The verified ARM64 image is exported at:

```text
dist/offline/lightnode-linux-controller-0.1.0-alpha.2-arm64.tar.gz
SHA-256: 242d9f40c8b6018f05f7d046cd92b6c2af25691d2552389cc1a68e81a52475e2
```
