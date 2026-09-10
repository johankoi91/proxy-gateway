# Remote Mac

远端 Mac 是运行 Moonlight、发起控制的电脑。它不安装 HostScreenRelay.pkg，也不需要
本仓库中的脚本。

## 安装步骤

1. 打开广州后台 <http://106.53.243.63:9185/>。
2. 在“远端 Mac”区域新增一个客户端，每台设备使用唯一名称。
3. 下载该设备的 `client.conf`。
4. 安装官方 WireGuard App，导入并启用 `client.conf`。
5. 安装官方 Moonlight，新增主机 `10.211.55.4`。
6. 完成一次 Sunshine PIN 配对，点击 `Desktop`。

远端配置只路由远控私网：

```ini
AllowedIPs = 10.88.0.0/24, 10.211.55.4/32
PersistentKeepalive = 25
```

它不包含 `0.0.0.0/0`，因此远端 Mac 的浏览器及普通网络仍走自己的本地网络。

`client.example.conf` 只用于理解格式。实际使用必须从后台生成唯一密钥，不能复制示例
或复用其他设备配置。
