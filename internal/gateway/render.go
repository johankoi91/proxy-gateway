package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) renderLocked() error {
	if err := os.MkdirAll(m.cfg.RuntimeDir, 0o700); err != nil {
		return err
	}
	files := map[string][]byte{
		"wg0.conf":     []byte(m.wireGuardServerConfigLocked()),
		"xray.json":    m.xrayServerConfigLocked(),
		"dnsmasq.conf": []byte(m.dnsmasqConfigLocked()),
	}
	for name, body := range files {
		if err := writeSecret(filepath.Join(m.cfg.RuntimeDir, name), body); err != nil {
			return err
		}
	}
	return nil
}

func writeSecret(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (m *Manager) wireGuardServerConfigLocked() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nAddress = 10.77.0.1/24, fd77:77:77::1/64\nListenPort = %d\nMTU = %d\nPrivateKey = %s\nSaveConfig = false\n", m.state.WireGuard.Port, m.state.WireGuard.MTU, m.state.WireGuard.PrivateKey)
	fmt.Fprintf(&b, "PostUp = iptables -t nat -A POSTROUTING -s 10.77.0.0/24 -o %s -j MASQUERADE; iptables -I FORWARD 1 -i wg0 -j ACCEPT; iptables -I FORWARD 1 -o wg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n", m.state.EgressIF)
	fmt.Fprintf(&b, "PostDown = iptables -t nat -D POSTROUTING -s 10.77.0.0/24 -o %s -j MASQUERADE; iptables -D FORWARD -i wg0 -j ACCEPT; iptables -D FORWARD -o wg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n", m.state.EgressIF)
	for _, client := range m.state.Clients {
		fmt.Fprintf(&b, "\n[Peer]\n# %s (%s)\nPublicKey = %s\nAllowedIPs = %s/32, %s/128\n", client.Name, client.ID, client.WireGuardPublic, client.IPv4, client.IPv6)
	}
	return b.String()
}

func (m *Manager) xrayServerConfigLocked() []byte {
	listenAddress := "0.0.0.0"
	if m.cfg.RealityListen != m.state.Reality.Port {
		listenAddress = "127.0.0.1"
	}
	clients := make([]map[string]string, 0, len(m.state.Clients))
	shortIDs := make([]string, 0, len(m.state.Clients))
	for _, client := range m.state.Clients {
		clients = append(clients, map[string]string{
			"id": client.VLESSUUID, "flow": "xtls-rprx-vision", "email": client.Name,
		})
		shortIDs = append(shortIDs, client.RealityShortID)
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": listenAddress, "port": m.cfg.RealityListen, "protocol": "vless",
			"settings": map[string]any{"clients": clients, "decryption": "none"},
			"streamSettings": map[string]any{
				"network": "tcp", "security": "reality",
				"realitySettings": map[string]any{
					"show": false, "dest": m.state.Reality.Target, "xver": 0,
					"serverNames": []string{m.state.Reality.ServerName},
					"privateKey":  m.state.Reality.PrivateKey, "shortIds": shortIDs,
				},
			},
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
		}},
		"outbounds": []any{
			map[string]any{"protocol": "freedom", "tag": "direct"},
			map[string]any{"protocol": "blackhole", "tag": "block"},
		},
	}
	b, _ := json.MarshalIndent(config, "", "  ")
	return b
}

func (m *Manager) dnsmasqConfigLocked() string {
	return "interface=wg0\nlisten-address=10.77.0.1\nbind-dynamic\nport=53\nserver=1.1.1.1\nserver=8.8.8.8\nfilter-AAAA\ndomain-needed\nbogus-priv\ncache-size=1000\n"
}

func (m *Manager) WireGuardClientConfig(client Client) string {
	state := m.State()
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32, %s/128
DNS = %s
MTU = %d

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`, client.WireGuardPrivate, client.IPv4, client.IPv6, state.WireGuard.DNS, state.WireGuard.MTU, state.WireGuard.PublicKey, state.PublicIP, state.WireGuard.Port)
}

func (m *Manager) VLESSURI(client Client) string {
	state := m.State()
	query := url.Values{}
	query.Set("type", "tcp")
	query.Set("encryption", "none")
	query.Set("security", "reality")
	query.Set("pbk", state.Reality.PublicKey)
	query.Set("fp", "chrome")
	query.Set("sni", state.Reality.ServerName)
	query.Set("sid", client.RealityShortID)
	query.Set("spx", "/")
	query.Set("flow", "xtls-rprx-vision")
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", client.VLESSUUID, state.PublicIP, state.Reality.Port, query.Encode(), url.PathEscape("VPN-"+client.Name))
}

func (m *Manager) VPSConfig(client Client) string {
	state := m.State()
	configuration := VPSConfig{
		Schema:  VPSConfigSchema,
		Version: VPSConfigVersion,
		Client: VPSConfigClient{
			ID:   client.ID,
			Name: client.Name,
		},
		Endpoint: VPSConfigEndpoint{
			Address:        state.PublicIP,
			Port:           state.Reality.Port,
			ExpectedEgress: state.PublicIP,
		},
		VLESS: VPSConfigVLESS{
			UUID:       client.VLESSUUID,
			Flow:       "xtls-rprx-vision",
			Transport:  "tcp",
			Encryption: "none",
			TLS: VPSConfigTLS{
				Enabled:         true,
				ServerName:      state.Reality.ServerName,
				UTLSFingerprint: "chrome",
				Reality: VPSConfigReality{
					Enabled:   true,
					PublicKey: state.Reality.PublicKey,
					ShortID:   client.RealityShortID,
				},
			},
		},
	}
	data, _ := json.MarshalIndent(configuration, "", "  ")
	return string(append(data, '\n'))
}

func (m *Manager) ClashConfig(client Client) string {
	state := m.State()
	var b bytes.Buffer
	fmt.Fprintf(&b, `mixed-port: 7897
allow-lan: false
mode: global
log-level: info
ipv6: false
unified-delay: true
tcp-concurrent: true
profile:
  store-selected: true
  store-fake-ip: true
tun:
  enable: true
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  dns-hijack:
    - any:53
dns:
  enable: true
  ipv6: false
  enhanced-mode: fake-ip
  listen: 127.0.0.1:1053
  default-nameserver:
    - 1.1.1.1
  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query
proxies:
  - name: VPN-%s-Reality
    type: vless
    server: %s
    port: %d
    uuid: %s
    network: tcp
    tls: true
    udp: true
    flow: xtls-rprx-vision
    servername: %s
    client-fingerprint: chrome
    reality-opts:
      public-key: %s
      short-id: %s
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - VPN-%s-Reality
rules:
  - MATCH,PROXY
`, safeYAML(client.Name), state.PublicIP, state.Reality.Port, client.VLESSUUID, state.Reality.ServerName, state.Reality.PublicKey, client.RealityShortID, safeYAML(client.Name))
	return b.String()
}

func safeYAML(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.ReplaceAll(value, "#", "-")
	return value
}
