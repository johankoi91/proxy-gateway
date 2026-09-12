# KasmVNC Chromium Workspace

This workspace runs official KasmVNC 1.5.0 and ARM64 Chromium inside the UTM
`Ubuntu-Server` VM. Linux Controller is deployed as a separate Docker Compose
project on the same Ubuntu host.

`vendor-kasmvnc.deb` is the official Bookworm ARM64 release asset. Its upstream
SHA-256 digest is checked during every image build.

```bash
docker compose up -d --build
```

Access from an existing remote WireGuard peer:

```text
https://10.88.0.254:8443/
username: admin
password: admin1111
```

The host Mac can simulate a remote path through Guangzhou at:

```text
https://10.88.0.254:8443/
```

Traffic path:

```text
remote browser -> Guangzhou hairpin 10.88.0.254:8443
               -> host Mac 10.88.0.2:8443
               -> host PF -> UTM Ubuntu 10.211.55.7:8443
               -> Docker -> KasmVNC -> Chromium

host simulation -> same Guangzhou hairpin path
```

Downloaded files are available through a separate read-only portal:

```text
https://10.88.0.254:8444/
username: downloads
password: admin1111
```

The portal uses FileBrowser Quantum `v1.5.6-stable`, pinned by SHA-256. Its
account can only list and download files from Chromium's `Downloads` directory.
The source is also mounted read-only and WebDAV is disabled, so upload, create,
modify, rename, delete, share, and execute operations are unavailable.

Generate its private TLS certificate before the first deployment:

```bash
sudo ./fileportal/generate-cert.sh
```

The Ubuntu VM does not run WireGuard. The host Mac owns the Guangzhou remote
access path and forwards KasmVNC TCP 8443 and file portal TCP 8444 to the
VM. Chromium egress is captured by Linux Controller and exits through the
Tokyo VPS. nftables blocks direct fallback through the UTM gateway.

Linux Controller keeps narrow inbound management exceptions for TCP 8443 and
8444 from the UTM gateway `10.211.55.1` to the VM's
`10.211.55.7`. These exceptions must not permit Chromium to bypass the Tokyo
egress policy.

The Guangzhou VPS permits TCP 8443 and 8444 only between WireGuard
peers. It does not listen on either public port. Both services use self-signed
certificates, so the browser requires a one-time certificate exception per
entry point.

This deployment keeps KasmVNC on TCP and sends `Cache-Control: no-store` for
the client assets. Kasm WebRTC was tested through the nested WireGuard relay,
but its unordered, zero-retransmit data channel produced corrupted incremental
frames during packet loss. The image forces WebRTC off even when a browser has
previously persisted the enabled setting.

Verified runtime evidence:

```text
KasmVNC: stable WebSocket over TCP
Ubuntu -> YouTube generate_204: about 0.35-0.44 seconds
Tokyo tunnel 10 MB download: about 6.8 MiB/s
```

Chromium is started with Chinese as the preferred language and QUIC disabled.
Google misclassifies the Tokyo VPS address as Myanmar in its own location
database; forcing `zh-CN` prevents that location choice from turning the page
interface into Burmese. Disabling QUIC keeps YouTube media requests on reliable
TCP through the VLESS path instead of attempting unstable UDP 443 first.

Fcitx5, its GTK3 frontend, and its Pinyin engine run inside the remote Linux
desktop because the local Mac input-method composition state does not cross
KasmVNC. Press `F8` inside the remote Chromium window to switch between the US
keyboard and Pinyin. `Control + Space` is also supported, but macOS may consume
that shortcut locally. The input-method profile persists with `chromium-home`.

Check, restart, and stop:

```bash
docker compose ps
docker compose logs --tail=100 chromium file-portal
docker compose restart chromium
docker compose restart file-portal
docker compose down
```

The `chromium-home` volume persists Chromium profile data and KasmVNC login
credentials. Managed KasmVNC and Openbox configuration is refreshed from the
image at every container start.

The certificate and credentials are suitable only for the private deployment.
Do not expose port 8443 directly to the public Internet. Verify
`lightnode-linux-controller doctor` before using risk-sensitive services.
