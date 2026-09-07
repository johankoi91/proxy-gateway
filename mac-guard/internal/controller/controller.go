package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/model"
	"lightnode-mac-controller/internal/observer"
)

type Controller struct {
	cfg                 config.Config
	runner              Runner
	checker             *checker
	version             string
	mu                  sync.RWMutex
	logMu               sync.Mutex
	checkMu             sync.Mutex
	operationMu         sync.Mutex
	actionMu            sync.RWMutex
	snapshot            model.Snapshot
	events              []model.Event
	eventID             atomic.Int64
	actionID            atomic.Int64
	lastPFRepair        atomic.Int64
	lastDNSRepair       atomic.Int64
	lastConflictRepair  atomic.Int64
	lastConflictTrigger atomic.Int64
	startedAt           time.Time
	connectionObserver  *observer.Observer
	observerInitError   string
	currentAction       *model.LifecycleAction
	actions             map[string]model.LifecycleAction
	actionOrder         []string
}

func NewController(cfg config.Config, runner Runner, version string) *Controller {
	now := time.Now().UTC()
	controller := &Controller{
		cfg:       cfg,
		runner:    runner,
		checker:   newChecker(cfg, runner),
		version:   version,
		startedAt: now,
		actions:   make(map[string]model.LifecycleAction),
		snapshot: model.Snapshot{
			Version: version, State: model.StateBootLocked, StateSince: now,
			Provider: cfg.ProviderName, ExpectedEgress: cfg.ExpectedEgressIP,
			RuntimeConfig: runtimeConfiguration(cfg),
		},
	}
	controller.rotateLogs()
	controller.loadEvents()
	controller.loadLifecycleAction()
	connectionObserver, err := observer.New(observer.Options{
		APIURL: cfg.ConnectionAPIURL, APISecret: cfg.ConnectionAPISecret,
		PollInterval: time.Duration(cfg.ConnectionPollIntervalSeconds) * time.Second,
		OnViolation:  controller.connectionViolationObserved,
	})
	if err != nil {
		controller.observerInitError = err.Error()
		log.Printf("[error] connection_observer: %v", err)
	} else {
		controller.connectionObserver = connectionObserver
	}
	return controller
}

func (c *Controller) Start(ctx context.Context) {
	go func() {
		c.resumeLifecycleAction()
		c.CheckNow(ctx)
		ticker := time.NewTicker(c.cfg.CheckInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.CheckNow(ctx)
			}
		}
	}()
	if c.cfg.ConflictAutoRemediate {
		go c.watchNetworkConflicts(ctx)
	}
	if c.connectionObserver != nil {
		c.connectionObserver.Start(ctx)
	}
}

func (c *Controller) watchNetworkConflicts(ctx context.Context) {
	interval := time.Duration(c.cfg.ConflictCheckIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, c.cfg.CheckTimeout())
			component := c.checker.checkNetworkConflict(probeCtx, time.Now().UTC())
			cancel()
			if component.Health == model.HealthHealthy {
				continue
			}
			now := time.Now().Unix()
			previous := c.lastConflictTrigger.Load()
			if now-previous >= 10 && c.lastConflictTrigger.CompareAndSwap(previous, now) {
				go c.CheckNow(ctx)
			}
		}
	}
}

func (c *Controller) CheckNow(parent context.Context) model.Snapshot {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()
	c.rotateLogs()
	c.setChecking(true)
	ctx, cancel := context.WithTimeout(parent, c.cfg.CheckTimeout()*6)
	defer cancel()
	output := c.checker.run(ctx)
	c.apply(output)
	c.reconcileAfterCheck(output)
	c.setChecking(false)
	return c.Snapshot()
}

func (c *Controller) reconcileAfterCheck(output checkOutput) {
	if c.safelyPaused() {
		c.maybeRepairFirewall(output)
		if c.Snapshot().State == model.StateInconsistentLocked {
			_, _ = c.QueueLifecycleAction("pause")
		}
		return
	}
	if componentIsUnhealthy(output, "network_conflict") {
		c.maybeRepairNetworkConflict(output)
	} else {
		c.maybeRepairFirewall(output)
		c.maybeRepairDNS(output)
	}
}

