package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"lightnode-linux-controller/internal/config"
)

const HostStatusPath = "/run/lightnode-linux-controller/host-status.json"
const HostRefreshRequestPath = "/run/lightnode-linux-controller/refresh-request"

type Action struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Message    string     `json:"message"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type PublicConfig struct {
	ManagementAddress string `json:"managementAddress"`
	PhysicalInterface string `json:"physicalInterface"`
	ManagementCIDR    string `json:"managementCidr"`
	TunnelInterface   string `json:"tunnelInterface"`
	TunnelAddress     string `json:"tunnelAddress"`
	TunnelDNS         string `json:"tunnelDns"`
	VPSAddress        string `json:"vpsAddress"`
	VPSPort           int    `json:"vpsPort"`
	ExpectedEgress    string `json:"expectedEgress"`
	Provider          string `json:"provider"`
	Runtime           string `json:"runtime"`
}

type RuntimeSnapshot struct {
	Report
	ProviderState string       `json:"providerState"`
	Mode          string       `json:"mode"`
	Action        *Action      `json:"action,omitempty"`
	Configuration PublicConfig `json:"configuration"`
}

type ContainerRuntime struct {
	manager        Manager
	configPath     string
	cfg            config.Controller
	mu             sync.Mutex
	provider       *exec.Cmd
	providerDone   chan error
	action         *Action
	runtimeCtx     context.Context
	desiredRunning bool
}

func NewContainerRuntime(manager Manager, configPath string) (*ContainerRuntime, error) {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return nil, err
	}
	check := exec.Command(cfg.SingBoxBinary, "check", "-c", cfg.SingBoxConfig)
	if output, err := check.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sing-box configuration: %w: %s", err, string(output))
	}
	return &ContainerRuntime{manager: manager, configPath: configPath, cfg: cfg}, nil
}

func (r *ContainerRuntime) StartInitial(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("container runtime requires root")
	}
	r.mu.Lock()
	r.runtimeCtx, r.desiredRunning = ctx, true
	r.mu.Unlock()
	if err := r.startProvider(); err != nil {
		return err
	}
	go func() { <-ctx.Done(); _ = r.stopProvider() }()
	return nil
}

func (r *ContainerRuntime) startProvider() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.provider != nil {
		r.desiredRunning = true
		return nil
	}
	r.desiredRunning = true
	provider := exec.Command(r.cfg.SingBoxBinary, "run", "-c", r.cfg.SingBoxConfig)
	provider.Stdout, provider.Stderr = os.Stdout, os.Stderr
	if err := provider.Start(); err != nil {
		return fmt.Errorf("start sing-box: %w", err)
	}
	done := make(chan error, 1)
	r.provider, r.providerDone = provider, done
	go func() {
		err := provider.Wait()
		done <- err
		close(done)
		r.mu.Lock()
		if r.provider == provider {
			r.provider, r.providerDone = nil, nil
		}
		shouldRestart := r.desiredRunning && r.runtimeCtx != nil && r.runtimeCtx.Err() == nil
		r.mu.Unlock()
		if shouldRestart {
			time.Sleep(2 * time.Second)
			_ = r.startProvider()
			r.RequestHostRefresh()
		}
	}()
	return nil
}

func (r *ContainerRuntime) stopProvider() error {
	r.mu.Lock()
	r.desiredRunning = false
	provider, done := r.provider, r.providerDone
	r.mu.Unlock()
	if provider == nil {
		return nil
	}
	if err := provider.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(8 * time.Second):
		_ = provider.Process.Kill()
		<-done
		return nil
	}
}

func (r *ContainerRuntime) QueueAction(kind string) (Action, error) {
	if kind != "start" && kind != "pause" && kind != "restart" {
		return Action{}, errors.New("unsupported action")
	}
	r.mu.Lock()
	if r.action != nil && r.action.Status == "running" {
		current := *r.action
		r.mu.Unlock()
		return current, errors.New("another action is running")
	}
	now := time.Now().UTC()
	action := &Action{ID: fmt.Sprintf("%d", now.UnixNano()), Kind: kind, Status: "running", Phase: "executing", Message: actionMessage(kind, false), StartedAt: now}
	r.action = action
	copy := *action
	r.mu.Unlock()
	go r.executeAction(action)
	return copy, nil
}

func (r *ContainerRuntime) executeAction(action *Action) {
	defer r.RequestHostRefresh()
	var err error
	switch action.Kind {
	case "pause":
		err = r.stopProvider()
	case "start":
		err = r.startProvider()
		if err == nil {
			err = r.manager.waitForInterface(context.Background(), r.cfg.TunnelInterface, 15*time.Second)
		}
	case "restart":
		err = r.stopProvider()
		if err == nil {
			err = r.startProvider()
		}
		if err == nil {
			err = r.manager.waitForInterface(context.Background(), r.cfg.TunnelInterface, 15*time.Second)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	action.FinishedAt, action.Phase = &now, "finished"
	if err != nil {
		action.Status, action.Message = "failed", err.Error()
		return
	}
	action.Status, action.Message = "succeeded", actionMessage(action.Kind, true)
}

func (r *ContainerRuntime) RequestHostRefresh() {
	_ = os.WriteFile(HostRefreshRequestPath, []byte("refresh\n"), 0o644)
}

func actionMessage(kind string, finished bool) string {
	if finished {
		switch kind {
		case "pause":
			return "东京隧道已暂停，Kill Switch 保持生效"
		case "start":
			return "东京隧道已启动"
		default:
			return "东京隧道已重启"
		}
	}
	switch kind {
	case "pause":
		return "正在停止 sing-box"
	case "start":
		return "正在启动 sing-box"
	default:
		return "正在重启 sing-box"
	}
}

func (r *ContainerRuntime) Snapshot() RuntimeSnapshot {
	report := Report{CheckedAt: time.Now().UTC(), ExpectedEgress: r.cfg.ExpectedEgress}
	if data, err := os.ReadFile(HostStatusPath); err == nil {
		_ = json.Unmarshal(data, &report)
	}
	r.mu.Lock()
	providerRunning := r.provider != nil
	var action *Action
	if r.action != nil {
		copy := *r.action
		action = &copy
	}
	r.mu.Unlock()
	state := "paused"
	if providerRunning {
		state = "running"
	}
	return RuntimeSnapshot{Report: report, ProviderState: state, Mode: "docker", Action: action,
		Configuration: PublicConfig{ManagementAddress: r.cfg.ManagementAddress, PhysicalInterface: r.cfg.PhysicalInterface,
			ManagementCIDR: r.cfg.ManagementCIDR, TunnelInterface: r.cfg.TunnelInterface, TunnelAddress: r.cfg.TunnelAddress,
			TunnelDNS: r.cfg.TunnelDNS, VPSAddress: r.cfg.VPSAddress, VPSPort: r.cfg.VPSPort,
			ExpectedEgress: r.cfg.ExpectedEgress, Provider: "sing-box 1.14.0", Runtime: "Docker"}}
}

func (r *ContainerRuntime) Action(id string) (Action, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.action == nil || r.action.ID != id {
		return Action{}, false
	}
	return *r.action, true
}

func (r *ContainerRuntime) ContainerHealth(ctx context.Context) error {
	return probeContainerHealth(ctx, r.cfg)
}

func ContainerHealth(ctx context.Context, configPath string) error {
	cfg, err := config.ReadController(configPath)
	if err != nil {
		return err
	}
	return probeContainerHealth(ctx, cfg)
}

func probeContainerHealth(ctx context.Context, cfg config.Controller) error {
	if !interfaceExists(cfg.TunnelInterface) {
		return fmt.Errorf("TUN interface %s is missing", cfg.TunnelInterface)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.EgressProbeURL, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("egress probe returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 128))
	if err != nil {
		return err
	}
	if stringValue(body) != cfg.ExpectedEgress {
		return fmt.Errorf("unexpected egress %q", stringValue(body))
	}
	return nil
}

func stringValue(value []byte) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ') {
		value = value[:len(value)-1]
	}
	return string(value)
}
