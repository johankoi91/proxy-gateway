package sysmanage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/singconfig"
)

const (
	controllerLabel   = "com.lightnode.controller"
	packageIdentifier = "com.lightnode.mac-controller"
)

func (m *Manager) PostInstall(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if err := m.validateInstalledPayload(); err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.AppDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(m.Paths.AppDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.LogDir, 0o750); err != nil {
		return err
	}
	if err := os.Chmod(m.Paths.LogDir, 0o750); err != nil {
		return err
	}
	if fileExists(filepath.Join(m.Paths.RollbackState, "token")) {
		return errors.New("cannot install during an unconfirmed Kill Switch trial")
	}
	if fileExists(m.Paths.RegressionState) {
		return errors.New("cannot install while a live regression recovery is armed")
	}
	for _, item := range []struct {
		name, destination string
		mode              os.FileMode
		onlyIfMissing     bool
	}{
		{"config.example.json", m.Paths.Config, 0o600, true},
		{"sing-box.example.json", m.Paths.SingBoxConfig, 0o600, true},
		{"pf.anchor.tmpl", filepath.Join(m.Paths.AppDir, "pf.anchor.tmpl"), 0o644, false},
		{"pf.main.tmpl", filepath.Join(m.Paths.AppDir, "pf.main.tmpl"), 0o644, false},
		{"com.lightnode.vless-killswitch-rollback.plist.tmpl", filepath.Join(m.Paths.AppDir, "com.lightnode.vless-killswitch-rollback.plist.tmpl"), 0o644, false},
	} {
		if item.onlyIfMissing && fileExists(item.destination) {
			continue
		}
		if err := copyFile(filepath.Join(m.Paths.TemplatesDir, item.name), item.destination, item.mode); err != nil {
			return err
		}
	}
	if err := m.normalizeConfiguration(); err != nil {
		return err
	}
	if err := m.DisablePhysicalIPv6(ctx); err != nil {
		return fmt.Errorf("disable physical IPv6: %w", err)
	}
	for _, root := range []string{m.Paths.AppDir, m.Paths.LogDir} {
		_ = filepath.Walk(root, func(path string, _ os.FileInfo, walkErr error) error {
			if walkErr == nil {
				_ = os.Chown(path, 0, 0)
			}
			return nil
		})
	}
	m.secureLogFiles()
	const pfBootstrapLabel = "com.lightnode.pf-bootstrap"
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+pfBootstrapLabel)
	if _, err := m.run(ctx, "/bin/launchctl", "enable", "system/"+pfBootstrapLabel); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.PFBootstrapPlist); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+pfBootstrapLabel); err != nil {
		return err
	}
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+singBoxLabel)
	if fileExists(m.Paths.Paused) {
		_, _ = m.run(ctx, "/bin/launchctl", "disable", "system/"+singBoxLabel)
	} else {
		placeholder, err := containsPlaceholder(m.Paths.SingBoxConfig)
		if err != nil {
			return err
		}
		if !placeholder {
			if err := m.StartTunnel(ctx); err != nil {
				return err
			}
		}
	}
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+controllerLabel)
	if _, err := m.run(ctx, "/bin/launchctl", "enable", "system/"+controllerLabel); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.ControllerPlist); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+controllerLabel); err != nil {
		return err
	}
	m.secureLogFiles()
	return nil
}

func (m *Manager) normalizeConfiguration() error {
	cfg, err := config.Load(m.Paths.Config)
	if err != nil {
		return err
	}
	defaults := config.Default()
	cfg.ConfigFilePath = ""
	cfg.FirewallCommittedFile = m.Paths.Committed
	cfg.FirewallRepairCommand = defaults.FirewallRepairCommand
	cfg.DNSRepairCommand = defaults.DNSRepairCommand
	cfg.ConflictRemediationCommand = defaults.ConflictRemediationCommand
	cfg.SingBoxConfigFile = m.Paths.SingBoxConfig
	cfg.LaunchDaemonLabel = controllerLabel
	cfg.ProviderLaunchDaemonLabel = defaults.ProviderLaunchDaemonLabel
	cfg.RecoveryCommand = defaults.RecoveryCommand
	cfg.StartCommand = defaults.StartCommand
	cfg.PauseCommand = defaults.PauseCommand
	cfg.MaintenanceFile = m.Paths.Paused
	cfg.LifecycleActionFile = filepath.Join(m.Paths.AppDir, "lifecycle-action.json")
	cfg.LifecycleActionTimeoutSeconds = defaults.LifecycleActionTimeoutSeconds
	cfg.NotificationCommand = defaults.NotificationCommand
	cfg.EventLogFile = filepath.Join(m.Paths.LogDir, "events.jsonl")
	cfg.BackupDir = filepath.Join(m.Paths.AppDir, "backups")
	if strings.TrimSpace(cfg.ConnectionAPISecret) == "" || strings.HasPrefix(cfg.ConnectionAPISecret, "REPLACE_") {
		cfg.ConnectionAPISecret, err = randomToken(24)
		if err != nil {
			return err
		}
	}
	singData, err := os.ReadFile(m.Paths.SingBoxConfig)
	if err != nil {
		return err
	}
	var singDocument map[string]any
	if err := json.Unmarshal(singData, &singDocument); err != nil {
		return err
	}
	if err := singconfig.EnforceGlobalTokyo(singDocument); err != nil {
		return err
	}
	experimental := singconfig.Object(singDocument, "experimental")
	connectionAPI := singconfig.Object(experimental, "clash_api")
	connectionListenAddress, err := connectionAPIListenAddress(cfg.ConnectionAPIURL)
	if err != nil {
		return err
	}
	connectionAPI["external_controller"] = connectionListenAddress
	connectionAPI["secret"] = cfg.ConnectionAPISecret
	connectionAPI["access_control_allow_origin"] = []string{"http://" + cfg.ListenAddress}
	connectionAPI["access_control_allow_private_network"] = false
	configData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	updatedSing, err := json.MarshalIndent(singDocument, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(m.Paths.Config, append(configData, '\n'), 0o600); err != nil {
		return err
	}
	return writeFileAtomic(m.Paths.SingBoxConfig, append(updatedSing, '\n'), 0o600)
}

func connectionAPIListenAddress(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid loopback connection API URL %q", rawURL)
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" ||
		(host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return "", fmt.Errorf("invalid loopback connection API URL %q", rawURL)
	}
	return parsed.Host, nil
}

func (m *Manager) validateInstalledPayload() error {
	required := []string{
		m.Paths.ControllerBinary, m.Paths.SingBoxBinary, m.Paths.ControllerPlist,
		m.Paths.SingBoxPlist, m.Paths.PFBootstrapPlist,
		filepath.Join(m.Paths.TemplatesDir, "config.example.json"),
		filepath.Join(m.Paths.TemplatesDir, "sing-box.example.json"),
		filepath.Join(m.Paths.TemplatesDir, "pf.anchor.tmpl"),
		filepath.Join(m.Paths.TemplatesDir, "pf.main.tmpl"),
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return fmt.Errorf("installed payload is incomplete: %s", path)
		}
	}
	return nil
}