func (c *Controller) safelyPaused() bool {
	if c.cfg.MaintenanceFile == "" {
		return false
	}
	_, err := os.Stat(c.cfg.MaintenanceFile)
	return err == nil
}

func componentIsUnhealthy(output checkOutput, id string) bool {
	for _, component := range output.components {
		if component.ID == id {
			return component.Health != model.HealthHealthy
		}
	}
	return true
}

func (c *Controller) maybeRepairNetworkConflict(output checkOutput) {
	if !c.cfg.ConflictAutoRemediate || len(c.cfg.ConflictRemediationCommand) == 0 || c.cfg.FirewallCommittedFile == "" {
		return
	}
	if _, err := os.Stat(c.cfg.FirewallCommittedFile); err != nil || !componentIsUnhealthy(output, "network_conflict") {
		return
	}
	now := time.Now().Unix()
	previous := c.lastConflictRepair.Load()
	if now-previous < 30 || !c.lastConflictRepair.CompareAndSwap(previous, now) {
		return
	}
	command := append([]string(nil), c.cfg.ConflictRemediationCommand...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := c.runner.Run(ctx, command[0], command[1:]...)
		c.mu.Lock()
		if err != nil {
			c.addEventLocked("critical", "network_conflict", "代理/VPN 自动隔离失败: "+err.Error())
		} else {
			c.addEventLocked("warning", "network_conflict", "已关闭已知冲突通道并恢复 LightNode 网络边界")
		}
		c.mu.Unlock()
		if err == nil {
			time.Sleep(2 * time.Second)
			c.CheckNow(context.Background())
		}
	}()
}

func (c *Controller) maybeRepairDNS(output checkOutput) {
	if len(c.cfg.DNSRepairCommand) == 0 || c.cfg.FirewallCommittedFile == "" {
		return
	}
	if _, err := os.Stat(c.cfg.FirewallCommittedFile); err != nil {
		return
	}
	health := make(map[string]model.Health, len(output.components))
	for _, component := range output.components {
		health[component.ID] = component.Health
	}
	if health["provider"] != model.HealthHealthy || health["tunnel"] != model.HealthHealthy || health["dns"] == model.HealthHealthy {
		return
	}
	now := time.Now().Unix()
	previous := c.lastDNSRepair.Load()
	if now-previous < 30 || !c.lastDNSRepair.CompareAndSwap(previous, now) {
		return
	}
	command := append([]string(nil), c.cfg.DNSRepairCommand...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := c.runner.Run(ctx, command[0], command[1:]...)
		c.mu.Lock()
		if err != nil {
			c.addEventLocked("warning", "dns", "DNS 自动修复失败: "+err.Error())
		} else {
			c.addEventLocked("warning", "dns", "已为当前网络服务重新设置隧道 DNS")
		}
		c.mu.Unlock()
		if err == nil {
			time.Sleep(time.Second)
			c.CheckNow(context.Background())
		}
	}()
}

func (c *Controller) maybeRepairFirewall(output checkOutput) {
	if len(c.cfg.FirewallRepairCommand) == 0 || c.cfg.FirewallCommittedFile == "" {
		return
	}
	if _, err := os.Stat(c.cfg.FirewallCommittedFile); err != nil {
		return
	}
	failed := false
	for _, component := range output.components {
		if strings.HasPrefix(component.ID, "firewall") && component.Health != model.HealthHealthy {
			failed = true
			break
		}
	}
	if !failed {
		return
	}
	now := time.Now().Unix()
	previous := c.lastPFRepair.Load()
	if now-previous < 30 || !c.lastPFRepair.CompareAndSwap(previous, now) {
		return
	}
	command := append([]string(nil), c.cfg.FirewallRepairCommand...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := c.runner.Run(ctx, command[0], command[1:]...)
		c.mu.Lock()
		if err != nil {
			c.addEventLocked("critical", "firewall", "PF 自动修复失败: "+err.Error())
		} else {
			c.addEventLocked("warning", "firewall", "PF 异常后已重新加载受控 Anchor")
		}
		c.mu.Unlock()
		if err == nil {
			time.Sleep(time.Second)
			c.CheckNow(context.Background())
		}
	}()
}

