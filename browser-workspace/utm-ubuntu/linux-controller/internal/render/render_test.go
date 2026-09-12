package render

import (
	"encoding/json"
	"strings"
	"testing"

	"lightnode-linux-controller/internal/config"
)

func fixture() (config.VPSConfig, config.Controller) {
	vps := config.VPSConfig{
		Schema: config.Schema, Version: 1,
		Endpoint: config.VPSEndpoint{Address: "149.104.78.121", Port: 443, ExpectedEgress: "149.104.78.121"},
		VLESS: config.VLESS{
			UUID: "7f9527a5-5967-449d-aa1f-7c5960eb35a1", Flow: "xtls-rprx-vision", Transport: "tcp", Encryption: "none",
			TLS: config.VLESSTLS{Enabled: true, ServerName: "www.cloudflare.com", UTLSFingerprint: "chrome", Reality: config.VLESSReality{Enabled: true, PublicKey: "VEnbXRyOqO98TYo4khJlxFSy0oPzTJsuepq2BNs4oiY", ShortID: "0123456789abcdef"}},
		},
	}
	return vps, config.DefaultController(vps)
}

func TestSingBoxHasOnlyTokyoOutbound(t *testing.T) {
	vps, controller := fixture()
	data, err := SingBox(vps, controller)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	outbounds := document["outbounds"].([]any)
	if len(outbounds) != 1 || outbounds[0].(map[string]any)["tag"] != "tokyo" {
		t.Fatalf("unexpected outbounds: %#v", outbounds)
	}
	if strings.Contains(string(data), `"type": "direct"`) {
		t.Fatal("direct outbound must not exist")
	}
	for _, required := range []string{`"auto_redirect": true`, `"strict_route": true`, `"detour": "tokyo"`, `"10.211.55.0/24"`, `"149.104.78.121/32"`} {
		if !strings.Contains(string(data), required) {
			t.Fatalf("missing %s", required)
		}
	}
}

func TestFirewallIsFailClosedAndKeepsManagement(t *testing.T) {
	_, controller := fixture()
	data := string(Firewall(controller))
	for _, required := range []string{
		"hook output priority 10; policy drop", "hook forward priority 10; policy drop",
		"hook input priority 10; policy accept",
		"149.104.78.121 tcp dport 443 accept", "10.211.55.0/24 tcp dport { 8443, 8444 } accept",
		`ct status dnat accept`, `oifname "lightnode-tun" accept`,
	} {
		if !strings.Contains(data, required) {
			t.Fatalf("missing firewall invariant %q", required)
		}
	}
}

func TestRedactionRemovesCredentials(t *testing.T) {
	vps, controller := fixture()
	data, _ := SingBox(vps, controller)
	redacted := string(RedactedSingBox(data))
	for _, secret := range []string{vps.VLESS.UUID, vps.VLESS.TLS.Reality.PublicKey, vps.VLESS.TLS.Reality.ShortID} {
		if strings.Contains(redacted, secret) {
			t.Fatal("credential was not redacted")
		}
	}
}
