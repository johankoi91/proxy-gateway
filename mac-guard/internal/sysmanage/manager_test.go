package sysmanage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestNetworkParsers(t *testing.T) {
	nwi := "Network interfaces: utun4 en0\n"
	if actual := firstInterface(nwi); actual != "en0" {
		t.Fatalf("physical interface = %q", actual)
	}
	services := `(1) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)
(2) Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)
`
	if actual := networkServiceForInterface(services, "en0"); actual != "Wi-Fi" {
		t.Fatalf("network service = %q", actual)
	}
	route := "   gateway: 172.19.0.1\n interface: utun8\n"
	if actual := routeInterface(route); actual != "utun8" {
		t.Fatalf("route interface = %q", actual)
	}
}

func TestExpectedEgressRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("149.104.78.121\n"))
	}))
	defer server.Close()
	manager := New(nil)
	manager.HTTPClient = server.Client()
	actual, err := manager.probeExpectedEgress(context.Background(), server.URL, "149.104.78.121", 2)
	if err != nil || actual != "149.104.78.121" || calls.Load() != 2 {
		t.Fatalf("egress=%q calls=%d err=%v", actual, calls.Load(), err)
	}
}

func TestTunnelAndCIDRParsing(t *testing.T) {
	interfaces := `en0: flags=8863<UP>
    inet 10.211.55.4 netmask 0xffffff00 broadcast 10.211.55.255
utun8: flags=8051<UP>
    inet 172.19.0.1 --> 172.19.0.1 netmask 0xffffffff
`
	if actual := tunnelInterfaceForAddress(interfaces, "172.19.0.1"); actual != "utun8" {
		t.Fatalf("tunnel interface = %q", actual)
	}
	if actual, err := managementCIDR("10.211.55.4", "0xffffff00"); err != nil || actual != "10.211.55.0/24" {
		t.Fatalf("management CIDR = %q, %v", actual, err)
	}
}

func TestRemoveManagedPFBlock(t *testing.T) {
	input := "set skip on lo0\n" + managedBegin + "\nblock drop out all\n" + managedEnd + "\npass all\n"
	if actual := removeManagedPFBlock(input); actual != "set skip on lo0\npass all\n" {
		t.Fatalf("cleaned PF config = %q", actual)
	}
}

func TestConfigCommandMigration(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	data := []byte(`{"version":1,"vpsAddress":"149.104.78.121","vpsPort":443,"expectedEgressIp":"149.104.78.121","egressProbeUrl":"https://api.ipify.org","dnsProbeHost":"claude.ai","custom":"preserved"}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := New(nil)
	manager.Paths.Config = configPath
	manager.Paths.GuardBinary = "/test/lightnode-guard"
	if err := manager.migrateConfigCommands(); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(updated, &document); err != nil {
		t.Fatal(err)
	}
	if document["custom"] != "preserved" {
		t.Fatal("custom configuration was not preserved")
	}
	actual := document["pauseCommand"]
	expected := []any{"/test/lightnode-guard", "tunnel", "pause"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("pause command = %#v", actual)
	}
}

func TestCommandRecognition(t *testing.T) {
	for _, value := range []string{"tunnel", "firewall", "dns", "conflicts", "notify", "install", "uninstall"} {
		if !IsCommand(value) {
			t.Fatalf("command %q not recognized", value)
		}
	}
	if IsCommand("daemon") {
		t.Fatal("daemon flags must stay on the default code path")
	}
}
