# Operations

## 入口

```text
远程 Chromium: https://10.88.0.254:8443/  (admin / admin1111)
文件下载:      https://10.88.0.254:8444/  (downloads / admin1111)
```

在远程 Chromium 中把文件保存到 `Downloads`。然后在远端 Mac 自己的浏览器
打开文件下载入口，登录后直接下载到远端 Mac。入口只读，不接受上传、删除、
改名或修改。

## Ubuntu

```bash
ssh ubuntu-server
cd /opt/lightnode/browser-workspace
docker compose ps
docker compose logs --tail=100 chromium file-portal
```

### Linux Controller

管理后台：<http://10.211.55.7:9180/>

```bash
sudo lightnode-linux-controller doctor
cd /opt/lightnode/linux-controller
docker compose ps
docker compose restart controller
```

日常安全暂停、启动和重启使用后台按钮。按钮只管理容器内的 sing-box 子进程，
后台自身不会停止。暂停后 nftables Kill Switch 和隧道 DNS 保持生效，因此 Ubuntu
公网失败关闭；SSH、后台、KasmVNC 和文件下载入口继续可用。

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

按远端 Peer、VPS hairpin、宿主 WireGuard、宿主 PF、Ubuntu 8443/8444、Docker
状态的顺序排查。
