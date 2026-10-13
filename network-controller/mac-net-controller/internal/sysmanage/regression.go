package sysmanage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"lightnode-mac-controller/internal/singconfig"
)

const regressionRecoveryLabel = "com.lightnode.regression-recovery"

type regressionState struct {
	Version               int             `json:"version"`
	Scenario              string          `json:"scenario"`
	NetworkService        string          `json:"networkService,omitempty"`
	OriginalSingBoxConfig json.RawMessage `json:"originalSingBoxConfig,omitempty"`
}

func (m *Manager) RunRegression(ctx context.Context, confirmed, includeNetworkDrop bool) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !confirmed {
		return errors.New("live regression changes network state; rerun with --yes from a VM snapshot")
	}
	if !fileExists(m.Paths.Committed) {
		return errors.New("live regression requires a committed PF Kill Switch")
	}
	if fileExists(m.Paths.RegressionState) {
		return errors.New("a regression recovery is already armed; run regression recover")
	}
	scenarios := []func(context.Context) error{m.regressionSingBox, m.regressionDNS, m.regressionVPS}
	for _, scenario := range scenarios {
		if err := scenario(ctx); err != nil {
			return err
		}
	}
	if includeNetworkDrop {
		if err := m.regressionNetworkDrop(ctx); err != nil {
			return err
		}
	}
	m.printf("All requested live failure regressions passed.\n")
	return nil
}

func (m *Manager) regressionSingBox(ctx context.Context) (err error) {
	m.printf("scenario=sing-box-crash status=running\n")
	if err := m.armRegressionRecovery(ctx, regressionState{Version: 1, Scenario: "sing-box-crash"}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.RecoverRegression(context.Background(), 0)) }()
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+singBoxLabel)
	time.Sleep(3 * time.Second)
	if m.runOK(ctx, "/usr/bin/pgrep", "-x", "sing-box") {
		return errors.New("sing-box remained running after fault injection")
	}
	if err := m.assertFailClosed(ctx); err != nil {
		return err
	}
	if err := m.assertPublicProbeBlocked(ctx); err != nil {
		return err
	}
	m.printf("scenario=sing-box-crash status=passed\n")
	return nil
}

func (m *Manager) regressionDNS(ctx context.Context) (err error) {
	physical, err := m.physicalInterface(ctx)
	if err != nil {
		return err
	}
	service, err := m.networkService(ctx, physical)
	if err != nil {
		return err
	}
	m.printf("scenario=dns-corruption status=running\n")
	if err := m.armRegressionRecovery(ctx, regressionState{Version: 1, Scenario: "dns-corruption", NetworkService: service}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.RecoverRegression(context.Background(), 0)) }()
	if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setdnsservers", service, "8.8.8.8"); err != nil {
		return err
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	repaired := false
	for attempt := 0; attempt < 45; attempt++ {
		output, _ := m.run(ctx, "/usr/sbin/networksetup", "-getdnsservers", service)
		if len(cfg.ExpectedDNSServers) > 0 && strings.Contains(string(output), cfg.ExpectedDNSServers[0]) {
			repaired = true
			break
		}
		time.Sleep(time.Second)
	}
	if !repaired {
		return errors.New("controller did not repair corrupted DNS within 45 seconds")
	}
	if err := m.assertFailClosed(ctx); err != nil {
		return err
	}
	m.printf("scenario=dns-corruption status=passed\n")
	return nil
}

func (m *Manager) regressionVPS(ctx context.Context) (err error) {
	original, err := os.ReadFile(m.Paths.SingBoxConfig)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(original, &document); err != nil {
		return err
	}
	outbound, err := singconfig.VLESSOutbound(document)
	if err != nil {
		return err
	}
	outbound["server_port"] = 1
	broken, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	m.printf("scenario=vps-unavailable status=running\n")
	if err := m.armRegressionRecovery(ctx, regressionState{Version: 1, Scenario: "vps-unavailable", OriginalSingBoxConfig: original}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.RecoverRegression(context.Background(), 0)) }()
	if err := writeFileAtomic(m.Paths.SingBoxConfig, append(broken, '\n'), 0o600); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+singBoxLabel); err != nil {
		return err
	}
	time.Sleep(6 * time.Second)
	if err := m.assertFailClosed(ctx); err != nil {
		return err
	}
	if err := m.assertPublicProbeBlocked(ctx); err != nil {
		return err
	}
	m.printf("scenario=vps-unavailable status=passed\n")
	return nil
}

