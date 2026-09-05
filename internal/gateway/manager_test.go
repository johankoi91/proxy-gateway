package gateway

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func testManager(t *testing.T) (*Manager, *Store, Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.RuntimeDir = filepath.Join(dir, "run")
	cfg.PublicIP = "203.0.113.10"
	cfg.EgressInterface = "eth0"
	cfg.RealityListen = 8443
	cfg.DefaultClients = []string{"mac", "android"}
	cfg.DryRun = true
	store := NewStore(filepath.Join(dir, "data", "state.json"))
	manager := NewManager(store, ExecRunner{}, cfg)
	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	return manager, store, cfg
}

func TestBootstrapAndRender(t *testing.T) {
	manager, store, cfg := testManager(t)
	state := manager.State()
	if state.PublicIP != "203.0.113.10" || len(state.Clients) != 2 {
		t.Fatalf("unexpected state: %#v", state)
	}
	if info, err := os.Stat(store.Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode: info=%v err=%v", info, err)
	}
	wg, err := os.ReadFile(filepath.Join(cfg.RuntimeDir, "wg0.conf"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(wg)
	for _, expected := range []string{"ListenPort = 443", "MTU = 1280", "10.77.0.2/32", "10.77.0.3/32", "iptables -t nat"} {
		if !strings.Contains(text, expected) {
			t.Errorf("WireGuard config missing %q", expected)
		}
	}
	clientConfig := manager.WireGuardClientConfig(state.Clients[0])
	for _, expected := range []string{"DNS = 10.77.0.1", "Endpoint = 203.0.113.10:443", "AllowedIPs = 0.0.0.0/0, ::/0"} {
		if !strings.Contains(clientConfig, expected) {
			t.Errorf("client config missing %q", expected)
		}
	}
	xray, err := os.ReadFile(filepath.Join(cfg.RuntimeDir, "xray.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"listen": "127.0.0.1"`, `"port": 8443`} {
		if !strings.Contains(string(xray), expected) {
			t.Errorf("Xray config missing %q", expected)
		}
	}
}

func TestGeneratedKeyFormats(t *testing.T) {
	manager, _, _ := testManager(t)
	state := manager.State()
	if key, err := base64.StdEncoding.DecodeString(state.WireGuard.PrivateKey); err != nil || len(key) != 32 {
		t.Fatalf("invalid WireGuard private key: length=%d err=%v", len(key), err)
	}
	if key, err := base64.RawURLEncoding.DecodeString(state.Reality.PrivateKey); err != nil || len(key) != 32 {
		t.Fatalf("invalid Reality private key: length=%d err=%v", len(key), err)
	}
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, client := range state.Clients {
		if !uuidPattern.MatchString(client.VLESSUUID) {
			t.Errorf("invalid UUID %q", client.VLESSUUID)
		}
		if len(client.RealityShortID) != 16 {
			t.Errorf("invalid short ID %q", client.RealityShortID)
		}
	}
}

func TestClientLifecycle(t *testing.T) {
	manager, _, _ := testManager(t)
	created, err := manager.AddClient(context.Background(), "pixel-phone")
	if err != nil {
		t.Fatal(err)
	}
	if len(manager.ListClients()) != 3 {
		t.Fatal("client was not added")
	}
	if err := manager.DeleteClient(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if len(manager.ListClients()) != 2 {
		t.Fatal("client was not deleted")
	}
}
