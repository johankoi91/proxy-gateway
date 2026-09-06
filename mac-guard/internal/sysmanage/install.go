package sysmanage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"lightnode-mac-guard/internal/singconfig"
)

var legacyRuntimeTools = []string{
	"lightnode-notify", "lightnode-render-pf", "lightnode-refresh-killswitch",
	"lightnode-refresh-kill-switch", "lightnode-refresh-dns", "lightnode-neutralize-conflicts",
	"lightnode-start-tunnel", "lightnode-pause-tunnel", "lightnode-enable-killswitch",
	"lightnode-enable-kill-switch", "lightnode-confirm-killswitch", "lightnode-confirm-kill-switch",
	"lightnode-disable-killswitch", "lightnode-disable-kill-switch", "lightnode-pf-rollback",
	"lightnode-uninstall", "lightnode-migrate-clash-config",
}

func (m *Manager) PostInstall(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
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
		return errors.New("cannot upgrade during an unconfirmed Kill Switch trial")
	}
	for _, item := range []struct {
		name, destination string
		mode              os.FileMode
		onlyIfMissing     bool
	}{
		{"config.example.json", m.Paths.Config, 0o600, true},
		{"sing-box.v2-global.example.json", m.Paths.SingBoxConfig, 0o600, true},
		{"v2-policy.example.json", m.Paths.Policy, 0o600, true},
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
	if err := m.migrateConfigCommands(); err != nil {
		return err
	}
	if err := m.migrateGlobalTokyoPolicy(); err != nil {
		return err
	}
	for _, root := range []string{m.Paths.AppDir, m.Paths.LogDir} {
		_ = filepath.Walk(root, func(path string, _ os.FileInfo, walkErr error) error {
			if walkErr == nil {
				_ = os.Chown(path, 0, 0)
			}
			return nil
		})
	}
	for _, name := range legacyRuntimeTools {
		_ = os.Remove(filepath.Join("/usr/local/libexec", name))
	}
	for _, name := range []string{"DNS-DESIGN.md", "V2-SPLIT-ROUTING-DESIGN.md", "PROXY-VPN-CONFLICT-ISOLATION.md"} {
		_ = os.Remove(filepath.Join(m.Paths.DocsDir, name))
	}
	if fileExists(m.Paths.Paused) {
		_, _ = m.run(ctx, "/bin/launchctl", "disable", "system/"+singBoxLabel)
		_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+singBoxLabel)
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
	const guardLabel = "com.lightnode.vless-guard"
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+guardLabel)
	if _, err := m.run(ctx, "/bin/launchctl", "enable", "system/"+guardLabel); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.GuardPlist); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+guardLabel); err != nil {
		return err
	}
	return nil
}

func (m *Manager) migrateGlobalTokyoPolicy() error {
	data, err := os.ReadFile(m.Paths.SingBoxConfig)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := singconfig.EnforceGlobalTokyo(document); err != nil {
		return err
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(m.Paths.SingBoxConfig, append(updated, '\n'), 0o600); err != nil {
		return err
	}
	policy := map[string]any{"version": 1, "mode": "enforce", "profile": "global_japan"}
	policyData, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(m.Paths.Policy, append(policyData, '\n'), 0o600)
}

func (m *Manager) migrateConfigCommands() error {
	data, err := os.ReadFile(m.Paths.Config)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	binary := m.Paths.GuardBinary
	document["firewallRepairCommand"] = []string{binary, "firewall", "refresh"}
	document["dnsRepairCommand"] = []string{binary, "dns", "refresh"}
	document["conflictRemediationCommand"] = []string{binary, "conflicts", "remediate"}
	document["recoveryCommand"] = []string{binary, "tunnel", "restart"}
	document["startCommand"] = []string{binary, "tunnel", "start"}
	document["pauseCommand"] = []string{binary, "tunnel", "pause"}
	document["notificationCommand"] = []string{binary, "notify"}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(m.Paths.Config, append(updated, '\n'), 0o600)
}

func (m *Manager) Uninstall(ctx context.Context, purge bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if fileExists(m.Paths.Committed) {
		return errors.New("refusing uninstall while the Kill Switch is committed; run: sudo /usr/local/libexec/lightnode-guard firewall disable --yes")
	}
	_ = m.RefreshDNS(ctx, true)
	for _, label := range []string{"com.lightnode.vless-guard", singBoxLabel} {
		_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+label)
	}
	for _, path := range []string{m.Paths.GuardPlist, m.Paths.SingBoxPlist, m.Paths.SingBoxBinary} {
		_ = os.Remove(path)
	}
	for _, name := range legacyRuntimeTools {
		_ = os.Remove(filepath.Join("/usr/local/libexec", name))
	}
	_ = os.RemoveAll(m.Paths.TemplatesDir)
	_ = os.RemoveAll(m.Paths.DocsDir)
	if purge {
		_ = os.RemoveAll(m.Paths.AppDir)
		_ = os.RemoveAll(m.Paths.LogDir)
	}
	m.printf("LightNode Guard and sing-box binaries removed. PF rules were not modified.\n")
	if err := os.Remove(m.Paths.GuardBinary); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove guard binary: %w", err)
	}
	return nil
}