func (c *Controller) Snapshot() model.Snapshot {
	c.mu.RLock()
	b, _ := json.Marshal(c.snapshot)
	c.mu.RUnlock()
	var out model.Snapshot
	_ = json.Unmarshal(b, &out)
	out.Action = c.CurrentLifecycleAction()
	if c.connectionObserver != nil {
		connectionSnapshot := c.connectionObserver.Snapshot()
		out.Connections = connectionSnapshot
		if out.State != model.StateMaintenance {
			out.Alerts = append(connectionAlerts(connectionSnapshot), out.Alerts...)
		}
	} else {
		out.Connections = model.ConnectionObservation{Health: model.HealthFailed, Message: c.observerInitError}
		out.Alerts = append([]model.Alert{{
			ID: "connection_observer_init", Severity: "warning", Title: "连接观测初始化失败",
			Message: c.observerInitError, Component: "connection_observer", RaisedAt: time.Now().UTC(),
		}}, out.Alerts...)
	}
	return out
}

func connectionAlerts(snapshot model.ConnectionObservation) []model.Alert {
	now := time.Now().UTC()
	alerts := make([]model.Alert, 0, 3)
	if snapshot.Health == model.HealthFailed {
		alerts = append(alerts, model.Alert{
			ID: "connection_observer", Severity: "warning", Title: "连接观测异常",
			Message: snapshot.Message, Component: "connection_observer", RaisedAt: now,
		})
	}
	if snapshot.ViolationCount > 0 {
		alerts = append(alerts, model.Alert{
			ID: "connection_violation", Severity: "critical", Title: "东京出口违例",
			Message:   fmt.Sprintf("发现 %d 条活动连接未经过期望出口。", snapshot.ViolationCount),
			Component: "connection_observer", RaisedAt: now,
		})
	}
	return alerts
}

func (c *Controller) connectionViolationObserved(connection model.ObservedConnection) {
	target := connection.Host
	if target == "" {
		target = connection.DestinationIP
	}
	c.mu.Lock()
	c.addEventLocked("critical", "connection_observer", fmt.Sprintf("东京出口违例: %s -> %s 未经过 tokyo 出站", filepath.Base(connection.ProcessPath), target))
	c.mu.Unlock()
}

func (c *Controller) snapshotProtected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot.Protected
}

