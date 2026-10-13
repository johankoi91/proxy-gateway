package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
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
	LastAudit      *Audit    `json:"lastAudit,omitempty"`
}

type Audit struct {
	CheckedAt    time.Time `json:"checkedAt"`
	Status       string    `json:"status"`
	Passed       []string  `json:"passed"`
	Skipped      []string  `json:"skipped"`
	Remediations []string  `json:"remediations,omitempty"`
}

const lastAuditPath = "/var/lib/lightnode-linux-controller/last-audit.json"

func New(runner Runner) Manager {
	return Manager{Runner: runner}
}

func (m Manager) Provision(vpsPath, outputDir string, publicBrowser bool) error {
	vps, err := config.ReadVPS(vpsPath)
	if err != nil {
		return err
	}
	controller := config.DefaultController(vps)
	m.discoverNetwork(&controller)
	if publicBrowser {
		controller.DashboardAddress = controller.ManagementAddress
        controller.PublicInboundTCPPorts = []int{8445, 9180, 5190, 59000}
		controller.PublicInboundUDPPorts = []int{59000}
		controller.ManagementTCPPorts = []int{22}
		controller.AllowedInboundNote = []string{
			"host SSH",
			"public Selkies HTTPS TCP 8445",
			"public Controller HTTPS TCP 9180",
			"public Claude Browser API HTTP/TLS TCP 9186",
			"public Selkies WebRTC UDP/TCP 59000",
		}
	}
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
		controller.DashboardAddress = ip.String()
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
	configured, _ := m.dnsConfigured(ctx, cfg)
	if configured {
		return nil
	}
	return m.ApplyDNS(ctx, cfg)
}

func (m Manager) dnsConfigured(ctx context.Context, cfg config.Controller) (bool, error) {
	checks := []struct {
		args     []string
		expected string
	}{
		{[]string{"dns", cfg.PhysicalInterface}, cfg.TunnelDNS},
		{[]string{"domain", cfg.PhysicalInterface}, "~."},
		{[]string{"default-route", cfg.PhysicalInterface}, "yes"},
	}
	for _, check := range checks {
		output, err := m.Runner.Run(ctx, "/usr/bin/resolvectl", check.args...)
		if err != nil {
			return false, err
		}
		if !strings.Contains(string(output), check.expected) {
			return false, nil
		}
	}
	return true, nil
}

