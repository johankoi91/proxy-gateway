package sysmanage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const singBoxLabel = "com.lightnode.sing-box"

func (m *Manager) StartTunnel(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	placeholder, err := containsPlaceholder(m.Paths.SingBoxConfig)
	if err != nil {
		return err
	}
	if placeholder {
		return errors.New("replace all VLESS/Reality placeholders before starting")
	}
	if _, err := m.run(ctx, m.Paths.SingBoxBinary, "check", "-c", m.Paths.SingBoxConfig); err != nil {
		return err
	}
	if _, err := m.run(ctx, "/bin/launchctl", "enable", "system/"+singBoxLabel); err != nil {
		return err
	}
	if !m.runOK(ctx, "/bin/launchctl", "print", "system/"+singBoxLabel) {
		if _, err := m.run(ctx, "/bin/launchctl", "bootstrap", "system", m.Paths.SingBoxPlist); err != nil {
			return err
		}
	}
	if _, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+singBoxLabel); err != nil {
		return err
	}

	ready := false
	for attempt := 0; attempt < 15; attempt++ {
		processOK := m.runOK(ctx, "/usr/bin/pgrep", "-x", "sing-box")
		interfaces, _ := m.run(ctx, "/sbin/ifconfig")
		if processOK && tunnelInterfaceForAddress(string(interfaces), "172.19.0.1") != "" {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		return errors.New("TUN did not become ready")
	}
	if err := m.RefreshDNS(ctx, false); err != nil {
		return err
	}
	if fileExists(m.Paths.Committed) {
		if err := m.RefreshFirewall(ctx, false); err != nil {
			return err
		}
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	observed, err := m.probeExpectedEgress(ctx, cfg.EgressProbeURL, cfg.ExpectedEgressIP, 3)
	if err != nil {
		return err
	}
	if err := os.Remove(m.Paths.Paused); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.log(ctx, "Tokyo tunnel started and verified at "+observed)
	m.printf("Tokyo protection started. Egress: %s\n", observed)
	return nil
}

func (m *Manager) PauseTunnel(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if !fileExists(m.Paths.Committed) {
		return errors.New("PF Kill Switch is not committed; refusing to stop the tunnel")
	}
	if err := os.MkdirAll(m.Paths.AppDir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(m.Paths.Paused, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return err
	}
	m.quitProtectedApps(ctx)
	if _, err := m.run(ctx, "/bin/launchctl", "disable", "system/"+singBoxLabel); err != nil {
		return err
	}
	_, _ = m.run(ctx, "/bin/launchctl", "bootout", "system/"+singBoxLabel)
	if m.runOK(ctx, "/usr/bin/pgrep", "-x", "sing-box") {
		return errors.New("sing-box is still running after launchd bootout")
	}
	m.log(ctx, "Tokyo tunnel safely paused; PF remains fail-closed")
	m.printf("Tokyo tunnel paused. PF Kill Switch remains active.\n")
	return nil
}

func (m *Manager) quitProtectedApps(ctx context.Context) {
	for _, app := range []struct {
		name    string
		pattern string
	}{
		{name: "Claude", pattern: `/Claude\.app/`},
		{name: "ChatGPT", pattern: `/ChatGPT\.app/`},
		{name: "ChatGPT Classic", pattern: `/ChatGPT Classic\.app/`},
	} {
		m.quitGUIApp(ctx, app.name, app.pattern)
	}
}

func (m *Manager) RestartTunnel(ctx context.Context) error {
	if err := m.requireRoot(); err != nil {
		return err
	}
	if fileExists(m.Paths.Paused) {
		return errors.New("protection is safely paused; use tunnel start")
	}
	_, err := m.run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+singBoxLabel)
	return err
}

func (m *Manager) quitGUIApp(ctx context.Context, appName, pattern string) {
	if !m.runOK(ctx, "/usr/bin/pgrep", "-f", pattern) {
		return
	}
	uid, ok := consoleUID()
	if !ok {
		m.printf("WARNING: %s is running, but no graphical user session is available for a normal quit.\n", appName)
		m.log(ctx, appName+" normal quit skipped: no graphical user session")
		return
	}
	script := fmt.Sprintf("tell application %s to quit", strconv.Quote(appName))
	if _, err := m.run(ctx, "/bin/launchctl", "asuser", strconv.FormatUint(uint64(uid), 10), "/usr/bin/osascript", "-e", script); err != nil {
		m.printf("WARNING: %s did not accept the normal quit request.\n", appName)
		m.log(ctx, appName+" did not accept the normal quit request")
		return
	}
	for attempt := 0; attempt < 5; attempt++ {
		if !m.runOK(ctx, "/usr/bin/pgrep", "-f", pattern) {
			m.printf("INFO: %s exited normally before the Tokyo tunnel stopped.\n", appName)
			m.log(ctx, appName+" exited normally before Tokyo tunnel pause")
			return
		}
		time.Sleep(time.Second)
	}
	m.printf("WARNING: %s is still running after the normal quit request; it was not force-quit.\n", appName)
	m.log(ctx, appName+" remained running after normal quit; no force-quit was used")
}

func (m *Manager) probeEgress(ctx context.Context, rawURL string) (string, error) {
	request, err := httpRequest(ctx, rawURL)
	if err != nil {
		return "", err
	}
	response, err := m.HTTPClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("egress probe returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 128))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (m *Manager) probeExpectedEgress(ctx context.Context, rawURL, expected string, attempts int) (string, error) {
	var observed string
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		observed, lastErr = m.probeEgress(probeCtx, rawURL)
		cancel()
		if lastErr == nil && observed == expected {
			return observed, nil
		}
		if attempt+1 < attempts {
			time.Sleep(time.Second)
		}
	}
	if lastErr != nil {
		return observed, fmt.Errorf("egress probe failed after %d attempts: %w", attempts, lastErr)
	}
	return observed, fmt.Errorf("egress is %s, expected %s", observed, expected)
}