func (c *Controller) apply(output checkOutput) {
	now := time.Now().UTC()
	byID := make(map[string]model.Component, len(output.components))
	for _, component := range output.components {
		byID[component.ID] = component
	}
	protected := isHealthy(byID["firewall"])
	connected := allHealthy(byID, "network_conflict", "firewall_config", "firewall_binding", "pf_bootstrap_service", "provider", "provider_service", "controller_service", "tunnel", "transport", "egress", "dns", "config")
	if c.connectionObserver != nil {
		connectionSnapshot := c.connectionObserver.Snapshot()
		if connectionSnapshot.ViolationCount > 0 {
			connected = false
		}
	}
	paused := c.safelyPaused()
	pausedRuntimeActive := paused && (!output.providerDisabled || isHealthy(byID["provider"]) || isHealthy(byID["tunnel"]) || isHealthy(byID["egress"]))
	state := model.StateHealthy
	switch {
	case !protected:
		state = model.StateUnprotected
	case pausedRuntimeActive:
		state = model.StateInconsistentLocked
		connected = false
	case paused:
		state = model.StateMaintenance
		connected = false
	case protected && !connected:
		state = model.StateDegradedLocked
	case protected && connected:
		state = model.StateHealthy
	}
	stages := buildStages(byID)
	alerts := buildAlerts(byID, state, now)
	if state == model.StateInconsistentLocked {
		alerts = append([]model.Alert{{
			ID: "lifecycle_inconsistent", Severity: "critical", Title: "暂停状态不一致",
			Message:   "安全暂停标记存在，但 sing-box、TUN 或东京出口仍在运行；系统正在重新执行暂停。",
			Component: "controller", RaisedAt: now,
		}}, alerts...)
	}
	if state == model.StateMaintenance {
		alerts = []model.Alert{{
			ID: "safely_paused", Severity: "info", Title: "东京保护已安全暂停",
			Message: "PF Kill Switch 仍在阻断本地公网回落。", Component: "provider", RaisedAt: now,
		}}
	}
	runtimeConfig := runtimeConfiguration(c.cfg)
	c.mu.Lock()
	defer c.mu.Unlock()
	previousComponents := make(map[string]model.Component, len(c.snapshot.Components))
	for _, component := range c.snapshot.Components {
		previousComponents[component.ID] = component
	}
	for _, component := range output.components {
		if state == model.StateMaintenance && expectedPausedComponent(component.ID) {
			continue
		}
		previous, ok := previousComponents[component.ID]
		if !ok && (component.Health == model.HealthFailed || component.Health == model.HealthDegraded) {
			severity := "warning"
			if component.Health == model.HealthFailed && component.Required {
				severity = "critical"
			}
			c.addEventLocked(severity, component.ID, component.Name+": "+component.Summary)
		} else if ok && previous.Health != component.Health {
			severity := "info"
			if component.Health == model.HealthFailed || component.Health == model.HealthDegraded {
				severity = "warning"
				if component.Health == model.HealthFailed && component.Required {
					severity = "critical"
				}
			}
			c.addEventLocked(severity, component.ID, fmt.Sprintf("%s: %s -> %s", component.Name, previous.Health, component.Health))
		}
	}
	if c.snapshot.LastCheckAt.IsZero() {
		c.addEventLocked("info", "controller", "首次组件检查完成")
	}
	if c.snapshot.State != state {
		c.transitionLocked(state, "controller", fmt.Sprintf("保护状态变更: %s -> %s", c.snapshot.State, state))
	}
	c.snapshot.Version = c.version
	c.snapshot.State = state
	c.snapshot.Protected = protected
	c.snapshot.Connected = connected
	c.snapshot.Provider = c.cfg.ProviderName
	c.snapshot.ExpectedEgress = c.cfg.ExpectedEgressIP
	c.snapshot.ObservedEgress = output.observedIP
	c.snapshot.PhysicalIF = output.physicalIF
	c.snapshot.TunnelIF = output.tunnelIF
	c.snapshot.LastCheckAt = now
	c.snapshot.NextCheckAt = now.Add(c.cfg.CheckInterval())
	c.snapshot.Components = output.components
	c.snapshot.Stages = stages
	c.snapshot.Tests = output.tests
	c.snapshot.Alerts = alerts
	c.snapshot.Events = append([]model.Event(nil), c.events...)
	c.snapshot.RuntimeConfig = runtimeConfig
}

func expectedPausedComponent(id string) bool {
	switch id {
	case "provider", "provider_service", "tunnel", "egress", "dns", "ipv6":
		return true
	default:
		return false
	}
}

func (c *Controller) setChecking(value bool) {
	c.mu.Lock()
	c.snapshot.Checking = value
	c.mu.Unlock()
}

func (c *Controller) transitionLocked(state model.ControllerState, component, message string) {
	if c.snapshot.State != state {
		c.snapshot.StateSince = time.Now().UTC()
	}
	c.snapshot.State = state
	severity := "info"
	if state == model.StateUnprotected {
		severity = "critical"
	} else if state == model.StateDegradedLocked || state == model.StateInconsistentLocked {
		severity = "warning"
	}
	c.addEventLocked(severity, component, message)
}

