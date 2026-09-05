package sysmanage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const firewallAnchor = "com.lightnode.vless-killswitch"

var interfacePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (m *Manager) renderPF(ctx context.Context, templatePath string) (string, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return "", err
	}
	if net.ParseIP(cfg.VPSAddress).To4() == nil || cfg.VPSPort < 1 || cfg.VPSPort > 65535 {
		return "", fmt.Errorf("invalid VPS endpoint in %s", m.Paths.Config)
	}
	physical, _ := m.physicalInterface(ctx)
	if physical == "" {
		route, routeErr := m.run(ctx, "/sbin/route", "-n", "get", cfg.VPSAddress)
		if routeErr != nil {
			return "", routeErr
		}
		physical = routeInterface(string(route))
	}
	if !interfacePattern.MatchString(physical) || strings.HasPrefix(physical, "utun") {
		return "", fmt.Errorf("could not resolve a physical interface for %s", cfg.VPSAddress)
	}
	interfaces, err := m.run(ctx, "/sbin/ifconfig")
	if err != nil {
		return "", err
	}
	tunnel := tunnelInterfaceForAddress(string(interfaces), cfg.TunnelAddress)
	if tunnel == "" {
		tunnel = cfg.TunnelInterfaceName
	}
	if !interfacePattern.MatchString(tunnel) {
		return "", fmt.Errorf("could not resolve the TUN interface holding %s", cfg.TunnelAddress)
	}
	localOutput, localErr := m.run(ctx, "/usr/sbin/ipconfig", "getifaddr", physical)
	localIP := strings.TrimSpace(string(localOutput))
	interfaceOutput, interfaceErr := m.run(ctx, "/sbin/ifconfig", physical)
	if interfaceErr != nil {
		return "", interfaceErr
	}
	fallbackIP, mask := parseInterfaceIPv4(string(interfaceOutput))
	if localErr != nil || localIP == "" {
		localIP = fallbackIP
	}
	cidr, err := managementCIDR(localIP, mask)
	if err != nil {
		return "", err
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}
	rendered := strings.NewReplacer(
		"{{VPS_IP}}", cfg.VPSAddress,
		"{{VPS_PORT}}", strconv.Itoa(cfg.VPSPort),
		"{{PHYSICAL_IF}}", physical,
		"{{TUN_IF}}", tunnel,
		"{{MANAGEMENT_CIDR}}", cidr,
	).Replace(string(template))
	return strings.TrimRight(rendered, "\n") + "\n", nil
}

