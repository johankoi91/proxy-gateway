# Beijing Runtime Audit Standard

Use this checklist after installation, upgrades, firewall changes, Docker
restart, host reboot, or a Tokyo node change.

## Required state

| Item | Expected |
| --- | --- |
| Host | Ubuntu 24.04 AMD64, Beijing VPS |
| Physical interface | `eth0`, private address `10.2.0.12` |
| TUN and DNS | `lightnode-tun`, DNS `172.19.0.2` |
| Tokyo endpoint and exit | `149.104.78.121:443`, exit `149.104.78.121` |
| Dashboard | `127.0.0.1:9180`, SSH tunnel only |
| Selkies | HTTPS TCP 8445, WebRTC UDP/TCP 59000 |

## Read-only audit

```bash
lightnode-linux-controller doctor
docker ps --no-trunc
docker inspect beijing-selkies-chromium
docker stats --no-stream
systemctl status lightnode-lockdown.service lightnode-container-dns.timer docker.service
nft -a list table inet lightnode_controller
ip -brief address
ip -4 route show table all
ip -6 route show table all
ip -d rule show
resolvectl status eth0
ss -lntup
chronyc tracking
```

Acceptance criteria:

- Controller and Selkies are running and healthy.
- All nine Controller checks are healthy; `protected=true`, `connected=true`.
- Host and Selkies container observe `149.104.78.121`.
- Output and forward chains are policy drop at priority `filter + 10`.
- Physical egress allows only the Tokyo endpoint; no public IPv6 fallback.
- Public ingress contains TCP 8445 and UDP/TCP 59000 and does not contain 8443.
- Dashboard is loopback-only.
- Selkies logs show a successful UDP candidate pair and `ICE completed`.
- The active client resolution and DPI match the client's reported values.
- Credentials and node secrets remain mode `0600` and are not printed.

## Evidence commands

```bash
curl --noproxy '*' -4 -fsS --max-time 10 https://api.ipify.org
docker exec beijing-selkies-chromium curl -4fsS --max-time 10 https://api.ipify.org
docker logs --since 15m beijing-selkies-chromium 2>&1 | \
  grep -E 'CandidatePair|ICE completed|Peer connection established|on_resize_handler|SETTINGS message'
curl -sk -o /dev/null -w '%{http_code}\n' https://154.8.225.152:8445/
```

An unauthenticated Selkies request must return `401`. Bind-to-interface curl is
not a valid bypass test because sing-box still intercepts it in OUTPUT. Use an
`eth0` packet capture while issuing the request: no packet may leave toward the
non-endpoint destination.

## Safe failure behavior

- Explicit safe pause stops sing-box and leaves nftables active.
- A missing TUN, broken DNS, or failed Tokyo endpoint must cause loss of browser
  Internet access, never Beijing direct egress.
- Unexpected sing-box exit is restarted after two seconds.
- Three consecutive egress failures trigger bounded recovery with a two-minute
  cooldown and at most three attempts.
- Fault injection requires explicit approval because it interrupts browsing.

## Report

Record findings by severity, runtime state, network-boundary evidence, image and
configuration integrity, resource use, skipped destructive tests, and residual
risks. If no leak is found, state that explicitly.
