package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	ConfigFilePath               string   `json:"-"`
	Version                      int      `json:"version"`
	ListenAddress                string   `json:"listenAddress"`
	AdminUser                    string   `json:"adminUser"`
	AdminPassword                string   `json:"adminPassword"`
	DisableAuthentication        bool     `json:"disableAuthentication"`
	ProviderName                 string   `json:"providerName"`
	ProviderProcesses            []string `json:"providerProcesses"`
	TunnelInterfacePrefix        string   `json:"tunnelInterfacePrefix"`
	TunnelInterfaceName          string   `json:"tunnelInterfaceName"`
	TunnelAddress                string   `json:"tunnelAddress"`
	VPSAddress                   string   `json:"vpsAddress"`
	VPSPort                      int      `json:"vpsPort"`
	ExpectedEgressIP             string   `json:"expectedEgressIp"`
	EgressProbeURL               string   `json:"egressProbeUrl"`
	IPv6ProbeURL                 string   `json:"ipv6ProbeUrl"`
	DNSProbeHost                 string   `json:"dnsProbeHost"`
	ExpectedDNSServers           []string `json:"expectedDnsServers"`
	PFAnchor                     string   `json:"pfAnchor"`
	PFAnchorFile                 string   `json:"pfAnchorFile"`
	PFMainConfigFile             string   `json:"pfMainConfigFile"`
	FirewallCommittedFile        string   `json:"firewallCommittedFile"`
	FirewallRepairCommand        []string `json:"firewallRepairCommand"`
	DNSRepairCommand             []string `json:"dnsRepairCommand"`
	ConflictProcessNames         []string `json:"conflictProcessNames"`
	ConflictRouteProbeIP         string   `json:"conflictRouteProbeIp"`
	ConflictAutoRemediate        bool     `json:"conflictAutoRemediate"`
	ConflictRemediationCommand   []string `json:"conflictRemediationCommand"`
	ConflictCheckIntervalSeconds int      `json:"conflictCheckIntervalSeconds"`
	V2Enabled                    bool     `json:"v2Enabled"`
	V2PolicyFile                 string   `json:"v2PolicyFile"`
	V2ClashAPIURL                string   `json:"v2ClashApiUrl"`
	V2ClashAPISecret             string   `json:"v2ClashApiSecret"`
	V2PollIntervalSeconds        int      `json:"v2PollIntervalSeconds"`
	SingBoxConfigFile            string   `json:"singBoxConfigFile"`
	LaunchDaemonLabel            string   `json:"launchDaemonLabel"`
	ProviderLaunchDaemonLabel    string   `json:"providerLaunchDaemonLabel"`
	CheckIntervalSeconds         int      `json:"checkIntervalSeconds"`
	CheckTimeoutSeconds          int      `json:"checkTimeoutSeconds"`
	RecoveryCommand              []string `json:"recoveryCommand"`
	StartCommand                 []string `json:"startCommand"`
	PauseCommand                 []string `json:"pauseCommand"`
	MaintenanceFile              string   `json:"maintenanceFile"`
	NotificationCommand          []string `json:"notificationCommand"`
	EventLogFile                 string   `json:"eventLogFile"`
	EventLimit                   int      `json:"eventLimit"`
}

func Default() Config {
	const guardBinary = "/usr/local/libexec/lightnode-guard"
	return Config{
		ConfigFilePath:        "/Library/Application Support/LightNodeGuard/config.json",
		Version:               1,
		ListenAddress:         "127.0.0.1:9180",
		AdminUser:             "admin",
		ProviderName:          "sing-box",
		ProviderProcesses:     []string{"sing-box"},
		TunnelInterfacePrefix: "utun",
		TunnelInterfaceName:   "utun255",
		TunnelAddress:         "172.19.0.1",
		VPSAddress:            "149.104.78.121",
		VPSPort:               443,
		ExpectedEgressIP:      "149.104.78.121",
		EgressProbeURL:        "https://api.ipify.org",
		IPv6ProbeURL:          "https://api64.ipify.org",
		DNSProbeHost:          "claude.ai",
		// sing-box 1.14 derives the native TUN DNS address from the next IP in
		// 172.19.0.1/30 when dns_mode is hijack.
		ExpectedDNSServers:           []string{"172.19.0.2"},
		PFAnchor:                     "com.lightnode.vless-killswitch",
		PFAnchorFile:                 "/etc/pf.anchors/com.lightnode.vless-killswitch",
		PFMainConfigFile:             "/etc/pf.conf",
		FirewallCommittedFile:        "/Library/Application Support/LightNodeGuard/firewall.committed",
		FirewallRepairCommand:        []string{guardBinary, "firewall", "refresh"},
		DNSRepairCommand:             []string{guardBinary, "dns", "refresh"},
		ConflictProcessNames:         []string{"clash-verge", "verge-mihomo", "FlClash"},
		ConflictRouteProbeIP:         "8.8.8.8",
		ConflictAutoRemediate:        true,
		ConflictRemediationCommand:   []string{guardBinary, "conflicts", "remediate"},
		ConflictCheckIntervalSeconds: 2,
		V2Enabled:                    false,
		V2PolicyFile:                 "/Library/Application Support/LightNodeGuard/v2-policy.json",
		V2ClashAPIURL:                "http://127.0.0.1:9090",
		V2PollIntervalSeconds:        1,
		SingBoxConfigFile:            "/Library/Application Support/LightNodeGuard/sing-box.json",
		LaunchDaemonLabel:            "com.lightnode.vless-guard",
		ProviderLaunchDaemonLabel:    "com.lightnode.sing-box",
		CheckIntervalSeconds:         10,
		CheckTimeoutSeconds:          6,
		RecoveryCommand:              []string{guardBinary, "tunnel", "restart"},
		StartCommand:                 []string{guardBinary, "tunnel", "start"},
		PauseCommand:                 []string{guardBinary, "tunnel", "pause"},
		MaintenanceFile:              "/Library/Application Support/LightNodeGuard/safely-paused",
		NotificationCommand:          []string{guardBinary, "notify"},
		EventLogFile:                 "/Library/Logs/LightNodeGuard/events.jsonl",
		EventLimit:                   200,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.ConfigFilePath = path
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.ListenAddress == "" || cfg.VPSAddress == "" || cfg.VPSPort < 1 || cfg.VPSPort > 65535 {
		return Config{}, fmt.Errorf("listenAddress and a valid VPS endpoint are required")
	}
	if cfg.ExpectedEgressIP == "" || cfg.EgressProbeURL == "" || cfg.DNSProbeHost == "" {
		return Config{}, fmt.Errorf("egress and DNS probes are required")
	}
	if cfg.CheckIntervalSeconds < 2 {
		cfg.CheckIntervalSeconds = 2
	}
	if cfg.CheckTimeoutSeconds < 1 {
		cfg.CheckTimeoutSeconds = 1
	}
	if cfg.ConflictCheckIntervalSeconds < 1 {
		cfg.ConflictCheckIntervalSeconds = 1
	}
	if cfg.V2PollIntervalSeconds < 1 {
		cfg.V2PollIntervalSeconds = 1
	}
	if cfg.EventLimit < 20 {
		cfg.EventLimit = 20
	}
	if value := os.Getenv("LIGHTNODE_GUARD_ADMIN_PASSWORD"); value != "" {
		cfg.AdminPassword = value
	}
	return cfg, nil
}

func (c Config) CheckInterval() time.Duration {
	return time.Duration(c.CheckIntervalSeconds) * time.Second
}

func (c Config) CheckTimeout() time.Duration {
	return time.Duration(c.CheckTimeoutSeconds) * time.Second
}
