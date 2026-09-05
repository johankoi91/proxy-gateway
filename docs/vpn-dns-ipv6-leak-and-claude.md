是的，如果 DNS 或 IPv6 配置不完整，确实可能出现“看似连着 VPN，但部分真实连接没有从 VPS 出口”的情况。不过要分清两个问题。

```text
DNS 查询泄漏
  ≠
业务请求泄漏
```

DNS 泄漏只是本地运营商看到了你查询 `claude.ai`。Claude 通常只能看到业务请求的源 IP，不会因为 DNS 在本地解析，就自动看到你的本地公网 IP。

真正危险的是 IPv6 旁路：

1. DNS 返回 Claude 的 `AAAA` 地址。
2. VPN 只接管 `0.0.0.0/0`，没有接管 `::/0`。
3. 系统通过手机或宽带原生 IPv6 直连 Claude。
4. Claude 此时看到的就是你的真实 IPv6，而不是 VPS 的 `149.104.78.121`。

你当前生成的 WireGuard 配置是：

```ini
DNS = 10.77.0.1
AllowedIPs = 0.0.0.0/0, ::/0
```

服务器的 `dnsmasq` 还启用了：

```ini
filter-AAAA
```

因此当前设计是三层防护：

- IPv4 默认路由进入 WireGuard。
- IPv6 默认路由也进入 WireGuard。
- 隧道 DNS 不向客户端返回 IPv6 地址。

正常情况下，Claude、GPT、YouTube 的请求都应该从 VPS 出口 `149.104.78.121` 发出。即使某个 App 自带 DoH，它访问 DoH 服务器的连接本身也会被全局 WireGuard 接管。

仍然可能产生旁路的情况包括：

- Android WireGuard 将 Claude 排除在隧道之外。
- 配置被改成只包含 `0.0.0.0/0`。
- 同时启用了另一个 VPN、代理或“智能分流”。
- WireGuard 断开后系统自动回落到手机网络。
- App 使用 VPN 不接管的特殊系统通道。
- Android 厂商的双通道加速、网络加速功能同时使用 Wi-Fi 和移动网络。

另外，出口 IP 一致也不代表 Claude 只能识别到这一项。它还可能综合判断：

- VPS 属于数据中心 ASN，而不是住宅网络。
- IP 所在国家与账号历史记录不一致。
- 时区、语言、系统地区和 IP 地理位置不一致。
- 频繁更换设备、IP 或国家。
- GPS、支付地区、手机号和账号注册地区。
- Cookie、设备标识和历史登录行为。

所以你的配置已经能解决 DNS/IPv6 泄漏问题，但不能保证 Claude 将 VPS IP 当作普通东京住宅用户。稳定使用同一个 VPS IP、避免国家频繁跳变，比单纯处理 DNS 更重要。
