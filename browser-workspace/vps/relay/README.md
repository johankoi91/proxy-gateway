# Browser Relay

运行组件：

```text
wg-quick@wg0.service
/usr/local/sbin/lightnode-browser-relay-rules
/usr/local/sbin/lightnode-add-remote-peer
/usr/local/sbin/lightnode-remove-remote-peer
```

数据面只允许：

```text
10.88.0.0/24 -> 10.88.0.254:8443
DNAT -> host peer 10.88.0.2:8443
```

远端 Peer 配置只包含 `AllowedIPs = 10.88.0.0/24`。
