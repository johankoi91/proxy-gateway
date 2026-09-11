# Remote Mac

远端 Mac 只安装官方 WireGuard，并用浏览器访问 KasmVNC。

1. 打开广州后台 <http://106.53.243.63:9185/>。
2. 为每台远端 Mac 新建唯一客户端。
3. 下载并导入该设备的 `client.conf`。
4. 启用 WireGuard。
5. 浏览器打开 <https://10.88.0.254:8443/>。
6. 首次访问接受自签名证书提示，再登录 KasmVNC。

配置只路由远程工作区私网：

```ini
AllowedIPs = 10.88.0.0/24
PersistentKeepalive = 25
```

官方 WireGuard App：

```text
Mac App Store ID: 1451685025
Bundle ID: com.wireguard.macos
```

实际配置必须从后台生成，不得复制示例私钥或复用其他设备配置。
