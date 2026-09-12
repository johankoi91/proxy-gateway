# Beijing Controller Architecture

## Data path

```text
Remote client -- HTTPS 8445 + WebRTC UDP 59000 --> Selkies
Selkies Chrome -- Docker bridge --> lightnode-tun
lightnode-tun -- VLESS Reality TCP 443 --> Tokyo VPS --> Internet
```

The remote media path terminates in Beijing. Browser Internet traffic is a
separate outbound path and always terminates in Tokyo.

## Enforcement

`inet lightnode_controller` uses output and forward hooks at filter priority
`+10`, both with default-drop policies. Output permits established traffic,
loopback, DHCP, the Tencent private subnet, the exact Tokyo endpoint, and the
TUN. Forward permits established traffic, Selkies ingress on TCP 8445 and
UDP/TCP 59000, and traffic whose selected egress is the TUN.

The rules cover IPv4 and IPv6. No physical IPv6 exception exists. Docker owns
its own DNAT and bridge chains; Controller adds an independent later boundary
and does not flush Docker tables.

## Boot and management

`lightnode-lockdown.service` loads the Kill Switch before Docker. Controller
runs with host networking, `/dev/net/tun`, `NET_ADMIN`, and `NET_RAW`; it does
not mount the Docker socket. It supervises sing-box and exposes the React API on
loopback `127.0.0.1:9180`.

DNS is fixed to `172.19.0.2` and hijacked into Tokyo DoH. If the tunnel fails,
DNS and Internet access fail closed rather than using Tencent DNS or native
IPv6.
