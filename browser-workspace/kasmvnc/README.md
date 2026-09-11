# KasmVNC Chromium POC

This proof of concept runs official KasmVNC 1.5.0 and ARM64 Chromium inside the
UTM `Ubuntu-Server` VM. It intentionally does not include Linux Controller yet.

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

The Ubuntu VM does not run WireGuard. The host Mac owns the Guangzhou remote
access path and forwards only KasmVNC TCP/UDP 8443 to the VM. Chromium currently
uses Ubuntu's ordinary default route. Do not use risk-sensitive accounts before
Linux Controller provides the Tokyo tunnel and fail-closed policy.

Linux Controller must keep one narrow management exception: allow established
TCP/UDP 8443 traffic from the UTM gateway `10.211.55.1` to the VM's
`10.211.55.7`. This exception is inbound-only and must not permit Chromium to
bypass the Tokyo egress policy.

The Guangzhou VPS permits TCP and UDP 8443 only between WireGuard peers. It
does not listen on public port 8443. KasmVNC uses a self-signed certificate, so
the browser requires a one-time certificate exception.

Check, restart, and stop:

```bash
docker compose ps
docker compose logs --tail=100 chromium
docker compose restart chromium
docker compose down
```

The `chromium-home` volume persists Chromium profile data and KasmVNC login
credentials. Managed KasmVNC and Openbox configuration is refreshed from the
image at every container start.

The certificate and credentials are suitable only for the private proof of
concept. Do not expose port 8443 directly to the public Internet. Do not sign in
to Claude, ChatGPT, or other risk-sensitive services before Linux Guard is
implemented and verified.
