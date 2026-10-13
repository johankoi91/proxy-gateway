package sysmanage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lightnode-mac-controller/internal/config"
)

const (
	managedBegin = "# BEGIN LIGHTNODE VLESS KILL SWITCH"
	managedEnd   = "# END LIGHTNODE VLESS KILL SWITCH"
)

var networkServiceHeaderPattern = regexp.MustCompile(`^\([0-9]+\) `)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type Paths struct {
	AppDir, LogDir, TemplatesDir, DocsDir           string
	Config, SingBoxConfig                           string
	Paused, Committed, RollbackState                string
	PFConfig, PFAnchor, RollbackPlist               string
	ControllerPlist, SingBoxPlist, PFBootstrapPlist string
	ControllerBinary, SingBoxBinary                 string
	RegressionState, RegressionPlist                string
}

func DefaultPaths() Paths {
	app := "/Library/Application Support/LightNodeController"
	return Paths{
		AppDir: app, LogDir: "/Library/Logs/LightNodeController",
		TemplatesDir: "/usr/local/share/lightnode-controller/templates",
		DocsDir:      "/usr/local/share/doc/lightnode-controller",
		Config:       filepath.Join(app, "config.json"), SingBoxConfig: filepath.Join(app, "sing-box.json"),
		Paused: filepath.Join(app, "safely-paused"), Committed: filepath.Join(app, "firewall.committed"),
		RollbackState: filepath.Join(app, "pf-rollback"),
		PFConfig:      "/etc/pf.conf", PFAnchor: "/etc/pf.anchors/com.lightnode.vless-killswitch",
		RollbackPlist:    "/Library/LaunchDaemons/com.lightnode.vless-killswitch-rollback.plist",
		ControllerPlist:  "/Library/LaunchDaemons/com.lightnode.controller.plist",
		SingBoxPlist:     "/Library/LaunchDaemons/com.lightnode.sing-box.plist",
		PFBootstrapPlist: "/Library/LaunchDaemons/com.lightnode.pf-bootstrap.plist",
		ControllerBinary: "/usr/local/libexec/lightnode-controller", SingBoxBinary: "/usr/local/libexec/sing-box",
		RegressionState: filepath.Join(app, "regression-state.json"), RegressionPlist: "/Library/LaunchDaemons/com.lightnode.regression-recovery.plist",
	}
}

type Manager struct {
	Runner     Runner
	Paths      Paths
	Stdout     io.Writer
	Stderr     io.Writer
	EUID       func() int
	HTTPClient *http.Client
}

func New(runner Runner) *Manager {
	return &Manager{
		Runner: runner, Paths: DefaultPaths(), Stdout: os.Stdout, Stderr: os.Stderr,
		EUID:       os.Geteuid,
		HTTPClient: &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}
}

func (m *Manager) requireRoot() error {
	if m.EUID != nil && m.EUID() != 0 {
		return errors.New("run with sudo")
	}
	return nil
}

func (m *Manager) loadConfig() (config.Config, error) { return config.Load(m.Paths.Config) }

func (m *Manager) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.Runner == nil {
		return nil, errors.New("system command runner is not configured")
	}
	return m.Runner.Run(ctx, name, args...)
}

func (m *Manager) runOK(ctx context.Context, name string, args ...string) bool {
	_, err := m.run(ctx, name, args...)
	return err == nil
}

func (m *Manager) printf(format string, args ...any) {
	if m.Stdout != nil {
		_, _ = fmt.Fprintf(m.Stdout, format, args...)
	}
}

func (m *Manager) log(ctx context.Context, message string) {
	_, _ = m.run(ctx, "/usr/bin/logger", "-t", "lightnode-controller", message)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsPlaceholder(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Contains(data, []byte("REPLACE_")), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".lightnode-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func copyFile(source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeFileAtomic(destination, data, mode)
}

func randomToken(bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func consoleUID() (uint32, bool) {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat.Uid, ok && stat.Uid != 0
}

func firstInterface(nwi string) string {
	for _, line := range strings.Split(nwi, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "Network" && fields[1] == "interfaces:" {
			for _, candidate := range fields[2:] {
				if candidate != "lo0" && !strings.HasPrefix(candidate, "utun") {
					return candidate
				}
			}
		}
	}
	return ""
}

func networkServiceForInterface(output, device string) string {
	service := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if networkServiceHeaderPattern.MatchString(trimmed) {
			if index := strings.Index(trimmed, ") "); index >= 0 {
				service = strings.TrimPrefix(trimmed[index+2:], "*")
			}
			continue
		}
		if service != "" && strings.Contains(trimmed, "Device: "+device+")") {
			return service
		}
	}
	return ""
}

func routeInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "interface:" {
			return fields[1]
		}
	}
	return ""
}

func tunnelInterfaceForAddress(output, address string) string {
	current := ""
	for _, line := range strings.Split(output, "\n") {
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			current = strings.TrimSuffix(strings.Fields(line)[0], ":")
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "inet" && fields[1] == address {
			return current
		}
	}
	return ""
}

func managementCIDR(localIP, maskHex string) (string, error) {
	ip := net.ParseIP(localIP).To4()
	if ip == nil {
		return "", fmt.Errorf("invalid local IPv4 %q", localIP)
	}
	maskHex = strings.TrimPrefix(maskHex, "0x")
	maskValue, err := strconv.ParseUint(maskHex, 16, 32)
	if err != nil || len(maskHex) != 8 {
		return "", fmt.Errorf("invalid IPv4 mask %q", maskHex)
	}
	mask := net.IPMask{byte(maskValue >> 24), byte(maskValue >> 16), byte(maskValue >> 8), byte(maskValue)}
	ones, bits := mask.Size()
	if bits != 32 || ones == 0 {
		return "", fmt.Errorf("non-contiguous IPv4 mask %q", maskHex)
	}
	network := ip.Mask(mask)
	return (&net.IPNet{IP: network, Mask: mask}).String(), nil
}

func removeManagedPFBlock(input string) string {
	lines := strings.Split(input, "\n")
	result := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case managedBegin:
			skipping = true
		case managedEnd:
			skipping = false
		default:
			if !skipping {
				result = append(result, line)
			}
		}
	}
	return strings.TrimRight(strings.Join(result, "\n"), "\n") + "\n"
}

func parseInterfaceIPv4(output string) (string, string) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "inet" {
			return fields[1], fields[3]
		}
	}
	return "", ""
}

func httpRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
}
