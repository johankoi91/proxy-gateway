# LightNode VPN Gateway

Portable personal VPN gateway with a local-only management console.

The image runs:

- WireGuard on UDP 443 with MTU 1280
- VLESS Reality on TCP 443 using `www.cloudflare.com` as the target
- dnsmasq on the WireGuard interface with AAAA filtering
- A Basic Auth protected Go + React console on `127.0.0.1:9080`

The console displays service health, WireGuard handshakes and transfer totals. It can create or revoke clients and provides WireGuard, VLESS and Clash configurations with QR codes.

For every client, the VLESS dialog also downloads `<client>.vps-config.json`. This
versioned file is intended for automated client provisioning, including a future
Mac Guard importer. It contains the selected client's VLESS UUID and Reality
short ID, so store it like a password.

## Requirements

- Linux x86_64 server
- Docker Engine and Docker Compose plugin
- Kernel WireGuard support
- TCP and UDP port 443 available
- Root access for the offline installer

The runtime container uses host networking plus `NET_ADMIN`, `NET_RAW` and `SYS_MODULE`. Treat the image and its persistent `/data` directory as privileged infrastructure.

## Offline installation

Copy the complete offline bundle to the new server, then run:

```bash
cd lightnode-vpn-gateway-offline-1.0.1
sha256sum -c SHA256SUMS
sudo ./install.sh
```

The installer:

1. Loads the local Docker image tar without contacting a registry.
2. Creates `/opt/lightnode-vpn-gateway` with default credentials `admin` / `admin1111`.
3. Enables IPv4/IPv6 forwarding.
4. Opens TCP/UDP 443 when UFW is already active.
5. Starts the gateway with persistent state under `/opt/lightnode-vpn-gateway/data`.

The image is offline, but first startup uses `api.ipify.org` to discover the server public IPv4 unless `PUBLIC_IP` is already set in `.env`. Set it manually for a fully disconnected installation.

## Open the console

The admin server is deliberately not exposed publicly. Create a tunnel:

```bash
ssh -L 9080:127.0.0.1:9080 root@SERVER_IP
```

Open [http://127.0.0.1:9080](http://127.0.0.1:9080) and use the credentials in:

```text
/opt/lightnode-vpn-gateway/.env
```

Do not publish port 9080. The console exposes client private keys and QR codes.

### Optional public HTTPS access

Direct public access is opt-in and uses a self-signed certificate because the
server is addressed by IP rather than a DNS name:

```bash
sudo PUBLIC_ADMIN=true ./install.sh
```

Then open `https://SERVER_IP`. The browser will show a certificate warning
until you explicitly trust `tls/server.crt`. Never expose the console over plain
HTTP; Basic Auth credentials and client private keys would be readable in transit.

TCP 443 is shared by an SNI-aware HAProxy frontend: the configured admin server
name is passed to the HTTPS console, while all other TLS traffic is passed to
VLESS Reality on its container-local port. WireGuard continues to use UDP 443.

## VPS configuration export

Open a client, choose **VLESS Reality**, then select **VPS 配置**. The authenticated
endpoint is:

```text
GET /api/clients/{id}/vps-config.json
```

The response is an attachment with `Cache-Control: no-store` and this stable
shape:

```json
{
  "schema": "lightnode.vps-config",
  "version": 1,
  "client": { "id": "...", "name": "mac-vm" },
  "endpoint": {
    "address": "149.104.78.121",
    "port": 443,
    "expectedEgress": "149.104.78.121"
  },
  "vless": {
    "uuid": "REDACTED",
    "flow": "xtls-rprx-vision",
    "transport": "tcp",
    "encryption": "none",
    "tls": {
      "enabled": true,
      "serverName": "www.cloudflare.com",
      "utlsFingerprint": "chrome",
      "reality": {
        "enabled": true,
        "publicKey": "REDACTED",
        "shortId": "REDACTED"
      }
    }
  }
}
```

The export does not contain the WireGuard private key, the server's Reality
private key, the SSH key or the console password. Do not commit downloaded
exports to source control.

For a browser-trusted certificate, point a DNS name directly at the server and
use Certbot. Install `scripts/certbot-deploy-hook.sh` as a Certbot deploy hook so
renewed certificates are copied into `tls/` and the gateway is restarted.

Recent Certbot releases can request a Let's Encrypt `shortlived` certificate
for the public IP itself. These certificates last six days, so enable the
bundled `certbot-ip-renew@.timer`; the timer checks four times daily and Certbot
renews only when the ACME renewal window opens.

## Operations

```bash
cd /opt/lightnode-vpn-gateway
docker compose ps
docker compose logs -f
docker compose restart
docker compose down
```

Back up both files before migration:

```text
/opt/lightnode-vpn-gateway/.env
/opt/lightnode-vpn-gateway/data/state.json
```

`state.json` contains every client credential. Keep backups encrypted and mode `0600`.

## Migrate to another server

Install the same offline image on the destination, stop both gateway containers, copy `data/state.json`, then update `PUBLIC_IP` and `EGRESS_INTERFACE` in the destination `.env`. On startup the gateway applies those endpoint values to the migrated state, preserving client keys while generating downloadable profiles for the new server address.

## Build the offline bundle

```bash
make test
make offline
```

The build pins Xray `v26.3.27` and verifies the upstream Linux archive SHA-256 before adding it to the image.

## Security defaults

- Management binds only to loopback.
- Basic Auth is mandatory.
- State and generated runtime configurations use mode `0600`.
- Client QR responses use `Cache-Control: no-store`.
- WireGuard peers receive independent keys.
- VLESS clients receive independent UUIDs and Reality short IDs.
- IPv6 is routed into the tunnel while the internal DNS suppresses AAAA responses, preventing native IPv6 bypass on servers without public IPv6.
