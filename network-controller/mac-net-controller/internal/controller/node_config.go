package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/model"
	"lightnode-mac-controller/internal/singconfig"
)

var (
	uuidPattern        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	realityKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{32,64}$`)
	shortIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{2,16}$`)
	hostnamePattern    = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)
	fingerprintPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
)

func (c *Controller) NodeConfiguration() (model.NodeConfiguration, error) {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()
	return c.readNodeConfiguration()
}

func (c *Controller) readNodeConfiguration() (model.NodeConfiguration, error) {
	cfg, err := config.Load(c.cfg.ConfigFilePath)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	data, err := os.ReadFile(cfg.SingBoxConfigFile)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return model.NodeConfiguration{}, err
	}
	outbound, err := singconfig.VLESSOutbound(document)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	tls := singconfig.Object(outbound, "tls")
	reality := singconfig.Object(tls, "reality")
	utls := singconfig.Object(tls, "utls")
	return model.NodeConfiguration{
		Server: singconfig.String(outbound["server"]), Port: singconfig.Int(outbound["server_port"]),
		ExpectedEgress: cfg.ExpectedEgressIP, Flow: singconfig.String(outbound["flow"]),
		TLSServerName: singconfig.String(tls["server_name"]), UTLSFingerprint: singconfig.String(utls["fingerprint"]),
		UUIDConfigured:             configuredSecret(singconfig.String(outbound["uuid"])),
		RealityPublicKeyConfigured: configuredSecret(singconfig.String(reality["public_key"])),
		RealityShortIDConfigured:   configuredSecret(singconfig.String(reality["short_id"])),
	}, nil
}

func configuredSecret(value string) bool {
	return value != "" && !strings.HasPrefix(value, "REPLACE_")
}

func (c *Controller) UpdateNode(parent context.Context, update model.NodeUpdate) (model.NodeConfiguration, error) {
	if !c.operationMu.TryLock() {
		return model.NodeConfiguration{}, errors.New("另一个网络操作正在执行")
	}
	defer c.operationMu.Unlock()
	if err := validateNodeUpdate(update); err != nil {
		return model.NodeConfiguration{}, err
	}
	if err := c.authorizeNodeUpdate(parent); err != nil {
		return model.NodeConfiguration{}, err
	}
	c.checkMu.Lock()
	defer c.checkMu.Unlock()

	controllerPath := c.cfg.ConfigFilePath
	singPath := c.cfg.SingBoxConfigFile
	oldController, err := os.ReadFile(controllerPath)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	oldSing, err := os.ReadFile(singPath)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	if _, err := c.createBackupFromDocumentsLocked("before-node-update", oldController, oldSing); err != nil {
		return model.NodeConfiguration{}, fmt.Errorf("create node safety backup: %w", err)
	}
	newController, newSing, err := buildNodeCandidates(oldController, oldSing, update)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	if err := c.validateSingBoxCandidate(parent, newSing, singPath); err != nil {
		return model.NodeConfiguration{}, err
	}
	if err := writeNodeFileAtomic(singPath, newSing); err != nil {
		return model.NodeConfiguration{}, err
	}
	if err := writeNodeFileAtomic(controllerPath, newController); err != nil {
		_ = writeNodeFileAtomic(singPath, oldSing)
		return model.NodeConfiguration{}, err
	}

	rollback := func(cause error) (model.NodeConfiguration, error) {
		_ = writeNodeFileAtomic(singPath, oldSing)
		_ = writeNodeFileAtomic(controllerPath, oldController)
		_, _ = c.runner.Run(context.Background(), "/usr/local/libexec/lightnode-controller", "firewall", "refresh")
		_, _ = c.runner.Run(context.Background(), "/usr/local/libexec/lightnode-controller", "tunnel", "start")
		return model.NodeConfiguration{}, cause
	}
	if _, err := c.runner.Run(parent, "/usr/local/libexec/lightnode-controller", "firewall", "refresh"); err != nil {
		return rollback(fmt.Errorf("refresh PF for new node: %w", err))
	}
	if _, err := c.runner.Run(parent, "/usr/local/libexec/lightnode-controller", "tunnel", "start"); err != nil {
		return rollback(fmt.Errorf("activate new node: %w", err))
	}
	result, err := nodeConfigurationFromDocuments(newController, newSing)
	if err != nil {
		return rollback(err)
	}
	c.scheduleControllerRestart()
	return result, nil
}

func (c *Controller) scheduleControllerRestart() {
	go func() {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = c.runner.Run(ctx, "/bin/launchctl", "kickstart", "-k", "system/"+c.cfg.LaunchDaemonLabel)
	}()
}

