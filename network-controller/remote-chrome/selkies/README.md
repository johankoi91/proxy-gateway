# Beijing Remote Chrome

This is the production remote-browser service on the Beijing VPS. The React
client and Go Gateway live in `../web`; the Browser API and Selkies runtime
are kept beside it so this directory is the single Compose deployment entry.

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
- The derived image adds the Chrome desktop entry, locale, and sandbox setup.

## Chrome sandbox

The derived image removes the upstream wrapper's forced `--no-sandbox`, fixes
the sandbox helper ownership and mode, and runs Chrome as UID 1000. Compose
does not grant `SYS_ADMIN` and does not use privileged mode. It permits Chrome
namespace creation through Docker's outer seccomp boundary while retaining
`no-new-privileges`; renderer processes then enter separate user/PID
namespaces and apply Chrome's own seccomp filter.

Verify the deployed process rather than only inspecting the image:

```bash
docker exec beijing-selkies-chromium pgrep -af '^/opt/google/chrome/chrome '
docker exec beijing-selkies-chromium stat -c '%a %U:%G %n' \
  /opt/google/chrome/chrome-sandbox
```

The main browser command must not contain `--no-sandbox`, and the helper must
be `4755 root:root`.

## Chinese input and long prompts

The image includes Fcitx5 as a fallback, but keeps the remote desktop in the US
keyboard input method. Direct server-side composition is not the default
because repeated composition updates can turn into backspaces in a remote
`contenteditable` field.

Use the fixed `文字输入` button in the Selkies client instead. It opens a local
multiline editor that never emits remote key events while editing. Clicking
`粘贴到远端` sends the complete UTF-8 text through the multipart clipboard data
channel, waits for that send to finish, and then issues one remote `Ctrl+V`.
Press Escape or click `关闭` to leave this mode.

When remote text arrives, the client shows `复制到本机`. That action must be
clicked by the user because browsers only permit system clipboard writes from
a user gesture. It writes the latest remote text with the Clipboard API and
falls back to `execCommand("copy")`; it does not rely on an asynchronous
WebRTC callback being allowed to change the host clipboard.

The pinned Selkies core is patched so intermediate `input` events are ignored
while an IME composition is active. Only `compositionend` commits text. The
image build requires the patch to match both source and bundled core exactly;
an upstream layout change fails the build instead of silently restoring the
destructive preedit/backspace behavior.

For long prompts, compose text on the client computer and paste it through the
Selkies clipboard channel. Text clipboard payloads are multipart rather than
keystroke replay, so Unicode text is not subject to keyboard-layout conversion.
The deployment verification pastes and reads back at least 28 KB of Chinese
text and requires byte length and SHA-256 to match.

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

The session is fixed at 2560x1440 and follows the client's DPI. H.264 runs at
45 FPS with GCC congestion control over a 5-12 Mbps range, initially 10 Mbps.
Static paint-over restores text quality after motion while scrolling is allowed
to reduce quality instead of accumulating latency. Chrome uses a Simplified
Chinese locale and preferred-language list.

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
gzip -dc beijing-selkies-chromium-2.0.6-amd64.tar.gz | docker load
```