func (m *Manager) regressionNetworkDrop(ctx context.Context) (err error) {
	physical, err := m.physicalInterface(ctx)
	if err != nil {
		return err
	}
	service, err := m.networkService(ctx, physical)
	if err != nil {
		return err
	}
	m.printf("scenario=network-transition status=running warning=management-will-disconnect\n")
	if err := m.armRegressionRecovery(ctx, regressionState{Version: 1, Scenario: "network-transition", NetworkService: service}); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.RecoverRegression(context.Background(), 0)) }()
	if _, err := m.run(ctx, "/usr/sbin/networksetup", "-setnetworkserviceenabled", service, "off"); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	if err := m.assertFailClosed(ctx); err != nil {
		return err
	}
	m.printf("scenario=network-transition status=passed\n")
	return nil
}

func (m *Manager) armRegressionRecovery(ctx context.Context, state regressionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(m.Paths.RegressionState, append(data, '\n'), 0o600); err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + regressionRecoveryLabel + `</string>
<key>ProgramArguments</key><array><string>/usr/local/libexec/lightnode-controller</string><string>regression</string><string>recover</string><string>--delay-seconds</string><string>90</string></array>
<key>RunAtLoad</key><true/>
<key>StandardOutPath</key><string>/Library/Logs/LightNodeController/regression-recovery.log</string>
<key>StandardErrorPath</key><string>/Library/Logs/LightNodeController/regression-recovery.error.log</string>
</dict></plist>
`
	if err := writeFileAtomic(m.Paths.RegressionPlist, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = os.Chown(m.Paths.RegressionPlist, 0, 0)
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+regressionRecoveryLabel)
	if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.RegressionPlist); err != nil {
		return err
	}
	return nil
}

func (m *Manager) RecoverRegression(ctx context.Context, delaySeconds int) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if delaySeconds > 0 {
		time.Sleep(time.Duration(delaySeconds) * time.Second)
	}
	data, err := os.ReadFile(m.Paths.RegressionState)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state regressionState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Version != 1 {
		return fmt.Errorf("unsupported regression state version %d", state.Version)
	}
	if state.NetworkService != "" {
		_, _ = m.run(ctx, "/usr/sbin/networksetup", "-setnetworkserviceenabled", state.NetworkService, "on")
	}
	if len(state.OriginalSingBoxConfig) > 0 {
		if err := writeFileAtomic(m.Paths.SingBoxConfig, state.OriginalSingBoxConfig, 0o600); err != nil {
			return err
		}
	}
	var recoveryErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := m.StartTunnel(ctx); err == nil {
			recoveryErr = nil
			break
		} else {
			recoveryErr = err
			time.Sleep(3 * time.Second)
		}
	}
	if recoveryErr != nil {
		return recoveryErr
	}
	if err := m.RefreshDNS(ctx, false); err != nil {
		return err
	}
	if err := m.RefreshFirewall(ctx, false); err != nil {
		return err
	}
	if err := os.Remove(m.Paths.RegressionState); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+regressionRecoveryLabel)
	_ = os.Remove(m.Paths.RegressionPlist)
	m.printf("Regression recovery completed.\n")
	return nil
}

func (m *Manager) assertFailClosed(ctx context.Context) error {
	info, err := m.run(ctx, "/sbin/pfctl", "-s", "info")
	if err != nil || !strings.Contains(string(info), "Status: Enabled") {
		return errors.New("PF is not enabled during fault injection")
	}
	rules, err := m.run(ctx, "/sbin/pfctl", "-a", firewallAnchor, "-sr")
	if err != nil || !strings.Contains(string(rules), "block drop out quick inet all") || !strings.Contains(string(rules), "block drop out quick inet6 all") {
		return errors.New("fail-closed PF rules are missing during fault injection")
	}
	return nil
}

func (m *Manager) assertPublicProbeBlocked(ctx context.Context) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cfg.EgressProbeURL, nil)
	if err != nil {
		return err
	}
	response, err := m.HTTPClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 128))
	return errors.New("public probe succeeded while the Tokyo tunnel was unavailable")
}
