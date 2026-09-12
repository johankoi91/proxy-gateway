package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lightnode-linux-controller/internal/config"
	"lightnode-linux-controller/internal/render"
)

type Manager struct {
	Runner Runner
}

type Check struct {
	ID      string `json:"id"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

type Report struct {
	Protected      bool      `json:"protected"`
	Connected      bool      `json:"connected"`
	ExpectedEgress string    `json:"expectedEgress"`
	ObservedEgress string    `json:"observedEgress,omitempty"`
	Checks         []Check   `json:"checks"`
	CheckedAt      time.Time `json:"checkedAt"`
}

func New(runner Runner) Manager {
	return Manager{Runner: runner}
}

func (m Manager) Provision(vpsPath, outputDir string) error {
	vps, err := config.ReadVPS(vpsPath)
	if err != nil {
		return err
	}
	controller := config.DefaultController(vps)
	m.discoverNetwork(&controller)
	if err := controller.Validate(); err != nil {
		return err
	}
	singBox, err := render.SingBox(vps, controller)
	if err != nil {
		return err
	}
	controllerJSON, err := render.Controller(controller)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return err
	}
	for _, file := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"controller.json", append(controllerJSON, '\n'), 0o600},
		{"sing-box.json", append(singBox, '\n'), 0o600},
		{"lockdown.nft", render.Firewall(controller), 0o600},
	} {
		if err := writeAtomic(filepath.Join(outputDir, file.name), file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func (m Manager) discoverNetwork(controller *config.Controller) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := m.Runner.Run(ctx, "/usr/sbin/ip", "-json", "route", "show", "default")
	if err != nil {
		return
	}
	var routes []struct {
		Dev     string `json:"dev"`
		Gateway string `json:"gateway"`
	}
	if json.Unmarshal(output, &routes) != nil || len(routes) == 0 || routes[0].Dev == "" || net.ParseIP(routes[0].Gateway).To4() == nil {
		return
	}
	physical, err := net.InterfaceByName(routes[0].Dev)
	if err != nil {
		return
	}
	addresses, err := physical.Addrs()
	if err != nil {
		return
	}
	for _, address := range addresses {
		ip, network, err := net.ParseCIDR(address.String())
		if err != nil || ip.To4() == nil {
			continue
		}
		controller.PhysicalInterface = routes[0].Dev
		controller.ManagementAddress = ip.String()
		controller.ManagementSource = routes[0].Gateway
		controller.ManagementCIDR = network.String()
		return
	}
}

func (m Manager) Validate(ctx context.Context, configPath string) error {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(cfg.FirewallConfig); err != nil {
		return fmt.Errorf("firewall configuration: %w", err)
	}
	if _, err := m.Runner.Run(ctx, cfg.SingBoxBinary, "check", "-c", cfg.SingBoxConfig); err != nil {
		return fmt.Errorf("sing-box configuration: %w", err)
	}
	if err := m.validateFirewall(ctx, cfg.FirewallConfig); err != nil {
		return fmt.Errorf("nftables configuration: %w", err)
	}
	return nil
}

func (m Manager) ApplyFirewall(ctx context.Context, configPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("firewall apply requires root")
	}
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, "/usr/sbin/nft", "add", "table", "inet", "lightnode_controller"); err != nil && !strings.Contains(err.Error(), "File exists") {
		return err
	}
	if _, err := m.Runner.Run(ctx, "/usr/sbin/nft", "--check", "--file", cfg.FirewallConfig); err != nil {
		return err
	}
	_, err = m.Runner.Run(ctx, "/usr/sbin/nft", "--file", cfg.FirewallConfig)
	return err
}

func (m Manager) ApplyDNS(ctx context.Context, cfg config.Controller) error {
	if os.Geteuid() != 0 {
		return errors.New("DNS apply requires root")
	}
	if _, err := m.Runner.Run(ctx, "/usr/bin/resolvectl", "dns", cfg.PhysicalInterface, cfg.TunnelDNS); err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, "/usr/bin/resolvectl", "domain", cfg.PhysicalInterface, "~."); err != nil {
		return err
	}
	_, err := m.Runner.Run(ctx, "/usr/bin/resolvectl", "default-route", cfg.PhysicalInterface, "yes")
	return err
}

func (m Manager) ApplyDNSConfig(ctx context.Context, configPath string) error {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	return m.ApplyDNS(ctx, cfg)
}

func (m Manager) WaitAndApplyDNS(ctx context.Context, configPath string) error {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	if err := m.waitForInterface(ctx, cfg.TunnelInterface, 60*time.Second); err != nil {
		return err
	}
	return m.ApplyDNS(ctx, cfg)
}

func (m Manager) EnsureDNS(ctx context.Context, configPath string) error {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	if !interfaceExists(cfg.TunnelInterface) {
		return nil
	}
	return m.ApplyDNS(ctx, cfg)
}

func (m Manager) RefreshHostStatus(ctx context.Context, configPath string) error {
	_ = os.Remove(HostRefreshRequestPath)
	_ = m.EnsureDNS(ctx, configPath)
	report := m.Doctor(ctx, configPath)
	data, err := EncodeReport(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(HostStatusPath), 0o755); err != nil {
		return err
	}
	return writeAtomic(HostStatusPath, append(data, '\n'), 0o644)
}

func (m Manager) validateFirewall(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, "/usr/sbin/nft", "list", "table", "inet", "lightnode_controller"); err == nil {
		_, err = m.Runner.Run(ctx, "/usr/sbin/nft", "--check", "--file", path)
		return err
	}
	data = []byte(strings.TrimPrefix(string(data), "flush table inet lightnode_controller\n"))
	temporary, err := os.CreateTemp("", "lightnode-nft-check-*.nft")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	_, err = m.Runner.Run(ctx, "/usr/sbin/nft", "--check", "--file", temporaryPath)
	return err
}

func (m Manager) Doctor(ctx context.Context, configPath string) Report {
	report := Report{CheckedAt: time.Now().UTC()}
	cfg, err := config.ReadController(configPath)
	if err != nil {
		report.Checks = append(report.Checks, Check{"config", false, err.Error()})
		return report
	}
	report.ExpectedEgress = cfg.ExpectedEgress
	add := func(id string, healthy bool, detail string) {
		report.Checks = append(report.Checks, Check{ID: id, Healthy: healthy, Detail: detail})
	}

	physical := interfaceExists(cfg.PhysicalInterface)
	add("physical_interface", physical, cfg.PhysicalInterface)

	nftOutput, nftErr := m.Runner.Run(ctx, "/usr/sbin/nft", "list", "table", "inet", "lightnode_controller")
	protected := nftErr == nil && bytesContainAll(nftOutput,
		"hook output priority filter + 10", "hook forward priority filter + 10",
		"policy drop", "ct status dnat accept", cfg.VPSAddress,
		cfg.TunnelInterface, "tcp dport { 8443, 8444 }")
	report.Protected = protected
	add("kill_switch", protected, compactError(nftErr, "inet lightnode_controller loaded"))

	serviceOutput, serviceErr := m.Runner.Run(ctx, "/usr/bin/pgrep", "-x", "sing-box")
	serviceOK := serviceErr == nil && strings.TrimSpace(string(serviceOutput)) != ""
	add("sing_box_process", serviceOK, compactError(serviceErr, "sing-box is running in the Controller container"))

	tunnel := interfaceExists(cfg.TunnelInterface)
	add("tunnel", tunnel, cfg.TunnelInterface)

	dnsOutput, dnsErr := m.Runner.Run(ctx, "/usr/bin/resolvectl", "dns", cfg.PhysicalInterface)
	dnsOK := dnsErr == nil && strings.Contains(string(dnsOutput), cfg.TunnelDNS)
	add("dns", dnsOK, compactError(dnsErr, strings.TrimSpace(string(dnsOutput))))

	ipv6Output, _ := m.Runner.Run(ctx, "/usr/sbin/ip", "-6", "route", "show", "default")
	ipv6Safe := protected || strings.TrimSpace(string(ipv6Output)) == ""
	add("ipv6_leak", ipv6Safe, "public IPv6 is blocked by the Kill Switch")

	if protected && serviceOK && tunnel {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		output, probeErr := m.Runner.Run(probeCtx, "/usr/bin/curl", "--fail", "--silent", "--show-error", "--max-time", "6", cfg.EgressProbeURL)
		observed := strings.TrimSpace(string(output))
		report.ObservedEgress = observed
		egressOK := probeErr == nil && observed == cfg.ExpectedEgress
		add("tokyo_egress", egressOK, compactError(probeErr, observed))
		report.Connected = egressOK && dnsOK
	} else {
		add("tokyo_egress", false, "not tested until firewall, service and TUN are healthy")
	}
	return report
}

func (m Manager) waitForInterface(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if interfaceExists(name) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("TUN interface %s did not appear", name)
}

func interfaceExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}

func bytesContainAll(value []byte, fragments ...string) bool {
	text := string(value)
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}

func compactError(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".lightnode-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func EncodeReport(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
