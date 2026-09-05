# Fixed-Exit VPN and Kill Switch Design

## 1. Purpose

This document defines the final cross-platform design for routing Claude, GPT,
YouTube, browsers, DNS, and other public traffic through the fixed Tokyo VPS
without falling back to the client's local public IP when the tunnel fails.

The primary security property is:

```text
Tunnel healthy  -> traffic exits through 149.104.78.121
Tunnel unhealthy -> public Internet traffic fails closed
Never            -> silently fall back to the local ISP address
```

This design complements the DNS and IPv6 analysis in
`docs/vpn-dns-ipv6-leak-and-claude.md`.

## 2. Current State

### Tokyo VPS

```text
Public IPv4: 149.104.78.121
Operating system: Ubuntu 24.04 x86_64
Container runtime: Docker Engine + Compose
VLESS Reality: TCP 443
WireGuard: UDP 443
Admin console: HTTPS TCP 443, routed by HAProxy SNI inspection
VLESS internal listener: 127.0.0.1:8443
Admin internal listener: 127.0.0.1:9080
```

The VPS currently runs:

- HAProxy as the public TCP 443 multiplexer.
- Xray as the VLESS Reality server.
- WireGuard as an optional UDP VPN server.
- dnsmasq for WireGuard tunnel DNS with AAAA filtering.
- The Go + React management console.

### Client status

- Android: v2rayNG with VLESS Reality works.
- macOS development machine: Clash Verge/Mihomo with VLESS Reality works.
- WireGuard requests reach the VPS, but UDP responses do not reach the tested
  Android and macOS clients on the tested network paths.
- The long-term client design therefore uses sing-box TUN where deterministic
  fail-closed behavior is required.

## 3. Scope and Non-Goals

### In scope

- Fixed VPS egress or no public connectivity.
- Global TUN routing rather than per-domain proxying.
- DNS leak prevention.
- IPv6 business-request leak prevention.
- Boot-time protection.
- Automatic tunnel recovery without disabling the Kill Switch.
- Local SSH and screen-sharing access during tunnel failures.
- macOS and Linux clients using one conceptual design.

### Out of scope

- Making a data-center IP appear residential.
- Guaranteeing acceptance by Claude or OpenAI risk systems.
- Hiding account history, device identity, payment region, GPS, timezone, or
  browser cookies.
- Automatically switching to a different country or public IP.
- Allowing unprotected Internet access when the tunnel is unavailable.

Claude and OpenAI may still evaluate the VPS ASN, IP reputation, account
history, device consistency, and region availability. A correct tunnel prevents
IP leakage; it does not override service policy or reputation systems.

## 4. Final Architecture

```text
Applications / browsers / DNS
              |
              v
        sing-box TUN
              |
              v
   VLESS Reality over TCP 443
              |
              v
       macOS PF / Linux nftables
              |
     only direct public exception
              |
              v
       149.104.78.121:443
              |
              v
          Tokyo VPS
              |
              v
           Internet
```

The firewall does not need to recognize Claude. Every public request must enter
the TUN. On the physical interface, the only permitted public destination is
the fixed VPS endpoint.

## 5. Platform Responsibilities

| Platform | Tunnel | Kill Switch | Service manager |
| --- | --- | --- | --- |
| VPS | Xray VLESS Reality server | UFW + provider firewall | Docker Compose |
| macOS | sing-box TUN | PF Anchor | launchd |
| Linux client | sing-box TUN | nftables | systemd |
| Android | v2rayNG or sing-box-based client | Android always-on VPN/lockdown | Android VPN service |

The server remains Xray. sing-box clients are protocol-compatible with the
existing VLESS Reality service, so the server does not need to be replaced.

## 6. macOS Design

### 6.1 sing-box

sing-box runs as a native `darwin/arm64` daemon with no Electron dependency.
Its responsibilities are:

- Create a global TUN interface.
- Route all public IPv4 traffic into the TUN.
- Hijack DNS into the local DNS engine.
- Connect to `149.104.78.121:443` using VLESS Reality.
- Disable IPv6 resolution while the VPS has no routed public IPv6.

Conceptual configuration:

```json
{
  "inbounds": [
    {
      "type": "tun",
      "interface_name": "utun-lightnode",
      "address": ["172.19.0.1/30"],
      "auto_route": true,
      "strict_route": true
    }
  ],
  "outbounds": [
    {
      "type": "vless",
      "server": "149.104.78.121",
      "server_port": 443,
      "flow": "xtls-rprx-vision",
      "tls": {
        "enabled": true,
        "server_name": "www.cloudflare.com",
        "reality": {
          "enabled": true,
          "public_key": "REDACTED",
          "short_id": "REDACTED"
        },
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        }
      }
    }
  ]
}
```

