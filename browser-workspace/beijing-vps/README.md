# Beijing Browser Workspace

The Beijing VPS runs one remote-browser implementation: Selkies WebRTC with
Chrome. Linux Controller forces all host and container Internet traffic through
the Tokyo fixed exit.

```text
Remote browser
  -> HTTPS TCP 154.8.225.152:8445
  -> WebRTC UDP 154.8.225.152:59000
  -> Selkies + Chrome on Beijing VPS
  -> lightnode-tun
  -> VLESS Reality TCP 149.104.78.121:443
  -> Internet via 149.104.78.121
```

ICE-TCP 59000 is retained only as a fallback for client networks that block
UDP. The active deployment has verified direct WebRTC UDP.

## Services

```text
/opt/lightnode/beijing-browser/controller
/opt/lightnode/beijing-browser/selkies
/etc/lightnode-linux-controller
```

Selkies: `https://154.8.225.152:8445/`

Controller dashboard remains private on `127.0.0.1:9180`:

```bash
ssh -L 9181:127.0.0.1:9180 beijing-server
```

## Security boundary

- Host `OUTPUT` and Docker `FORWARD` are fail-closed.
- Physical output allows DHCP, the Tencent private subnet, and only the Tokyo
  endpoint `149.104.78.121:443`.
- Public container ingress is limited to TCP 8445 and UDP/TCP 59000.
- DNS uses tunnel address `172.19.0.2`; native IPv6 has no physical fallback.
- There is no direct, selector, secondary proxy, or alternate browser service.

## Operations

```bash
ssh beijing-server
lightnode-linux-controller doctor
docker compose -f /opt/lightnode/beijing-browser/controller/compose.yaml ps
docker compose -f /opt/lightnode/beijing-browser/selkies/compose.yaml ps
```
