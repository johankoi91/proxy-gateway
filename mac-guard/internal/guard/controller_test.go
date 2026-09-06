package guard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lightnode-mac-guard/internal/config"
	"lightnode-mac-guard/internal/model"
)

func TestAllHealthy(t *testing.T) {
	components := map[string]model.Component{
		"provider": {Health: model.HealthHealthy},
		"tunnel":   {Health: model.HealthHealthy},
		"egress":   {Health: model.HealthHealthy},
	}
	if !allHealthy(components, "provider", "tunnel", "egress") {
		t.Fatal("expected all components to be healthy")
	}
	components["egress"] = model.Component{Health: model.HealthFailed}
	if allHealthy(components, "provider", "tunnel", "egress") {
		t.Fatal("failed component must make aggregate unhealthy")
	}
}

func TestRuntimeConfigurationIsSanitized(t *testing.T) {
	directory := t.TempDir()
	singBoxConfig := filepath.Join(directory, "sing-box.json")
	singBoxJSON := `{
  "dns":{"strategy":"ipv4_only","final":"dns-ai","servers":[{"type":"https","tag":"dns-ai","server":"104.16.249.249","server_port":443,"path":"/dns-query","detour":"tokyo","tls":{"server_name":"cloudflare-dns.com"}}]},
  "inbounds":[{"type":"tun","tag":"tun-in","address":["172.19.0.1/30"],"mtu":1280,"dns_mode":"hijack","auto_route":true,"strict_route":true}],
  "outbounds":[{"type":"vless","tag":"tokyo","server":"149.104.78.121","server_port":443,"uuid":"private-uuid-value","flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"www.cloudflare.com","reality":{"public_key":"private-public-key","short_id":"private-short-id"},"utls":{"enabled":true,"fingerprint":"chrome"}}}]
}`
	if err := os.WriteFile(singBoxConfig, []byte(singBoxJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.SingBoxConfigFile = singBoxConfig
	cfg.AdminPassword = "dashboard-secret-value"
	cfg.V2ClashAPISecret = "observer-secret-value"
	controller := NewController(cfg, ipv6Runner{}, "test")
	payload, err := json.Marshal(controller.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, secret := range []string{cfg.AdminPassword, cfg.V2ClashAPISecret, "private-uuid-value", "private-public-key", "private-short-id"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("runtime snapshot leaked secret %q", secret)
		}
	}
	if controller.Snapshot().RuntimeConfig.VPSAddress != cfg.VPSAddress {
		t.Fatal("sanitized runtime configuration is missing the VPS address")
	}
	if len(controller.Snapshot().RuntimeConfig.Components) != 6 || !strings.Contains(encoded, "172.19.0.1/30") || !strings.Contains(encoded, "xtls-rprx-vision") {
		t.Fatal("sanitized runtime component configuration is incomplete")
	}
}

func TestLaunchdPlistParsing(t *testing.T) {
	plist := []byte(`<?xml version="1.0"?><plist><dict><key>Label</key><string>com.example.test</string><key>ProgramArguments</key><array><string>/usr/local/bin/example</string><string>-config</string><string>/Path With Spaces/config.json</string></array></dict></plist>`)
	if actual := plistString(plist, "Label", "fallback"); actual != "com.example.test" {
		t.Fatalf("label = %q", actual)
	}
	want := []string{"/usr/local/bin/example", "-config", "/Path With Spaces/config.json"}
	actual := plistStringArray(plist, "ProgramArguments")
	if strings.Join(actual, "|") != strings.Join(want, "|") {
		t.Fatalf("arguments = %#v", actual)
	}
}

func TestMergeHealthUsesWorstState(t *testing.T) {
	merged := mergeHealth(
		model.Component{Health: model.HealthHealthy, Summary: "DNS 正常"},
		model.Component{Health: model.HealthFailed, Summary: "IPv6 泄漏"},
	)
	if merged.Health != model.HealthFailed {
		t.Fatalf("health = %s", merged.Health)
	}
}

func TestBuildStagesFollowArchitectureDataPath(t *testing.T) {
	components := map[string]model.Component{
		"network_conflict": {Health: model.HealthHealthy}, "firewall": {Health: model.HealthHealthy},
		"tunnel": {Health: model.HealthHealthy}, "provider": {Health: model.HealthHealthy},
		"dns": {Health: model.HealthHealthy}, "ipv6": {Health: model.HealthHealthy},
		"transport": {Health: model.HealthHealthy}, "egress": {Health: model.HealthHealthy},
	}
	stages := buildStages(components)
	want := []string{"network", "firewall", "tunnel", "provider", "dns", "vps", "egress"}
	if len(stages) != len(want) {
		t.Fatalf("stage count = %d", len(stages))
	}
	for index, expected := range want {
		if stages[index].ID != expected {
			t.Fatalf("stage %d = %q, want %q", index, stages[index].ID, expected)
		}
	}
}

func TestFindTunnel(t *testing.T) {
	output := "utun7: flags=8051<UP> mtu 9000\n\tinet 172.19.0.1 --> 172.19.0.1 netmask 0xfffffffc\n"
	if name, ok := findTunnel(output, "utun", "172.19.0.1"); !ok || name != "utun7" {
		t.Fatalf("name=%q ok=%t", name, ok)
	}
}

func TestPhysicalInterfaceFromNWI(t *testing.T) {
	output := "Network information\n\nIPv4 network interface information\n     en0 : flags : 0x5 (IPv4,DNS)\n\nNetwork interfaces: en0\n"
	if actual := physicalInterfaceFromNWI(output); actual != "en0" {
		t.Fatalf("interface = %q", actual)
	}
}

func TestEnabledSystemProxies(t *testing.T) {
	if !enabledSystemProxies("<dictionary> {\n  HTTPEnable : 1\n  HTTPSEnable : 0\n}") {
		t.Fatal("expected enabled HTTP proxy")
	}
	if !enabledSystemProxies("<dictionary> {\n  ProxyAutoConfigEnable : 1\n}") {
		t.Fatal("expected enabled PAC proxy")
	}
	if enabledSystemProxies("<dictionary> {\n  HTTPEnable : 0\n  HTTPSEnable : 0\n  SOCKSEnable : 0\n}") {
		t.Fatal("disabled proxies must not be reported")
	}
}

func TestConnectedVPNNames(t *testing.T) {
	output := `Available network connection services in the current set (*=enabled):
* (Disconnected) ABC PPP --> L2TP "Work" [VPN:L2TP]
* (Connected) DEF PPP --> IPSEC "Other VPN" [VPN:IPSec]
`
	names := connectedVPNNames(output)
	if len(names) != 1 || names[0] != "Other VPN" {
		t.Fatalf("names = %#v", names)
	}
}

func TestRouteInterface(t *testing.T) {
	if actual := routeInterface("route to: 8.8.8.8\ninterface: utun4\n"); actual != "utun4" {
		t.Fatalf("interface = %q", actual)
	}
}

func TestDefaultDNSUsesNonSupplementalResolver(t *testing.T) {
	output := `DNS configuration

resolver #1
  nameserver[0] : 10.251.1.1
  flags : Supplemental, Request A records

resolver #2
  nameserver[0] : 172.19.0.2
  flags : Request A records

DNS configuration (for scoped queries)
resolver #1
  nameserver[0] : 192.168.0.1
  flags : Scoped, Request A records
`
	if !defaultDNSUses(output, []string{"172.19.0.2"}) {
		t.Fatal("expected non-supplemental tunnel DNS")
	}
	if defaultDNSUses(output, []string{"10.251.1.1"}) {
		t.Fatal("supplemental DNS must not satisfy the default DNS check")
	}
	if defaultDNSUses(output, []string{"192.168.0.1"}) {
		t.Fatal("scoped-only DNS must not satisfy the default DNS check")
	}
}

type ipv6Runner struct {
	output string
	err    error
}

type outputRunner struct {
	output string
}

func (r outputRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte(r.output), nil
}

type networkRunner struct {
	proxyOutput string
	routeIF     string
}

func (r networkRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case name == "scutil" && len(args) == 1 && args[0] == "--proxy":
		return []byte(r.proxyOutput), nil
	case name == "scutil" && len(args) == 2 && args[0] == "--nc" && args[1] == "list":
		return []byte("Available network connection services\n"), nil
	case name == "pgrep":
		return nil, errors.New("not found")
	case name == "ifconfig":
		return []byte("utun7: flags=8051<UP> mtu 1280\n\tinet 172.19.0.1 --> 172.19.0.1 netmask 0xfffffffc\n"), nil
	case name == "route":
		return []byte("route to: 8.8.8.8\ninterface: " + r.routeIF + "\n"), nil
	default:
		return nil, errors.New("unexpected command")
	}
}

func TestNetworkConflictCheck(t *testing.T) {
	cfg := config.Default()
	now := time.Now().UTC()
	healthy := newChecker(cfg, networkRunner{
		proxyOutput: "HTTPEnable : 0\nHTTPSEnable : 0\nSOCKSEnable : 0\n",
		routeIF:     "utun7",
	}).checkNetworkConflict(context.Background(), now)
	if healthy.Health != model.HealthHealthy {
		t.Fatalf("healthy check = %#v", healthy)
	}

	conflicted := newChecker(cfg, networkRunner{
		proxyOutput: "HTTPEnable : 1\n",
		routeIF:     "utun9",
	}).checkNetworkConflict(context.Background(), now)
	if conflicted.Health != model.HealthFailed {
		t.Fatalf("conflicted check = %#v", conflicted)
	}
}

func (r ipv6Runner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name == "curl" {
		return []byte(r.output), r.err
	}
	return nil, errors.New("unexpected command")
}

func TestIPv6ProbeOnlyTreatsRealIPv6AsLeak(t *testing.T) {
	cfg := config.Default()
	for _, tc := range []struct {
		name   string
		output string
		err    error
		health model.Health
	}{
		{name: "native IPv6", output: "2001:db8::1\n", health: model.HealthFailed},
		{name: "captured as IPv4", output: "149.104.78.121\n", health: model.HealthHealthy},
		{name: "IPv6 unavailable", err: errors.New("unreachable"), health: model.HealthHealthy},
		{name: "invalid response", output: "not-an-ip", health: model.HealthDegraded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := newChecker(cfg, ipv6Runner{output: tc.output, err: tc.err})
			component, _ := check.checkIPv6(context.Background(), time.Now().UTC())
			if component.Health != tc.health {
				t.Fatalf("health = %s, want %s", component.Health, tc.health)
			}
		})
	}
}

