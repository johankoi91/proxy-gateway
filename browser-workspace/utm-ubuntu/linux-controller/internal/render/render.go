package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"lightnode-linux-controller/internal/config"
)

func SingBox(v config.VPSConfig, c config.Controller) ([]byte, error) {
	document := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{map[string]any{
				"type": "https", "tag": "dns-tunnel", "server": "104.16.249.249",
				"server_port": 443, "path": "/dns-query", "detour": "tokyo",
				"tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"},
			}},
			"strategy": "ipv4_only", "final": "dns-tunnel",
		},
		"inbounds": []any{map[string]any{
			"type": "tun", "tag": "tun-in", "interface_name": c.TunnelInterface,
			"address": []string{c.TunnelAddress}, "mtu": 1280, "dns_mode": "hijack",
			"auto_route": true, "strict_route": true, "auto_redirect": true,
			"route_exclude_address": []string{c.ManagementCIDR, c.VPSAddress + "/32"},
		}},
		"outbounds": []any{map[string]any{
			"type": "vless", "tag": "tokyo", "server": v.Endpoint.Address,
			"server_port": v.Endpoint.Port, "uuid": v.VLESS.UUID, "flow": v.VLESS.Flow,
			"tls": map[string]any{
				"enabled": true, "server_name": v.VLESS.TLS.ServerName,
				"reality": map[string]any{"enabled": true, "public_key": v.VLESS.TLS.Reality.PublicKey, "short_id": v.VLESS.TLS.Reality.ShortID},
				"utls":    map[string]any{"enabled": true, "fingerprint": v.VLESS.TLS.UTLSFingerprint},
			},
		}},
		"route": map[string]any{
			"auto_detect_interface": true,
			"rules": []any{
				map[string]any{"protocol": "dns", "action": "hijack-dns"},
				map[string]any{"inbound": []string{"tun-in"}, "action": "sniff"},
			},
			"final": "tokyo",
		},
	}
	return json.MarshalIndent(document, "", "  ")
}

func Controller(c config.Controller) ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

func Firewall(c config.Controller) []byte {
	return []byte(fmt.Sprintf(`flush table inet lightnode_controller
table inet lightnode_controller {
  chain input {
    type filter hook input priority 10; policy accept;
    ct state invalid drop
  }

  chain output {
    type filter hook output priority 10; policy drop;
    ct state invalid drop
    ct state established,related accept
    ct status dnat accept
    oifname "lo" accept
    oifname %q udp sport 68 udp dport 67 accept
    oifname %q ip daddr %s accept
    oifname %q ip daddr %s tcp dport %d accept
    oifname %q accept
  }

  chain forward {
    type filter hook forward priority 10; policy drop;
    ct state invalid drop
    ct state established,related accept
    iifname %q ip saddr %s tcp dport %s accept
    oifname %q accept
  }
}
`, c.PhysicalInterface, c.PhysicalInterface, c.ManagementCIDR,
		c.PhysicalInterface, c.VPSAddress, c.VPSPort, c.TunnelInterface,
		c.PhysicalInterface, c.ManagementCIDR, nftPorts(without(c.ManagementTCPPorts, 22)),
		c.TunnelInterface))
}

func nftPorts(values []int) string {
	values = append([]int(nil), values...)
	sort.Ints(values)
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func without(values []int, excluded int) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value != excluded {
			result = append(result, value)
		}
	}
	return result
}

func RedactedSingBox(document []byte) []byte {
	var value map[string]any
	if json.Unmarshal(document, &value) != nil {
		return nil
	}
	outbounds, _ := value["outbounds"].([]any)
	for _, raw := range outbounds {
		outbound, _ := raw.(map[string]any)
		if outbound["type"] == "vless" {
			outbound["uuid"] = "<redacted>"
			tls, _ := outbound["tls"].(map[string]any)
			reality, _ := tls["reality"].(map[string]any)
			reality["public_key"] = "<redacted>"
			reality["short_id"] = "<redacted>"
		}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
	return buffer.Bytes()
}
