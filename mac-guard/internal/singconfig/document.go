package singconfig

import "fmt"

func VLESSOutbound(document map[string]any) (map[string]any, error) {
	outbounds, ok := document["outbounds"].([]any)
	if !ok {
		return nil, fmt.Errorf("sing-box outbounds are missing")
	}
	for _, rawOutbound := range outbounds {
		if outbound, ok := rawOutbound.(map[string]any); ok && String(outbound["type"]) == "vless" {
			return outbound, nil
		}
	}
	return nil, fmt.Errorf("sing-box VLESS outbound is missing")
}

func EnforceGlobalTokyo(document map[string]any) error {
	if dns, ok := document["dns"].(map[string]any); ok {
		if servers, ok := dns["servers"].([]any); ok {
			for index, rawServer := range servers {
				if server, ok := rawServer.(map[string]any); ok {
					server["detour"] = "tokyo"
					if index == 0 {
						server["tag"] = "dns-tunnel"
					}
				}
			}
			if len(servers) > 0 {
				dns["final"] = "dns-tunnel"
			}
		}
	}
	outbound, err := VLESSOutbound(document)
	if err != nil {
		return err
	}
	outbound["tag"] = "tokyo"
	document["outbounds"] = []any{outbound}
	route, ok := document["route"].(map[string]any)
	if !ok {
		return fmt.Errorf("sing-box route is missing")
	}
	route["final"] = "tokyo"
	route["rules"] = []any{
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{"inbound": []any{"tun-in"}, "action": "sniff"},
	}
	return nil
}

func Object(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := make(map[string]any)
	parent[key] = value
	return value
}

func String(value any) string {
	if result, ok := value.(string); ok {
		return result
	}
	return ""
}

func Int(value any) int {
	switch result := value.(type) {
	case float64:
		return int(result)
	case int:
		return result
	default:
		return 0
	}
}