func (m Manager) RefreshHostStatus(ctx context.Context, configPath string) error {
	_ = os.Remove(HostRefreshRequestPath)
	_ = m.EnsureDNS(ctx, configPath)
	report := m.Doctor(ctx, configPath)
	if !report.Protected {
		if err := m.ApplyFirewall(ctx, configPath); err == nil {
			report = m.Doctor(ctx, configPath)
		}
	}
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
	report := Report{CheckedAt: time.Now().UTC(), LastAudit: readLastAudit(lastAuditPath)}
	cfg, err := config.ReadController(configPath)
	if err != nil {
		report.Checks = append(report.Checks, Check{"config", false, err.Error()})
		return report
	}
	report.ExpectedEgress = cfg.ExpectedEgress
	add := func(id string, healthy bool, detail string) {
		report.Checks = append(report.Checks, Check{ID: id, Healthy: healthy, Detail: detail})
	}

	physical, physicalDetail := interfaceHasAddress(ctx, m.Runner, cfg.PhysicalInterface, cfg.ManagementAddress)
	add("physical_interface", physical, physicalDetail)

	nftOutput, nftErr := m.Runner.Run(ctx, "/usr/sbin/nft", "list", "table", "inet", "lightnode_controller")
	protected := nftErr == nil && firewallPolicyMatches(nftOutput, render.Firewall(cfg))
	report.Protected = protected
	add("kill_switch", protected, compactError(nftErr, "active rules exactly match the fail-closed policy"))

	bootOK := commandEquals(ctx, m.Runner, "/usr/bin/systemctl", "active", "is-active", "lightnode-lockdown.service") &&
		commandEquals(ctx, m.Runner, "/usr/bin/systemctl", "enabled", "is-enabled", "lightnode-lockdown.service") &&
		commandEquals(ctx, m.Runner, "/usr/bin/systemctl", "active", "is-active", "lightnode-container-dns.timer") &&
		commandEquals(ctx, m.Runner, "/usr/bin/systemctl", "enabled", "is-enabled", "lightnode-container-dns.timer")
	add("boot_services", bootOK, "Kill Switch boot service and 10-second host refresh timer")

	serviceOutput, serviceErr := m.Runner.Run(ctx, "/usr/bin/pgrep", "-x", "sing-box")
	serviceOK := serviceErr == nil && len(strings.Fields(string(serviceOutput))) == 1
	add("sing_box_process", serviceOK, compactError(serviceErr, "one supervised sing-box process"))

	tunnel, tunnelDetail := interfaceHasAddress(ctx, m.Runner, cfg.TunnelInterface, strings.Split(cfg.TunnelAddress, "/")[0])
	add("tunnel", tunnel, tunnelDetail)

	ruleOutput, ruleErr := m.Runner.Run(ctx, "/usr/sbin/ip", "-json", "-details", "rule", "show")
	ruleCount, countErr := priorityRuleCount(ruleOutput, 1)
	ruleText := string(ruleOutput)
	rulesOK := ruleErr == nil && countErr == nil && ruleCount == 1 && strings.Contains(ruleText, `"fwmark":"0x2023"`) && strings.Contains(ruleText, `"table":"2022"`)
	ruleDetail := fmt.Sprintf("%d active sing-box local rule(s); mark 0x2023 -> table 2022", ruleCount)
	if ruleErr != nil || countErr != nil {
		ruleDetail = compactError(firstError(ruleErr, countErr), ruleDetail)
	}
	add("route_rule_hygiene", rulesOK, ruleDetail)

	dnsOutput, dnsErr := m.Runner.Run(ctx, "/usr/bin/resolvectl", "dns", cfg.PhysicalInterface)
	dnsConfigured := dnsErr == nil && strings.Contains(string(dnsOutput), cfg.TunnelDNS)
	dnsCtx, cancelDNS := context.WithTimeout(ctx, 4*time.Second)
	_, dnsQueryErr := m.Runner.Run(dnsCtx, "/usr/bin/resolvectl", "query", "--cache=no", "example.com")
	cancelDNS()
	dnsOK := dnsConfigured && dnsQueryErr == nil
	dnsDetail := cfg.TunnelDNS + " configured; live query succeeded"
	if !dnsOK {
		dnsDetail = compactError(firstError(dnsErr, dnsQueryErr), strings.TrimSpace(string(dnsOutput)))
	}
	add("dns", dnsOK, dnsDetail)

	timeOutput, timeErr := m.Runner.Run(ctx, "/usr/bin/chronyc", "tracking")
	chronyConfig, chronyErr := os.ReadFile("/etc/chrony/conf.d/lightnode-controller.conf")
	timeOK := timeErr == nil && chronyErr == nil && strings.Contains(string(timeOutput), "Leap status") && strings.Contains(string(timeOutput), "Normal") && strings.Contains(string(chronyConfig), "bindacqdevice "+cfg.TunnelInterface)
	add("time_sync", timeOK, compactError(firstError(timeErr, chronyErr), "chrony synchronized and bound to "+cfg.TunnelInterface))

	ipv6Output, ipv6RouteErr := m.Runner.Run(ctx, "/usr/sbin/ip", "-6", "route", "show", "default")
	ipv6Ctx, cancelIPv6 := context.WithTimeout(ctx, 3*time.Second)
	_, ipv6ProbeErr := m.Runner.Run(ipv6Ctx, "/usr/bin/curl", "-6", "--fail", "--silent", "--show-error", "--connect-timeout", "1", "--max-time", "2", "https://api64.ipify.org")
	cancelIPv6()
	ipv6Safe := ipv6RouteErr == nil && strings.TrimSpace(string(ipv6Output)) == "" && ipv6ProbeErr != nil && protected
	add("ipv6_leak", ipv6Safe, "no public IPv6 default route; active IPv6 probe blocked")

	endpointStart := time.Now()
	_, endpointErr := m.Runner.Run(ctx, "/usr/bin/nc", "-4", "-z", "-w", "3", cfg.VPSAddress, strconv.Itoa(cfg.VPSPort))
	endpointLatency := time.Since(endpointStart).Milliseconds()
	endpointOK := endpointErr == nil
	add("tokyo_endpoint", endpointOK, compactError(endpointErr, fmt.Sprintf("%s:%d reachable in %d ms", cfg.VPSAddress, cfg.VPSPort, endpointLatency)))

	egressOK := false
	if protected && serviceOK && tunnel && dnsOK && endpointOK {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		output, probeErr := m.Runner.Run(probeCtx, "/usr/bin/curl", "--fail", "--silent", "--show-error", "--max-time", "6", cfg.EgressProbeURL)
		cancel()
		observed := strings.TrimSpace(string(output))
		report.ObservedEgress = observed
		egressOK = probeErr == nil && observed == cfg.ExpectedEgress
		add("tokyo_egress", egressOK, compactError(probeErr, observed))
	} else {
		add("tokyo_egress", false, "waiting for firewall, service, TUN, DNS and Endpoint")
	}

	dockerEgressOutput, dockerEgressErr := m.Runner.Run(ctx, "/usr/bin/docker", "exec", "beijing-selkies-chromium", "curl", "-4fsS", "--max-time", "6", cfg.EgressProbeURL)
	dockerObserved := strings.TrimSpace(string(dockerEgressOutput))
	dockerEgressOK := dockerEgressErr == nil && dockerObserved == cfg.ExpectedEgress
	add("docker_egress", dockerEgressOK, compactError(dockerEgressErr, dockerObserved))

	report.Connected = egressOK && dockerEgressOK
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

func interfaceHasAddress(ctx context.Context, runner Runner, name, address string) (bool, string) {
	if !interfaceExists(name) {
		return false, name + " is missing"
	}
	output, err := runner.Run(ctx, "/usr/sbin/ip", "-4", "-brief", "address", "show", "dev", name)
	detail := strings.TrimSpace(string(output))
	if err != nil {
		return false, err.Error()
	}
	return strings.Contains(detail, address+"/"), detail
}

func commandEquals(ctx context.Context, runner Runner, name, expected string, args ...string) bool {
	output, err := runner.Run(ctx, name, args...)
	return err == nil && strings.TrimSpace(string(output)) == expected
}

func firewallPolicyMatches(actual, expected []byte) bool {
	return reflect.DeepEqual(policyChains(actual), policyChains(expected))
}

func policyChains(value []byte) map[string][]string {
	result := map[string][]string{"output": {}, "forward": {}}
	chain := ""
	for _, raw := range strings.Split(string(value), "\n") {
		line := strings.TrimSpace(raw)
		if index := strings.Index(line, "#"); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		if strings.HasPrefix(line, "chain ") {
			fields := strings.Fields(line)
			chain = ""
			if len(fields) >= 2 {
				if _, ok := result[fields[1]]; ok {
					chain = fields[1]
				}
			}
			continue
		}
		if line == "}" {
			chain = ""
			continue
		}
		if chain == "" || line == "" {
			continue
		}
		line = strings.Replace(line, "priority 10;", "priority filter + 10;", 1)
		result[chain] = append(result[chain], strings.Join(strings.Fields(line), " "))
	}
	for name := range result {
		sort.Strings(result[name])
	}
	return result
}

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func readLastAudit(path string) *Audit {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var audit Audit
	if json.Unmarshal(data, &audit) != nil || audit.Status == "" || audit.CheckedAt.IsZero() {
		return nil
	}
	return &audit
}

func priorityRuleCount(value []byte, priority int) (int, error) {
	var rules []struct {
		Priority int `json:"priority"`
	}
	if err := json.Unmarshal(value, &rules); err != nil {
		return 0, fmt.Errorf("decode policy rules: %w", err)
	}
	count := 0
	for _, rule := range rules {
		if rule.Priority == priority {
			count++
		}
	}
	return count, nil
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
