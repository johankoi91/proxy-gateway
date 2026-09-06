package sysmanage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingRunner struct {
	mu       sync.Mutex
	commands []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))
	r.mu.Unlock()
	if name == "/sbin/pfctl" && reflect.DeepEqual(args, []string{"-s", "info"}) {
		return []byte("Status: Disabled\n"), nil
	}
	if name == "/usr/sbin/scutil" && reflect.DeepEqual(args, []string{"--nwi"}) {
		return []byte("Network interfaces: en0\n"), nil
	}
	if name == "/usr/sbin/networksetup" && reflect.DeepEqual(args, []string{"-listnetworkserviceorder"}) {
		return []byte("(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n"), nil
	}
	if name == "/usr/bin/pgrep" {
		return nil, errors.New("not running")
	}
	return nil, nil
}

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

func TestConnectionAPIListenAddress(t *testing.T) {
	address, err := connectionAPIListenAddress("http://127.0.0.1:9090")
	if err != nil || address != "127.0.0.1:9090" {
		t.Fatalf("address=%q err=%v", address, err)
	}
	for _, value := range []string{"127.0.0.1:9090", "https://127.0.0.1:9090", "http://127.0.0.1:9090/path", "http://example.com:9090"} {
		if _, err := connectionAPIListenAddress(value); err == nil {
			t.Fatalf("invalid URL accepted: %q", value)
		}
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

func TestBootstrapFirewallLoadsAndEnablesCommittedRules(t *testing.T) {
	directory := t.TempDir()
	mainConfig := filepath.Join(directory, "pf.conf")
	anchor := filepath.Join(directory, "anchor")
	committed := filepath.Join(directory, "firewall.committed")
	if err := os.WriteFile(mainConfig, []byte(managedBegin+"\nanchor \"com.lightnode.vless-killswitch\"\nload anchor \"com.lightnode.vless-killswitch\" from \""+anchor+"\"\n"+managedEnd+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(anchor, []byte("block drop out quick inet all\nblock drop out quick inet6 all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(committed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	manager := New(runner)
	manager.EUID = func() int { return 0 }
	manager.Paths.PFConfig = mainConfig
	manager.Paths.PFAnchor = anchor
	manager.Paths.Committed = committed
	if err := manager.BootstrapFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	for _, expected := range []string{"/sbin/pfctl -vnf " + mainConfig, "/sbin/pfctl -f " + mainConfig, "/sbin/pfctl -e"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in commands:\n%s", expected, joined)
		}
	}
}

func TestCommandRecognition(t *testing.T) {
	for _, value := range []string{"tunnel", "firewall", "dns", "conflicts", "notify", "install", "uninstall", "regression"} {
		if !IsCommand(value) {
			t.Fatalf("command %q not recognized", value)
		}
	}
	if IsCommand("daemon") {
		t.Fatal("daemon flags must stay on the default code path")
	}
}

func TestFullUninstallRequiresConfirmationAndRemovesState(t *testing.T) {
	directory := t.TempDir()
	paths := Paths{
		AppDir: filepath.Join(directory, "app"), LogDir: filepath.Join(directory, "logs"),
		TemplatesDir: filepath.Join(directory, "templates"), DocsDir: filepath.Join(directory, "docs"),
		Config: filepath.Join(directory, "app", "config.json"), SingBoxConfig: filepath.Join(directory, "app", "sing-box.json"),
		Paused: filepath.Join(directory, "app", "paused"), Committed: filepath.Join(directory, "app", "firewall.committed"),
		RollbackState: filepath.Join(directory, "app", "rollback"), RegressionState: filepath.Join(directory, "app", "regression.json"),
		PFConfig: filepath.Join(directory, "pf.conf"), PFAnchor: filepath.Join(directory, "pf.anchor"),
		RollbackPlist: filepath.Join(directory, "rollback.plist"), RegressionPlist: filepath.Join(directory, "regression.plist"),
		ControllerPlist: filepath.Join(directory, "controller.plist"), SingBoxPlist: filepath.Join(directory, "sing-box.plist"),
		PFBootstrapPlist: filepath.Join(directory, "pf-bootstrap.plist"),
		ControllerBinary: filepath.Join(directory, "lightnode-controller"), SingBoxBinary: filepath.Join(directory, "sing-box"),
	}
	for _, path := range []string{paths.AppDir, paths.LogDir, paths.TemplatesDir, paths.DocsDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		paths.Config, paths.SingBoxConfig, paths.Committed, paths.PFAnchor,
		paths.ControllerPlist, paths.SingBoxPlist, paths.PFBootstrapPlist,
		paths.RollbackPlist, paths.ControllerBinary, paths.SingBoxBinary,
	} {
		if err := os.WriteFile(path, []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.PFConfig, []byte(managedBegin+"\n"+managedEnd+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &recordingRunner{}
	manager := New(runner)
	manager.EUID = func() int { return 0 }
	manager.Paths = paths
	if err := manager.Uninstall(context.Background(), false); err == nil {
		t.Fatal("unconfirmed uninstall must be rejected")
	}
	if err := manager.Uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		paths.AppDir, paths.LogDir, paths.TemplatesDir, paths.DocsDir,
		paths.ControllerBinary, paths.SingBoxBinary, paths.PFAnchor,
		paths.ControllerPlist, paths.SingBoxPlist, paths.PFBootstrapPlist,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall left %s", path)
		}
	}
	commands := strings.Join(runner.commands, "\n")
	for _, expected := range []string{
		"/usr/sbin/networksetup -setdnsservers Wi-Fi empty",
		"/usr/sbin/networksetup -setv6automatic Wi-Fi",
		"/usr/sbin/pkgutil --forget " + packageIdentifier,
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("uninstall did not run %q:\n%s", expected, commands)
		}
	}
}
