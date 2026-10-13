package sysmanage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) physicalInterface(ctx context.Context) (string, error) {
	output, err := m.run(ctx, "/usr/sbin/scutil", "--nwi")
	if err != nil {
		return "", err
	}
	value := firstInterface(string(output))
	if value == "" {
		return "", errors.New("no active physical network interface")
	}
	return value, nil
}

func (m *Manager) networkService(ctx context.Context, physicalInterface string) (string, error) {
	output, err := m.run(ctx, "/usr/sbin/networksetup", "-listnetworkserviceorder")
	if err != nil {
		return "", err
	}
	service := networkServiceForInterface(string(output), physicalInterface)
	if service == "" {
		return "", fmt.Errorf("no network service maps to %s", physicalInterface)
	}
	return service, nil
}

func (m *Manager) RefreshDNS(ctx context.Context, restore bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	physical, err := m.physicalInterface(ctx)
	if err != nil {
		return err
	}
	service, err := m.networkService(ctx, physical)
	if err != nil {
		return err
	}
	if restore {
		if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setdnsservers", service, "empty"); err != nil {
			return err
		}
		m.log(ctx, fmt.Sprintf("Restored automatic DNS on %s (%s)", service, physical))
		return nil
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.ExpectedDNSServers) == 0 || net.ParseIP(cfg.ExpectedDNSServers[0]) == nil {
		return fmt.Errorf("invalid tunnel DNS server in %s", m.Paths.Config)
	}
	dns := cfg.ExpectedDNSServers[0]
	if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setdnsservers", service, dns); err != nil {
		return err
	}
	m.log(ctx, fmt.Sprintf("Set tunnel DNS on %s (%s)", service, physical))
	m.printf("network_service=%s physical_interface=%s dns_server=%s\n", service, physical, dns)
	return nil
}

func (m *Manager) DisablePhysicalIPv6(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	physical, err := m.physicalInterface(ctx)
	if err != nil {
		return err
	}
	service, err := m.networkService(ctx, physical)
	if err != nil {
		return err
	}
	if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setv6off", service); err != nil {
		return err
	}
	m.log(ctx, fmt.Sprintf("Disabled physical IPv6 on %s (%s)", service, physical))
	m.printf("network_service=%s physical_interface=%s ipv6=off\n", service, physical)
	return nil
}

func (m *Manager) RestorePhysicalIPv6(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	physical, err := m.physicalInterface(ctx)
	if err != nil {
		return err
	}
	service, err := m.networkService(ctx, physical)
	if err != nil {
		return err
	}
	if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setv6automatic", service); err != nil {
		return err
	}
	m.log(ctx, fmt.Sprintf("Restored automatic IPv6 on %s (%s)", service, physical))
	m.printf("network_service=%s physical_interface=%s ipv6=automatic\n", service, physical)
	return nil
}

func (m *Manager) RemediateConflicts(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !fileExists(m.Paths.Committed) {
		return errors.New("refusing conflict remediation before the Kill Switch is committed")
	}
	physical, _ := m.physicalInterface(ctx)
	service := ""
	if physical != "" {
		service, _ = m.networkService(ctx, physical)
	}
	disableProxies := func() {
		if service == "" {
			return
		}
		for _, arguments := range [][]string{
			{"-setwebproxystate", service, "off"},
			{"-setsecurewebproxystate", service, "off"},
			{"-setsocksfirewallproxystate", service, "off"},
			{"-setautoproxystate", service, "off"},
		} {
			_, _ = m.run(ctx, "/usr/sbin/networksetup", arguments...)
		}
	}
	disableProxies()
	if uid, ok := consoleUID(); ok {
		for _, app := range []string{"Clash Verge", "FlClash"} {
			script := fmt.Sprintf("tell application %s to quit", strconv.Quote(app))
			_, _ = m.run(ctx, "/bin/launchctl", "asuser", strconv.FormatUint(uint64(uid), 10), "/usr/bin/osascript", "-e", script)
		}
	}
	processes := []string{"clash-verge", "verge-mihomo", "FlClash"}
	for _, process := range processes {
		_, _ = m.run(ctx, "/usr/bin/pkill", "-TERM", "-x", process)
	}
	time.Sleep(2 * time.Second)
	for _, process := range processes {
		_, _ = m.run(ctx, "/usr/bin/pkill", "-KILL", "-x", process)
	}
	time.Sleep(2 * time.Second)
	disableProxies()
	for _, process := range processes {
		if m.runOK(ctx, "/usr/bin/pgrep", "-x", process) {
			return errors.New("known proxy processes are still running")
		}
	}
	vpnOutput, _ := m.run(ctx, "/usr/sbin/scutil", "--nc", "list")
	if strings.Contains(string(vpnOutput), "* (Connected)") {
		return errors.New("a system VPN remains connected; it was left running to preserve remote management")
	}
	proxyOutput, _ := m.run(ctx, "/usr/sbin/scutil", "--proxy")
	for _, key := range []string{"HTTPEnable", "HTTPSEnable", "SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
		if strings.Contains(string(proxyOutput), key+" : 1") {
			return errors.New("a system proxy remains enabled")
		}
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+singBoxLabel); err != nil {
		return err
	}
	for attempt := 0; attempt < 10; attempt++ {
		interfaces, _ := m.run(ctx, "/sbin/ifconfig")
		if tunnelInterfaceForAddress(string(interfaces), "172.19.0.1") != "" {
			break
		}
		time.Sleep(time.Second)
	}
	if err := m.RefreshDNS(ctx, false); err != nil {
		return err
	}
	if err := m.RefreshFirewall(ctx, false); err != nil {
		return err
	}
	m.log(ctx, "Known proxy conflicts were neutralized")
	m.printf("Known proxy conflicts neutralized; LightNode DNS and PF refreshed.\n")
	return nil
}

func (m *Manager) Notify(ctx context.Context, severity, component, message string) error {
	if severity == "" {
		severity = "warning"
	}
	if component == "" {
		component = "controller"
	}
	if message == "" {
		message = "LightNode Controller state changed"
	}
	m.log(ctx, fmt.Sprintf("[%s] %s: %s", severity, component, message))
	uid, ok := consoleUID()
	if !ok {
		return nil
	}
	title := "LightNode Controller: " + component
	message = strings.NewReplacer("\r", " ", "\n", " ").Replace(message)
	script := fmt.Sprintf("display notification %s with title %s", strconv.Quote(message), strconv.Quote(title))
	_, _ = m.run(ctx, "/bin/launchctl", "asuser", strconv.FormatUint(uint64(uid), 10), "/usr/bin/osascript", "-e", script)
	return nil
}
