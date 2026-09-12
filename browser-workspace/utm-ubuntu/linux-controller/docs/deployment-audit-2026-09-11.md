# Linux Controller Deployment Audit - 2026-09-11

## Target

```text
UTM Ubuntu: 10.211.55.7
Physical interface: enp0s1
UTM gateway: 10.211.55.1
Tokyo endpoint and expected egress: 149.104.78.121:443
Dedicated VLESS client: ubuntu-browser
```

Credentials are not recorded in this document or source control.

## Verified configuration

- sing-box `1.14.0`, official Linux ARM64 build with pinned hashes.
- TUN interface `lightnode-tun`, address `172.19.0.1/30`.
- DNS `172.19.0.2`, DoH detour fixed to `tokyo`.
- VLESS Reality is the only outbound; no direct or fallback outbound exists.
- nftables host output and Docker forward chains are fail-closed.
- Native IPv6 has no physical egress exception.
- Management subnet and Tokyo endpoint are excluded from TUN auto-routing.
- SSH remains available through `10.211.55.1`.
- KasmVNC TCP 8443 and file portal TCP 8444 remain available.
- sing-box runs only inside `lightnode-linux-controller` with a read-only root.
- The old native sing-box binary and systemd service were removed.
- Management dashboard bound to `10.211.55.7:9180` with no public forwarding.
- Dashboard API exposes no VLESS UUID, Reality public key, or short ID.

## Functional results

```text
Host public egress:       149.104.78.121
Chromium public egress:   149.104.78.121
Tunnel DNS:               healthy
Native IPv6 fallback:     blocked
Guangzhou 8443 hairpin:   reachable
Guangzhou 8444 hairpin:   reachable
```

## Failure regression

While `docker compose stop controller` was active:

```text
sing-box:                 inactive
lightnode-tun:            absent
host public Internet:     blocked
Chromium public Internet: blocked
SSH:                      reachable
KasmVNC:                  reachable
file portal:              reachable
Kill Switch:              active
```

After `docker compose up -d controller`, both host and Chromium returned to the
Tokyo egress.

The dashboard-managed pause was also verified: the Go web process stayed
reachable with HTTP 200 while its sing-box child and TUN were stopped. The host
status changed to `connected=false`, and the fixed systemd path triggered an
immediate fresh check. Dashboard-managed start restored the Tokyo egress.

The sing-box process was killed with `SIGKILL` during a live test. The same
Controller container started a new sing-box PID after two seconds and returned
to a healthy Tokyo egress without container replacement.

## Reboot regression

A complete UTM VM reboot verified:

- `lightnode-lockdown.service` active.
- `lightnode-linux-controller` container healthy.
- `lightnode-container-dns.timer` active.
- `docker.service` active.
- DNS automatically restored to `172.19.0.2` by the host timer.
- Both Docker services healthy.
- Chromium egress still `149.104.78.121`.
- Both remote entry points reachable.
- Dashboard automatically reachable at `10.211.55.7:9180`.

## Implementation findings

The Controller nftables filter hooks run at priority `10`. Raw priority cannot
use conntrack safely, and mangle priority can run before sing-box auto-redirect.
Host TCP redirected by sing-box is accepted only with `ct status dnat`; direct
physical TCP remains blocked except for the exact Tokyo endpoint.
