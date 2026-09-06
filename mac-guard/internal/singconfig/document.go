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
			for _, rawServer := range servers {
				if server, ok := rawServer.(map[string]any); ok {
					server["detour"] = "tokyo"
				}
			}
		}
	}
	outbound, err := VLESSOutbound(document)
	if err != nil {
		return err
	}
	outbound["tag"] = "tokyo"
	route, ok := document["route"].(map[string]any)
	if !ok {
		return fmt.Errorf("sing-box route is missing")
	}
	route["final"] = "tokyo"
	if rules, ok := route["rules"].([]any); ok {
		filtered := make([]any, 0, len(rules))
		for _, rawRule := range rules {
			rule, ok := rawRule.(map[string]any)
			if !ok {
				filtered = append(filtered, rawRule)
				continue
			}
			if _, processRule := rule["process_path_regex"]; processRule {
				continue
			}
			if _, domainRule := rule["domain_suffix"]; domainRule {
				continue
			}
			filtered = append(filtered, rule)
		}
		route["rules"] = filtered
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