func validateNodeUpdate(update model.NodeUpdate) error {
	if net.ParseIP(strings.TrimSpace(update.Server)).To4() == nil {
		return errors.New("节点地址必须是 IPv4")
	}
	if update.Port < 1 || update.Port > 65535 {
		return errors.New("节点端口必须在 1 到 65535 之间")
	}
	if net.ParseIP(strings.TrimSpace(update.ExpectedEgress)).To4() == nil {
		return errors.New("期望出口必须是 IPv4")
	}
	if update.Flow != "xtls-rprx-vision" {
		return errors.New("当前仅支持 xtls-rprx-vision")
	}
	if !hostnamePattern.MatchString(update.TLSServerName) {
		return errors.New("TLS Server Name 无效")
	}
	if !fingerprintPattern.MatchString(update.UTLSFingerprint) {
		return errors.New("uTLS 指纹无效")
	}
	if update.UUID != "" && !uuidPattern.MatchString(update.UUID) {
		return errors.New("VLESS UUID 格式无效")
	}
	if update.RealityPublicKey != "" && !realityKeyPattern.MatchString(update.RealityPublicKey) {
		return errors.New("reality 公钥格式无效")
	}
	if update.RealityShortID != "" && (!shortIDPattern.MatchString(update.RealityShortID) || len(update.RealityShortID)%2 != 0) {
		return errors.New("reality short ID 必须是偶数长度十六进制")
	}
	return nil
}

func buildNodeCandidates(controllerData, singData []byte, update model.NodeUpdate) ([]byte, []byte, error) {
	var controllerDocument map[string]any
	if err := json.Unmarshal(controllerData, &controllerDocument); err != nil {
		return nil, nil, err
	}
	var singDocument map[string]any
	if err := json.Unmarshal(singData, &singDocument); err != nil {
		return nil, nil, err
	}
	outbound, err := singconfig.VLESSOutbound(singDocument)
	if err != nil {
		return nil, nil, err
	}
	outbound["server"] = strings.TrimSpace(update.Server)
	outbound["server_port"] = update.Port
	outbound["flow"] = update.Flow
	if update.UUID != "" {
		outbound["uuid"] = update.UUID
	}
	tls := singconfig.Object(outbound, "tls")
	tls["server_name"] = update.TLSServerName
	utls := singconfig.Object(tls, "utls")
	utls["enabled"] = true
	utls["fingerprint"] = update.UTLSFingerprint
	reality := singconfig.Object(tls, "reality")
	if update.RealityPublicKey != "" {
		reality["public_key"] = update.RealityPublicKey
	}
	if update.RealityShortID != "" {
		reality["short_id"] = update.RealityShortID
	}
	for name, value := range map[string]string{
		"uuid": singconfig.String(outbound["uuid"]), "public_key": singconfig.String(reality["public_key"]), "short_id": singconfig.String(reality["short_id"]),
	} {
		if !configuredSecret(value) {
			return nil, nil, fmt.Errorf("%s 尚未配置", name)
		}
	}
	if err := singconfig.EnforceGlobalTokyo(singDocument); err != nil {
		return nil, nil, err
	}
	controllerDocument["vpsAddress"] = strings.TrimSpace(update.Server)
	controllerDocument["vpsPort"] = update.Port
	controllerDocument["expectedEgressIp"] = strings.TrimSpace(update.ExpectedEgress)
	newController, err := json.MarshalIndent(controllerDocument, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	newSing, err := json.MarshalIndent(singDocument, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(newController, '\n'), append(newSing, '\n'), nil
}

func nodeConfigurationFromDocuments(controllerData, singData []byte) (model.NodeConfiguration, error) {
	var controllerDocument map[string]any
	if err := json.Unmarshal(controllerData, &controllerDocument); err != nil {
		return model.NodeConfiguration{}, err
	}
	var singDocument map[string]any
	if err := json.Unmarshal(singData, &singDocument); err != nil {
		return model.NodeConfiguration{}, err
	}
	outbound, err := singconfig.VLESSOutbound(singDocument)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	tls := singconfig.Object(outbound, "tls")
	reality := singconfig.Object(tls, "reality")
	utls := singconfig.Object(tls, "utls")
	return model.NodeConfiguration{
		Server: singconfig.String(outbound["server"]), Port: singconfig.Int(outbound["server_port"]),
		ExpectedEgress: singconfig.String(controllerDocument["expectedEgressIp"]), Flow: singconfig.String(outbound["flow"]),
		TLSServerName: singconfig.String(tls["server_name"]), UTLSFingerprint: singconfig.String(utls["fingerprint"]),
		UUIDConfigured: configuredSecret(singconfig.String(outbound["uuid"])), RealityPublicKeyConfigured: configuredSecret(singconfig.String(reality["public_key"])),
		RealityShortIDConfigured: configuredSecret(singconfig.String(reality["short_id"])),
	}, nil
}

func (c *Controller) validateSingBoxCandidate(ctx context.Context, data []byte, destination string) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".node-candidate-*.json")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := c.runner.Run(ctx, "/usr/local/libexec/sing-box", "check", "-c", path); err != nil {
		return fmt.Errorf("候选 sing-box 配置校验失败: %w", err)
	}
	return nil
}

func writeNodeFileAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".node-write-*.json")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (c *Controller) authorizeNodeUpdate(ctx context.Context) error {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return errors.New("无法确定当前图形用户")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 {
		return errors.New("没有可用的图形用户会话")
	}
	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	user := "#" + uid
	script := `do shell script "/usr/bin/true" with administrator privileges`
	if _, err := c.runner.Run(ctx, "/bin/launchctl", "asuser", uid, "/usr/bin/sudo", "-u", user, "/usr/bin/osascript", "-e", script); err != nil {
		return fmt.Errorf("管理员授权未完成: %w", err)
	}
	return nil
}
