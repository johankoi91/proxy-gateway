package guard

import (
	"encoding/json"
	"strings"
	"testing"

	"lightnode-mac-guard/internal/model"
)

func TestBuildNodeCandidatesPreservesSecretsAndUpdatesEndpoint(t *testing.T) {
	guardData := []byte(`{"version":1,"vpsAddress":"149.104.78.121","vpsPort":443,"expectedEgressIp":"149.104.78.121","custom":"keep"}`)
	singData := []byte(`{
  "dns":{"servers":[{"type":"https","detour":"tokyo"}]},
  "outbounds":[{"type":"vless","tag":"tokyo","server":"149.104.78.121","server_port":443,"uuid":"keep-uuid","flow":"xtls-rprx-vision","tls":{"server_name":"old.example.com","reality":{"public_key":"keep-public-key","short_id":"aabbccdd"},"utls":{"fingerprint":"chrome"}}}],
  "route":{"rules":[{"protocol":"dns","action":"hijack-dns"}],"final":"tokyo"},
  "experimental":{"clash_api":{"secret":"keep-api-secret"}}
}`)
	update := model.NodeUpdate{
		Server: "203.0.113.20", Port: 8443, ExpectedEgress: "203.0.113.21",
		Flow: "xtls-rprx-vision", TLSServerName: "new.example.com", UTLSFingerprint: "safari",
	}
	newGuard, newSing, err := buildNodeCandidates(guardData, singData, update)
	if err != nil {
		t.Fatal(err)
	}
	text := string(newSing)
	for _, expected := range []string{"keep-uuid", "keep-public-key", "aabbccdd", "keep-api-secret", "203.0.113.20", "new.example.com", "safari", `"tag": "tokyo"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("candidate lost %q: %s", expected, text)
		}
	}
	var guard map[string]any
	if err := json.Unmarshal(newGuard, &guard); err != nil {
		t.Fatal(err)
	}
	if guard["custom"] != "keep" || guard["vpsAddress"] != "203.0.113.20" || guard["expectedEgressIp"] != "203.0.113.21" {
		t.Fatalf("guard candidate = %#v", guard)
	}
	view, err := nodeConfigurationFromDocuments(newGuard, newSing)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(view)
	for _, secret := range []string{"keep-uuid", "keep-public-key", "aabbccdd", "keep-api-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("node response leaked %q", secret)
		}
	}
	if !view.UUIDConfigured || !view.RealityPublicKeyConfigured || !view.RealityShortIDConfigured {
		t.Fatalf("configured flags = %#v", view)
	}
}

func TestValidateNodeUpdate(t *testing.T) {
	valid := model.NodeUpdate{
		Server: "149.104.78.121", Port: 443, ExpectedEgress: "149.104.78.121",
		Flow: "xtls-rprx-vision", TLSServerName: "www.cloudflare.com", UTLSFingerprint: "chrome",
	}
	if err := validateNodeUpdate(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Server = "not-an-ip"
	if err := validateNodeUpdate(invalid); err == nil {
		t.Fatal("invalid server must be rejected")
	}
	invalid = valid
	invalid.RealityShortID = "abc"
	if err := validateNodeUpdate(invalid); err == nil {
		t.Fatal("odd-length short ID must be rejected")
	}
}