The real UUID, Reality public key, and short ID must be stored with mode `0600`
and must never be written into logs.

### 6.2 PF Kill Switch

PF is the final network boundary. It operates below application proxy settings
and blocks applications that ignore proxy configuration.

Physical-interface allowlist:

```text
loopback
current local management subnet
DHCP traffic
TCP 443 to 149.104.78.121
```

Physical-interface denylist:

```text
all other public IPv4 traffic
all public IPv6 traffic
direct DNS over UDP/TCP 53
direct DNS over TLS 853
direct DNS over HTTPS to non-VPS addresses
all direct public UDP traffic
```

Conceptual PF Anchor:

```pf
set skip on lo0

table <vps_endpoint> persist { 149.104.78.121 }
table <management_lan> persist { CURRENT_LOCAL_SUBNET }

pass out quick on $physical_if inet proto tcp \
  to <vps_endpoint> port 443 keep state
pass out quick on $physical_if inet \
  to <management_lan> keep state
pass out quick on $physical_if inet proto udp \
  from port 68 to port 67 keep state

block drop out quick on $physical_if inet all
block drop out quick on $physical_if inet6 all
```

The implementation must detect the active physical interface and management
subnet dynamically. It must not permanently assume `en0` or
`10.203.0.0/22`.

PF rules live under an independent anchor:

```text
/etc/pf.anchors/com.lightnode.vless-killswitch
```

The main `/etc/pf.conf` only references this anchor, preserving Apple and other
system anchors.

### 6.3 Local management

The current local subnet remains reachable for:

- SSH.
- macOS Screen Sharing.
- Local administrative recovery.

The firewall allows destinations inside the management subnet, not arbitrary
Internet traffic through the local gateway.

## 7. Linux Client Design

Linux clients use the same sing-box VLESS Reality outbound and a TUN inbound.
nftables replaces PF.

The nftables output policy allows:

```text
loopback
current management subnet
DHCP
TCP 443 to 149.104.78.121
traffic emitted through the sing-box TUN path
```

All other physical-interface IPv4 and IPv6 traffic is dropped. systemd starts
the nftables Kill Switch before sing-box.

The same fail-closed rule applies:

```text
sing-box failure -> no public Internet
VPS failure      -> no public Internet
network change   -> recompute local management subnet, keep blocking
```

## 8. Android Design

Android uses v2rayNG or a maintained sing-box-based client in VPN/TUN mode.

For strict protection:

- Do not use an include-only application list for Claude.
- Prefer all applications through the VPN, with explicit exclusions only when
  required.
- Include Claude, ChatGPT, browsers, WebView, and authentication helpers.
- Do not run WireGuard, RethinkDNS, another VPN, or a second proxy concurrently.
- Use Android always-on VPN when stable.
- Enable "block connections without VPN" only after validating the client and
  recovery behavior.

Android cannot use PF. Its final fail-closed boundary is the operating system's
always-on VPN lockdown.

## 9. DNS Design

DNS query leakage is not identical to business-request leakage. A local DNS
query usually exposes the queried hostname to the ISP but does not itself make
Claude see the local public IP. It is still blocked for privacy and consistency.

Normal DNS path:

```text
application query
  -> TUN DNS interception
  -> sing-box DNS engine
  -> remote DoH routed through VLESS
  -> VPS egress
```

Requirements:

- The VLESS server uses a fixed IP, so tunnel bootstrap needs no local DNS.
- Remote DNS connections must use the VLESS outbound.
- Direct UDP/TCP 53 and TCP 853 on the physical interface are blocked.
- Direct DoH to other public IPs is blocked by the general PF/nftables policy.
- DNS failure must not trigger fallback to the local resolver.

## 10. IPv6 Design

IPv6 request leakage is more dangerous than DNS leakage because the actual
Claude request can bypass the VPS.

While the VPS has no complete public IPv6 route:

- sing-box does not return or route public IPv6 destinations.
- The physical-interface firewall blocks public IPv6.
- The client does not permanently disable link-local IPv6 needed by macOS or
  local services.
- IPv6 tests must report unavailable, not the client's native IPv6 address.

When the VPS gains routed public IPv6, IPv6 can be enabled only after adding a
complete TUN route, server forwarding, firewall policy, and leak tests.

## 11. Control Plane

A small Go daemon, `lightnode-guard`, manages lifecycle and policy. Shell is
used only by the installer and emergency rollback.

Suggested layout:

```text
/usr/local/libexec/lightnode-guard
/Library/Application Support/LightNodeGuard/config.yaml
/etc/pf.anchors/com.lightnode.vless-killswitch
/Library/LaunchDaemons/com.lightnode.vless-guard.plist
/Library/LaunchAgents/com.lightnode.sing-box.plist
```

