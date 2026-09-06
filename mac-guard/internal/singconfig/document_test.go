package singconfig

import "testing"

func TestEnforceGlobalTokyoRemovesAlternativeRoutesAndOutbounds(t *testing.T) {
	tokyo := map[string]any{"type": "vless", "tag": "old-vless"}
	document := map[string]any{
		"dns": map[string]any{
			"final":   "old-dns",
			"servers": []any{map[string]any{"tag": "old-dns", "detour": "direct"}},
		},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			tokyo,
			map[string]any{"type": "selector", "tag": "selector"},
		},
		"route": map[string]any{
			"final": "direct",
			"rules": []any{
				map[string]any{"domain_suffix": []any{"example.cn"}, "outbound": "direct"},
				map[string]any{"process_path_regex": []any{"Browser"}, "outbound": "selector"},
			},
		},
	}

	if err := EnforceGlobalTokyo(document); err != nil {
		t.Fatal(err)
	}
	outbounds := document["outbounds"].([]any)
	if len(outbounds) != 1 || outbounds[0].(map[string]any)["tag"] != "tokyo" {
		t.Fatalf("outbounds were not reduced to Tokyo: %#v", outbounds)
	}
	route := document["route"].(map[string]any)
	if route["final"] != "tokyo" || len(route["rules"].([]any)) != 2 {
		t.Fatalf("route was not canonicalized: %#v", route)
	}
	dns := document["dns"].(map[string]any)
	server := dns["servers"].([]any)[0].(map[string]any)
	if dns["final"] != "dns-tunnel" || server["tag"] != "dns-tunnel" || server["detour"] != "tokyo" {
		t.Fatalf("DNS was not forced through Tokyo: %#v", dns)
	}
}
