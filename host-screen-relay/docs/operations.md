# Operations

## 日常检查

VPS：

```bash
systemctl is-active wg-quick@wg0 lightnode-relay-admin
wg show wg0
curl -u admin:admin http://127.0.0.1:9185/api/status
```

宿主 Mac：

```bash
sudo /usr/local/bin/lightnode-host-relay doctor
launchctl print system/com.lightnode.host-wireguard
launchctl print system/com.lightnode.sunshine-route
route -n get 10.88.0.1
```

Guest：

```bash
curl https://api.ipify.org
scutil --dns
networksetup -getinfo Ethernet
```

当前预期值为公网出口 `149.104.78.121`、DNS `172.19.0.2`、IPv6 `Off`。

## 后台操作

- “新增客户端”：为一台远端 Mac 生成唯一 Peer。
- “下载配置”：取得该远端 Mac 的 `client.conf`。
- “撤销”：立即从运行态和持久配置删除 Peer，并移入 VPS 的 revoked 目录。
- “下载安装包”：下载宿主 PKG、单例 `host.conf` 和一键安装命令组成的 ZIP。
- “仅下载配置”：只用于同一宿主恢复或受控迁移。

## 宿主命令

重新应用配置：

```bash
sudo /usr/local/bin/lightnode-host-relay configure /path/to/host.conf
```

停止但保留配置：

```bash
sudo /usr/local/bin/lightnode-host-relay uninstall
```

完全删除宿主配置和私钥：

```bash
sudo /usr/local/bin/lightnode-host-relay uninstall --purge
```

## 故障定位顺序

1. VPS `wg-quick@wg0` 是否 active。
2. VPS 是否能看到宿主 `10.88.0.2` 的近期握手。
3. 宿主 `doctor` 是否找到 WireGuard `utun`。
4. 宿主 PF 父 anchor 和 Sunshine 子 anchor 是否存在。
5. 宿主能否访问 `10.88.0.254` 的 Sunshine TCP 端口。
6. Guest Sunshine 是否运行并监听端口。
7. 远端 Peer 是否有近期握手和流量计数。

宿主切换 Wi-Fi 后 WireGuard Endpoint 会重新漫游，正常情况下无需手工重置。若链路没有
恢复，先执行 `doctor`，再执行 `configure`；不要修改 Guest 的 mac-guard 来修广州链路。
