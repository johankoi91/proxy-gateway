# Beijing Selkies Browser

This is the production remote-browser service on the Beijing VPS.

```text
Browser client
  -> HTTPS TCP 8445 (page and signaling)
  -> WebRTC UDP 59000 (preferred video/input transport)
  -> Selkies + Chrome on Beijing VPS
  -> Linux Controller TUN
  -> Tokyo VLESS Reality TCP 443
  -> Internet via 149.104.78.121
```

The server also exposes ICE-TCP 59000 as a fallback when a client network
blocks UDP. It is not the preferred transport. `SELKIES_WEBRTC_ICE_LITE=true`
and the public 1:1 NAT address allow direct ICE without a public TURN relay.

## Version boundary

- Selkies release: `v2.0.0rc0`
- Platform: Linux AMD64
- The exact upstream manifest is recorded in `UPSTREAM_IMAGE_DIGEST`.
- The derived image only adds a Chrome autostart desktop entry.

## Deploy

Create the server-only secret file and keep it mode `0600`:

```bash
cp .env.example .env
chmod 0600 .env
docker compose build
docker compose up -d
```

Open `https://154.8.225.152:8445/` and accept the self-signed certificate.
The username and password come from `.env`.

Required inbound ports are TCP 8445, UDP 59000 and TCP 59000. They are ingress
exceptions only. Container Internet traffic remains forced through
`lightnode-tun`; no direct physical egress exception is added.

## Verify

The session follows the client's native resolution and DPI. H.264 is fixed at
30 FPS and 12 Mbps with a maximum QP of 32 to preserve browser text. Chrome
uses a Simplified Chinese locale and preferred-language list.

```bash
docker compose ps
docker compose logs --tail=200 desktop
sudo nft list table inet lightnode_controller
curl -skI https://127.0.0.1:8445/
```

In the Selkies side panel, verify the active transport is WebRTC and the ICE
candidate is UDP. Inside its Chrome, `https://api.ipify.org` must return
`149.104.78.121`.

## Offline image

After a successful deployment:

```bash
./export-offline-image.sh
```

Restore on another AMD64 host with:

```bash
gzip -dc beijing-selkies-chromium-2.0.0rc0-amd64.tar.gz | docker load
```