The Go daemon detects:

- Physical interface and gateway.
- Current local management subnet.
- sing-box process state.
- TUN interface presence.
- TCP reachability of VPS port 443.
- DNS health through the tunnel.
- Current public egress IP.
- PF Anchor presence and expected digest.

## 12. State Machine

```text
BOOT_LOCKED
  -> TUN_CONNECTING
  -> HEALTHY
  -> DEGRADED_LOCKED
  -> RECOVERING
```

Rules:

- Boot always starts locked.
- Health failure never disables the firewall.
- Recovery may restart sing-box with backoff.
- Public access is considered healthy only when the observed egress equals
  `149.104.78.121`.
- Local SSH and screen sharing remain available in every state.

## 13. Boot and Recovery Order

```text
macOS/Linux boot
  -> load PF/nftables Kill Switch
  -> permit only LAN management and VPS TCP 443
  -> start sing-box
  -> create TUN
  -> establish VLESS
  -> verify DNS and egress IP
  -> enter HEALTHY
```

If sing-box or the VPS fails, the system remains in `DEGRADED_LOCKED` and
retries without exposing the local public IP.

## 14. Maintenance Mode

Provide explicit commands:

```text
lightnode-guard status
lightnode-guard enable
lightnode-guard disable
lightnode-guard maintenance --minutes 5
```

Maintenance mode is time-limited and automatically restores the Kill Switch.
There is no persistent "temporarily allow direct Internet" state.

## 15. Safe Deployment Procedure

1. Back up sing-box/Clash configuration and `/etc/pf.conf`.
2. Install sing-box without changing the firewall.
3. Validate the generated configuration with `sing-box check`.
4. Enable global TUN and verify normal browsing, Claude, and YouTube.
5. Confirm IPv4 egress is `149.104.78.121`.
6. Confirm IPv6 is unavailable and DNS does not use the local ISP.
7. Generate the PF Anchor and validate it with `pfctl -vnf`.
8. Load PF with a three-minute automatic rollback timer.
9. Verify local SSH and screen sharing remain reachable.
10. Stop sing-box and confirm all public access fails.
11. Restart sing-box and confirm automatic recovery.
12. Install launchd persistence.
13. Reboot and repeat the full acceptance suite.
14. Remove the temporary rollback only after successful validation.

## 16. Acceptance Tests

### Healthy state

```text
IPv4 egress: 149.104.78.121
IPv6: unavailable
DNS: no local ISP resolver
Claude/browser/YouTube: functional through VLESS
Physical interface: only VPS TCP 443 plus local-management traffic
```

### Failure tests

```text
stop sing-box       -> public Internet fails
stop VPS Xray       -> public Internet fails
block VPS TCP 443   -> public Internet fails
break DNS config    -> DNS fails without local fallback
switch Wi-Fi        -> no direct-public-IP window
reboot before login -> no direct-public-IP window
```

### Recovery tests

```text
restore VPS         -> automatic recovery
restart sing-box    -> automatic recovery
local SSH/VNC       -> remains available throughout
```

### Packet-level audit

Capture the physical interface during normal use and during failures. Outside
the local subnet, the only permitted direct destination must be:

```text
149.104.78.121:443/TCP
```

## 17. Operational Caveats

- A captive portal cannot be used while the Kill Switch is active; use the
  time-limited maintenance mode.
- macOS updates may reload PF state; the guard verifies and restores its anchor.
- Changes to the VPS IP require an atomic client configuration and firewall
  update.
- Direct UDP applications either use VLESS-supported UDP encapsulation or fail;
  they never bypass the tunnel.
- The fixed VPS IP may still be classified as a data-center address by Claude or
  OpenAI.
- Stable use of one country and one IP is preferable to frequent endpoint
  switching, but must remain consistent with service availability and terms.

## 18. Implementation Decision

The final recommended stack is:

```text
Server:  Xray VLESS Reality + HAProxy + Docker
macOS:   sing-box TUN + PF + Go guard + launchd
Linux:   sing-box TUN + nftables + Go guard + systemd
Android: v2rayNG/sing-box VPN mode + Android lockdown
```

WireGuard remains deployed on the VPS as an optional high-performance protocol
for networks where bidirectional UDP is verified. It is not the fail-closed
primary tunnel for the currently tested clients.

## 19. Current Implementation Status

This document is a design specification. At the time of writing:

- The VPS VLESS service is deployed and working.
- Android v2rayNG and macOS Clash Verge can connect to VLESS.
- sing-box, PF Kill Switch, nftables Kill Switch, Go guard, launchd persistence,
  and systemd persistence described here have not yet been deployed.

