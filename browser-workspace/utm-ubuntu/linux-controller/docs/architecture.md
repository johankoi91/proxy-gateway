# Linux Controller Architecture

## Data planes

The design keeps remote access and protected Internet egress separate:

```text
Remote Mac
  -> WireGuard -> Guangzhou VPS -> host PF
  -> UTM Ubuntu 8443/8444

Chromium container
  -> Docker bridge -> Linux routing
  -> Controller container (host network)
  -> lightnode-tun -> VLESS Reality
  -> Tokyo VPS -> Internet
```

The Guangzhou route is inbound management only. It does not become an Ubuntu
Internet egress. The Tokyo route is outbound only and is not used to expose the
browser workspace.

## nftables hooks

The dedicated `inet lightnode_controller` table uses priority `10` and does
not modify Docker-owned chains.

Priority `10` is deliberately after conntrack, sing-box auto-redirect marking,
and Docker's standard filter hooks. An accept verdict in an earlier base chain
does not bypass a later base chain, so the Controller can still drop an unsafe
physical egress. Running at raw or mangle priority would either inspect state
before conntrack or block traffic before sing-box can redirect it to the TUN.
Host-local TCP that sing-box has redirected to its loopback listener is accepted
only when conntrack reports `dnat`; this does not permit an unredirected socket
to leave through the physical interface.

```text
input:   accept; UTM Shared Network and host firewall define the inbound edge
output:  default drop; allow loopback, management subnet, TUN, DHCP,
         and the exact Tokyo endpoint
forward: default drop; allow host management into published containers
         and container traffic whose selected egress is the TUN
```

Both IPv4 and IPv6 traverse an `inet` base chain. No physical IPv6 exception
is defined, so native IPv6 cannot become a fallback path.

The Kill Switch does not use an early drop policy for host-local input. That
policy cannot improve egress leak protection and can lock out SSH before route
diagnostics are available. Published container ports still cross the strict
`forward` chain and remain limited to 8443/8444 from the management subnet.

## Boot ordering

```text
local filesystems
  -> lightnode-lockdown.service
  -> network-pre.target
  -> Docker
  -> lightnode-linux-controller container
  -> lightnode-container-dns.timer
```

The boot unit uses the already generated endpoint and interface values and does
not need DNS. A VPS hostname is intentionally unsupported at this boundary.

## DNS

systemd-resolved receives `172.19.0.2` as the route-only default DNS. sing-box
hijacks DNS in the TUN and sends Cloudflare DoH through the `tokyo` outbound.
When sing-box stops, the address stops answering while direct DNS remains
blocked by nftables.

## Docker

Docker continues to own its bridge, DNAT, and connection tracking chains. The
Controller does not flush or rewrite them. Its independent forward hook accepts
only inbound management ports and traffic routed to `lightnode-tun`; direct
container traffic to the physical interface is dropped.

The Controller container uses host networking, `/dev/net/tun`, and only
`NET_ADMIN` plus `NET_RAW`. Its root filesystem is read-only. The generated
configuration is mounted read-only, and no Docker socket is mounted. The image
contains only the static Controller, static sing-box, and a pinned CA bundle;
there is no package-manager step during the build.

The only host-side executable is the small Controller CLI used by the boot
Kill Switch, DNS timer, and diagnostics. It does not proxy traffic. This host
bootstrap is required because no container can load a firewall before Docker
itself starts.

## Management plane

The Go process inside the Controller container owns both the React API and the
sing-box child process. Pausing the Tokyo path terminates only the child, so the
management page remains available at `10.211.55.7:9180`.

Unexpected child exit triggers an in-container restart after two seconds. The
desired-state flag is cleared before an explicit pause, so supervision cannot
undo an operator's fail-closed maintenance state.

The host timer writes a sanitized report to
`/run/lightnode-linux-controller/host-status.json` every 10 seconds. The runtime
directory is shared with the container so it can create one fixed
`refresh-request` marker. A systemd path unit translates only that marker into
the predefined host check; it cannot contain a command or arguments. The
container has no Docker socket and cannot execute arbitrary host commands.
Node UUID and Reality credentials are never returned by the API.

Port 9180 is bound to the UTM IPv4 address and is not forwarded by host PF or
the Guangzhou VPS. The local host can open it directly; remote users can open
it inside the KasmVNC browser workspace.

## Trust boundary

Root can disable nftables or replace the binaries and is outside the threat
model. The design prevents accidental route changes, tunnel crashes, DHCP DNS,
native IPv6, and Docker forwarding from exposing the local public egress.
