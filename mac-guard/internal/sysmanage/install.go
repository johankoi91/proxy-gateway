package sysmanage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/singconfig"
)

const controllerLabel = "com.lightnode.controller"

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
	connectionAPI["external_controller"] = cfg.ConnectionAPIURL
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

func (m *Manager) Uninstall(ctx context.Context, purge bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if fileExists(m.Paths.Committed) {
		return errors.New("refusing uninstall while the Kill Switch is committed; run: sudo /usr/local/libexec/lightnode-controller firewall disable --yes")
	}
	_ = m.RefreshDNS(ctx, true)
	for _, label := range []string{controllerLabel, singBoxLabel, "com.lightnode.pf-bootstrap", regressionRecoveryLabel} {
		_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+label)
	}
	for _, path := range []string{m.Paths.ControllerPlist, m.Paths.PFBootstrapPlist, m.Paths.SingBoxPlist, m.Paths.SingBoxBinary, m.Paths.RegressionPlist} {
		_ = os.Remove(path)
	}
	_ = os.RemoveAll(m.Paths.TemplatesDir)
	_ = os.RemoveAll(m.Paths.DocsDir)
	if purge {
		_ = os.RemoveAll(m.Paths.AppDir)
		_ = os.RemoveAll(m.Paths.LogDir)
	}
	m.printf("LightNode Controller and sing-box binaries removed. PF rules were not modified.\n")
	if err := os.Remove(m.Paths.ControllerBinary); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove controller binary: %w", err)
	}
	return nil
}