func (m *Manager) secureLogFiles() {
	for _, name := range []string{
		"controller.log", "controller.error.log", "sing-box.log", "sing-box.error.log",
		"pf-bootstrap.log", "pf-bootstrap.error.log", "pf-rollback.log", "pf-rollback.error.log",
		"regression-recovery.log", "regression-recovery.error.log", "events.jsonl",
	} {
		_ = os.Chmod(filepath.Join(m.Paths.LogDir, name), 0o600)
	}
}

func (m *Manager) Uninstall(ctx context.Context, confirmed bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !confirmed {
		return errors.New("uninstall permanently removes the tunnel, PF rules, configuration, backups and logs; repeat with --yes")
	}
	if fileExists(m.Paths.RegressionState) {
		return errors.New("cannot uninstall while a live regression recovery is armed; run regression recover first")
	}
	tokenFile := filepath.Join(m.Paths.RollbackState, "token")
	if token, err := os.ReadFile(tokenFile); err == nil {
		if err := m.RollbackFirewall(ctx, strings.TrimSpace(string(token)), 0); err != nil {
			return fmt.Errorf("rollback active PF trial: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	m.quitProtectedApps(ctx)
	if uid, ok := consoleUID(); ok {
		_, _ = m.run(ctx, "/bin/launchctl", "bootout", fmt.Sprintf("gui/%d/com.openai.chat-helper", uid))
	}
	_, _ = m.run(ctx, "/bin/launchctl", "disable", "system/"+singBoxLabel)
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+singBoxLabel)
	if err := m.RefreshDNS(ctx, true); err != nil {
		return fmt.Errorf("restore automatic DNS: %w", err)
	}
	if err := m.RestorePhysicalIPv6(ctx); err != nil {
		return fmt.Errorf("restore automatic IPv6: %w", err)
	}
	if managedFirewallPresent(m.Paths.PFConfig, m.Paths.PFAnchor) || fileExists(m.Paths.Committed) {
		if err := m.DisableFirewall(ctx, true); err != nil {
			return fmt.Errorf("remove managed PF rules: %w", err)
		}
	}

	for _, label := range []string{
		controllerLabel, singBoxLabel, "com.lightnode.pf-bootstrap",
		"com.lightnode.vless-killswitch-rollback", regressionRecoveryLabel,
	} {
		_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+label)
	}
	for _, path := range []string{
		m.Paths.ControllerPlist, m.Paths.PFBootstrapPlist, m.Paths.SingBoxPlist,
		m.Paths.RollbackPlist, m.Paths.RegressionPlist, m.Paths.SingBoxBinary,
		m.Paths.PFAnchor,
	} {
		_ = os.Remove(path)
	}
	_ = os.RemoveAll(m.Paths.AppDir)
	_ = os.RemoveAll(m.Paths.LogDir)
	_ = os.RemoveAll(m.Paths.TemplatesDir)
	_ = os.Remove(filepath.Dir(m.Paths.TemplatesDir))
	_ = os.RemoveAll(m.Paths.DocsDir)
	_, _ = m.run(ctx, "/usr/sbin/pkgutil", "--forget", packageIdentifier)
	m.printf("LightNode Controller fully removed; automatic DNS and IPv6 restored.\n")
	if err := os.Remove(m.Paths.ControllerBinary); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove controller binary: %w", err)
	}
	return nil
}

func managedFirewallPresent(configPath, anchorPath string) bool {
	if fileExists(anchorPath) {
		return true
	}
	data, err := os.ReadFile(configPath)
	return err == nil && strings.Contains(string(data), managedBegin)
}
