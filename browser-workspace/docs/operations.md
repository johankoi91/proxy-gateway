# Operations

## 入口

```text
https://10.88.0.254:8443/
```

## Ubuntu

```bash
ssh ubuntu-server
cd /opt/lightnode/browser-workspace
docker compose ps
docker compose logs --tail=100 chromium
```

## 宿主

```bash
sudo lightnode-browser-host doctor
sudo launchctl print system/com.lightnode.browser-host-wireguard
sudo launchctl print system/com.lightnode.browser-route
sudo pfctl -a com.apple.internet-sharing/lightnode-browser-route -sn
sudo pfctl -a com.apple.internet-sharing/lightnode-browser-route -sr
```

## 广州 VPS

```bash
ssh root@106.53.243.63
wg show wg0
iptables -nvL FORWARD
iptables -t nat -nvL PREROUTING
iptables -t nat -nvL POSTROUTING
```

按远端 Peer、VPS hairpin、宿主 WireGuard、宿主 PF、Ubuntu 8443、Docker 健康状态的顺序排查。