func temporaryFile(prefix string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp("/tmp", prefix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func (m *Manager) RefreshFirewall(ctx context.Context, force bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !force && !fileExists(m.Paths.Committed) {
		m.printf("Kill Switch has not been committed; no changes made.\n")
		return nil
	}
	anchor, renderErr := m.renderPF(ctx, filepath.Join(m.Paths.AppDir, "pf.anchor.tmpl"))
	if renderErr != nil {
		existing, err := os.ReadFile(m.Paths.PFAnchor)
		if err != nil || !strings.Contains(string(existing), "block drop out quick inet all") {
			return fmt.Errorf("no valid fail-closed PF Anchor is available: %w", renderErr)
		}
		anchor = string(existing)
		m.log(ctx, "PF refresh reused the last fail-closed Anchor because interfaces are unavailable")
	}
	anchorTemp, err := temporaryFile("lightnode-pf-*", []byte(anchor), 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(anchorTemp)
	if _, err := m.run(ctx, "/sbin/pfctl", "-vnf", anchorTemp); err != nil {
		return err
	}
	if err := copyFile(anchorTemp, m.Paths.PFAnchor, 0o644); err != nil {
		return err
	}

	mainRules, mainErr := m.renderPF(ctx, filepath.Join(m.Paths.AppDir, "pf.main.tmpl"))
	current, err := os.ReadFile(m.Paths.PFConfig)
	if err != nil {
		return err
	}
	if mainErr == nil {
		combined := removeManagedPFBlock(string(current)) + "\n" + mainRules
		mainTemp, err := temporaryFile("lightnode-pfconf-*", []byte(combined), 0o644)
		if err != nil {
			return err
		}
		defer os.Remove(mainTemp)
		if _, err := m.run(ctx, "/sbin/pfctl", "-vnf", mainTemp); err != nil {
			return err
		}
		if err := copyFile(mainTemp, m.Paths.PFConfig, 0o644); err != nil {
			return err
		}
	} else if !strings.Contains(string(current), managedBegin) {
		return fmt.Errorf("no valid managed PF main rules are available: %w", mainErr)
	}
	if _, err := m.run(ctx, "/sbin/pfctl", "-vnf", m.Paths.PFConfig); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/sbin/pfctl", "-f", m.Paths.PFConfig); err != nil {
		return err
	}
	info, _ := m.run(ctx, "/sbin/pfctl", "-s", "info")
	if !strings.Contains(string(info), "Status: Enabled") {
		if _, err := m.run(ctx, "/sbin/pfctl", "-e"); err != nil {
			return err
		}
	}
	m.log(ctx, "PF Anchor refreshed")
	return nil
}

func (m *Manager) EnableFirewall(ctx context.Context, rollbackSeconds int) (err error) {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if rollbackSeconds <= 0 {
		rollbackSeconds = 180
	}
	if fileExists(m.Paths.Committed) {
		if err := m.RefreshFirewall(ctx, false); err != nil {
			return err
		}
		m.printf("Kill Switch is already committed and has been refreshed.\n")
		return nil
	}
	tokenFile := filepath.Join(m.Paths.RollbackState, "token")
	if fileExists(tokenFile) {
		return errors.New("a Kill Switch trial is already awaiting confirmation")
	}
	placeholder, err := containsPlaceholder(m.Paths.SingBoxConfig)
	if err != nil {
		return err
	}
	if placeholder {
		return errors.New("replace all VLESS/Reality placeholders before enabling PF")
	}
	if _, err := m.run(ctx, m.Paths.SingBoxBinary, "check", "-c", m.Paths.SingBoxConfig); err != nil {
		return err
	}
	if !m.runOK(ctx, "/usr/bin/pgrep", "-x", "sing-box") {
		return errors.New("sing-box is not running")
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	_, err = m.probeExpectedEgress(ctx, cfg.EgressProbeURL, cfg.ExpectedEgressIP, 2)
	if err != nil {
		return fmt.Errorf("refusing PF activation: egress probe failed: %w", err)
	}
	if err := os.MkdirAll(m.Paths.RollbackState, 0o700); err != nil {
		return err
	}
	if err := copyFile(m.Paths.PFConfig, filepath.Join(m.Paths.RollbackState, "pf.conf.before"), 0o644); err != nil {
		return err
	}
	anchorBefore := filepath.Join(m.Paths.RollbackState, "anchor.before")
	if fileExists(m.Paths.PFAnchor) {
		if err := copyFile(m.Paths.PFAnchor, anchorBefore, 0o644); err != nil {
			return err
		}
	} else {
		_ = os.Remove(anchorBefore)
	}
	info, _ := m.run(ctx, "/sbin/pfctl", "-s", "info")
	wasDisabled := filepath.Join(m.Paths.RollbackState, "pf-was-disabled")
	if !strings.Contains(string(info), "Status: Enabled") {
		if err := writeFileAtomic(wasDisabled, nil, 0o600); err != nil {
			return err
		}
	} else {
		_ = os.Remove(wasDisabled)
	}
	token, err := randomToken(18)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	armed := true
	defer func() {
		if err != nil && armed {
			_ = m.RollbackFirewall(context.Background(), token, 0)
			_, _ = m.run(context.Background(), "/bin/launchctl", "bootout", "system/com.lightnode.vless-killswitch-rollback")
			_ = os.Remove(m.Paths.RollbackPlist)
		}
	}()
	template, err := os.ReadFile(filepath.Join(m.Paths.AppDir, "com.lightnode.vless-killswitch-rollback.plist.tmpl"))
	if err != nil {
		return err
	}
	plist := strings.NewReplacer("{{ROLLBACK_TOKEN}}", token, "{{ROLLBACK_SECONDS}}", strconv.Itoa(rollbackSeconds)).Replace(string(template))
	if err := writeFileAtomic(m.Paths.RollbackPlist, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = os.Chown(m.Paths.RollbackPlist, 0, 0)
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/com.lightnode.vless-killswitch-rollback")
	if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.RollbackPlist); err != nil {
		return err
	}
	if err := m.RefreshFirewall(ctx, true); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/sbin/pfctl", "-vnf", m.Paths.PFConfig); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/sbin/pfctl", "-f", m.Paths.PFConfig); err != nil {
		return err
	}
	armed = false
	m.printf("Kill Switch trial enabled. It will roll back in %d seconds.\n", rollbackSeconds)
	m.printf("Verify SSH, Screen Sharing, egress, DNS and IPv6, then run:\n")
	m.printf("  sudo /usr/local/libexec/lightnode-guard firewall confirm\n")
	return nil
}

func (m *Manager) ConfirmFirewall(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	tokenFile := filepath.Join(m.Paths.RollbackState, "token")
	if !fileExists(tokenFile) {
		return errors.New("no active Kill Switch trial to confirm")
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	_, err = m.probeExpectedEgress(ctx, cfg.EgressProbeURL, cfg.ExpectedEgressIP, 2)
	if err != nil {
		return fmt.Errorf("refusing confirmation: egress probe failed: %w", err)
	}
	rules, err := m.run(ctx, "/sbin/pfctl", "-a", firewallAnchor, "-sr")
	if err != nil || !strings.Contains(string(rules), "block drop out quick") {
		return errors.New("refusing confirmation: managed PF Anchor is not active")
	}
	if err := writeFileAtomic(m.Paths.Committed, nil, 0o600); err != nil {
		return err
	}
	_ = os.Remove(tokenFile)
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/com.lightnode.vless-killswitch-rollback")
	_ = os.Remove(m.Paths.RollbackPlist)
	m.log(ctx, "PF Kill Switch committed")
	m.printf("Kill Switch committed. Automatic PF integrity repair is now armed.\n")
	return nil
}

func (m *Manager) RollbackFirewall(ctx context.Context, token string, delaySeconds int) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if token == "" {
		return errors.New("rollback token required")
	}
	if delaySeconds > 0 {
		time.Sleep(time.Duration(delaySeconds) * time.Second)
	}
	tokenFile := filepath.Join(m.Paths.RollbackState, "token")
	current, err := os.ReadFile(tokenFile)
	if os.IsNotExist(err) || strings.TrimSpace(string(current)) != token {
		return nil
	}
	if err != nil {
		return err
	}
	before := filepath.Join(m.Paths.RollbackState, "pf.conf.before")
	if fileExists(before) {
		_ = copyFile(before, m.Paths.PFConfig, 0o644)
	}
	anchorBefore := filepath.Join(m.Paths.RollbackState, "anchor.before")
	if fileExists(anchorBefore) {
		_ = copyFile(anchorBefore, m.Paths.PFAnchor, 0o644)
	} else {
		_ = os.Remove(m.Paths.PFAnchor)
	}
	_, _ = m.run(ctx, "/sbin/pfctl", "-f", m.Paths.PFConfig)
	if fileExists(filepath.Join(m.Paths.RollbackState, "pf-was-disabled")) {
		_, _ = m.run(ctx, "/sbin/pfctl", "-d")
	}
	_ = os.Remove(tokenFile)
	_ = os.Remove(m.Paths.Committed)
	m.log(ctx, "PF trial rolled back because it was not confirmed")
	return nil
}

func (m *Manager) DisableFirewall(ctx context.Context, confirmed bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !confirmed {
		return errors.New("this permits direct Internet fallback; repeat with --yes")
	}
	_ = os.Remove(m.Paths.Committed)
	current, err := os.ReadFile(m.Paths.PFConfig)
	if err != nil {
		return err
	}
	cleaned := removeManagedPFBlock(string(current))
	temporary, err := temporaryFile("lightnode-pfconf-*", []byte(cleaned), 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if _, err := m.run(ctx, "/sbin/pfctl", "-vnf", temporary); err != nil {
		return err
	}
	if err := copyFile(temporary, m.Paths.PFConfig, 0o644); err != nil {
		return err
	}
	_, _ = m.run(ctx, "/sbin/pfctl", "-a", firewallAnchor, "-F", "all")
	if _, err := m.run(ctx, "/sbin/pfctl", "-f", m.Paths.PFConfig); err != nil {
		return err
	}
	_ = os.Remove(m.Paths.PFAnchor)
	m.log(ctx, "PF Kill Switch explicitly disabled")
	m.printf("Managed Kill Switch rules removed. PF itself was left in its prior system state.\n")
	return nil
}