func TestTunnelFailureWithActiveFirewallIsDegradedLocked(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := NewController(cfg, ipv6Runner{}, "test")
	controller.apply(checkOutput{components: []model.Component{
		{ID: "firewall", Name: "PF 实时阻断", Health: model.HealthHealthy, Required: true},
		{ID: "tunnel", Name: "TUN", Health: model.HealthFailed, Required: true},
	}})
	snapshot := controller.Snapshot()
	if !snapshot.Protected || snapshot.State != model.StateDegradedLocked {
		t.Fatalf("protected=%t state=%s", snapshot.Protected, snapshot.State)
	}
}

func TestMissingFirewallIsUnprotected(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := NewController(cfg, ipv6Runner{}, "test")
	controller.apply(checkOutput{components: []model.Component{
		{ID: "firewall", Name: "PF 实时阻断", Health: model.HealthFailed, Required: true},
	}})
	snapshot := controller.Snapshot()
	if snapshot.Protected || snapshot.State != model.StateUnprotected {
		t.Fatalf("protected=%t state=%s", snapshot.Protected, snapshot.State)
	}
}

func TestLegacyClassificationEventsAreHidden(t *testing.T) {
	directory := t.TempDir()
	eventLog := filepath.Join(directory, "events.jsonl")
	content := `{"id":41,"time":"2026-09-05T10:00:00Z","severity":"warning","component":"policy_v2","message":"发现待分类连接: old -> example.com:443"}
{"id":42,"time":"2026-09-05T10:01:00Z","severity":"info","component":"guard","message":"首次组件检查完成"}
`
	if err := os.WriteFile(eventLog, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.EventLogFile = eventLog
	controller := NewController(cfg, ipv6Runner{}, "test")
	snapshot := controller.Snapshot()
	if len(snapshot.Events) != 1 || snapshot.Events[0].ID != 42 {
		t.Fatalf("events = %#v", snapshot.Events)
	}
}

func TestSafelyPausedStateKeepsFirewallProtection(t *testing.T) {
	directory := t.TempDir()
	maintenanceFile := filepath.Join(directory, "safely-paused")
	if err := os.WriteFile(maintenanceFile, []byte("paused\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.MaintenanceFile = maintenanceFile
	controller := NewController(cfg, ipv6Runner{}, "test")
	controller.apply(checkOutput{components: []model.Component{
		{ID: "firewall", Name: "PF 实时阻断", Health: model.HealthHealthy, Required: true},
		{ID: "provider", Name: "sing-box", Health: model.HealthFailed, Required: true},
	}})
	snapshot := controller.Snapshot()
	if !snapshot.Protected || snapshot.Connected || snapshot.State != model.StateMaintenance {
		t.Fatalf("protected=%t connected=%t state=%s", snapshot.Protected, snapshot.Connected, snapshot.State)
	}
	if len(snapshot.Alerts) != 1 || snapshot.Alerts[0].ID != "safely_paused" {
		t.Fatalf("alerts = %#v", snapshot.Alerts)
	}
}

func TestLifecycleWarningIsRecorded(t *testing.T) {
	directory := t.TempDir()
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.MaintenanceFile = ""
	controller := NewController(cfg, outputRunner{output: "INFO: ChatGPT exited normally.\nWARNING: Claude is still running.\n"}, "test")
	if err := controller.runTunnelAction(context.Background(), []string{"test-action"}, "started", "done", "failed"); err != nil {
		t.Fatal(err)
	}
	foundWarning := false
	foundInfo := false
	for _, event := range controller.Snapshot().Events {
		if event.Severity == "warning" && event.Message == "Claude is still running." {
			foundWarning = true
		}
		if event.Severity == "info" && event.Message == "ChatGPT exited normally." {
			foundInfo = true
		}
	}
	if !foundWarning || !foundInfo {
		t.Fatalf("lifecycle output was not recorded: warning=%t info=%t", foundWarning, foundInfo)
	}
}