func (c *Controller) addEventLocked(severity, component, message string) {
	event := model.Event{ID: c.eventID.Add(1), Time: time.Now().UTC(), Severity: severity, Component: component, Message: message}
	c.events = append([]model.Event{event}, c.events...)
	if len(c.events) > c.cfg.EventLimit {
		c.events = c.events[:c.cfg.EventLimit]
	}
	c.snapshot.Events = append([]model.Event(nil), c.events...)
	log.Printf("[%s] %s: %s", severity, component, message)
	c.persistEvent(event)
	if (severity == "critical" || severity == "warning") && len(c.cfg.NotificationCommand) > 0 {
		command := append([]string(nil), c.cfg.NotificationCommand...)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = c.runner.Run(ctx, command[0], append(command[1:], severity, component, message)...)
		}()
	}
}

func (c *Controller) persistEvent(event model.Event) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if c.cfg.EventLogFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.EventLogFile), 0o750); err != nil {
		return
	}
	f, err := os.OpenFile(c.cfg.EventLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(event)
}

func (c *Controller) loadEvents() {
	if c.cfg.EventLogFile == "" {
		return
	}
	b, err := os.ReadFile(c.cfg.EventLogFile)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0 && len(c.events) < c.cfg.EventLimit; i-- {
		var event model.Event
		if json.Unmarshal([]byte(lines[i]), &event) == nil {
			if event.ID > c.eventID.Load() {
				c.eventID.Store(event.ID)
			}
			c.events = append(c.events, event)
		}
	}
	c.snapshot.Events = append([]model.Event(nil), c.events...)
}

func buildStages(byID map[string]model.Component) []model.Stage {
	return []model.Stage{
		stage("network", "网络边界", byID["network_conflict"]),
		stage("firewall", "PF 边界", byID["firewall"]),
		stage("tunnel", "TUN", byID["tunnel"]),
		stage("provider", "sing-box", byID["provider"]),
		stage("dns", "DNS / IPv6", mergeComponents("DNS / IPv6", byID["dns"], byID["ipv6"])),
		stage("vps", "VPS 传输与固定出口", mergeComponents("VPS 传输与固定出口", byID["transport"], byID["egress"])),
	}
}

func buildAlerts(byID map[string]model.Component, state model.ControllerState, now time.Time) []model.Alert {
	alerts := make([]model.Alert, 0)
	if state == model.StateUnprotected {
		alerts = append(alerts, model.Alert{ID: "unprotected", Severity: "critical", Title: "出口未受保护", Message: "PF Kill Switch 未确认生效，存在本地公网回落风险。", Component: "firewall", RaisedAt: now})
	}
	for _, component := range byID {
		if state == model.StateUnprotected && component.ID == "firewall" {
			continue
		}
		if component.Required && (component.Health == model.HealthFailed || component.Health == model.HealthDegraded) {
			severity := "warning"
			if component.Health == model.HealthFailed {
				severity = "critical"
			}
			alerts = append(alerts, model.Alert{ID: component.ID, Severity: severity, Title: component.Name + "异常", Message: component.Summary + ": " + component.Detail, Component: component.ID, RaisedAt: now})
		}
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return severityRank(alerts[i].Severity) > severityRank(alerts[j].Severity)
	})
	return alerts
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func stage(id, name string, component model.Component) model.Stage {
	return model.Stage{ID: id, Name: name, Health: component.Health, Summary: component.Summary}
}

func mergeComponents(name string, a, b model.Component) model.Component {
	result := a
	result.Name = name
	result.Summary = strings.TrimSpace(a.Summary + " / " + b.Summary)
	if healthRank(b.Health) > healthRank(result.Health) {
		result.Health = b.Health
	}
	return result
}

func isHealthy(component model.Component) bool { return component.Health == model.HealthHealthy }

func allHealthy(byID map[string]model.Component, ids ...string) bool {
	for _, id := range ids {
		if !isHealthy(byID[id]) {
			return false
		}
	}
	return true
}

func healthRank(health model.Health) int {
	switch health {
	case model.HealthFailed:
		return 3
	case model.HealthDegraded:
		return 2
	case model.HealthUnknown:
		return 1
	default:
		return 0
	}
}
