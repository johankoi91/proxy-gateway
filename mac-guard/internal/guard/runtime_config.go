package guard

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"lightnode-mac-guard/internal/config"
	"lightnode-mac-guard/internal/model"
)

type sanitizedDNSServer struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Path       string `json:"path"`
	Detour     string `json:"detour"`
	TLS        struct {
		ServerName string `json:"server_name"`
	} `json:"tls"`
}

type sanitizedInbound struct {
	Type        string   `json:"type"`
	Tag         string   `json:"tag"`
	Address     []string `json:"address"`
	MTU         int      `json:"mtu"`
	DNSMode     string   `json:"dns_mode"`
	AutoRoute   bool     `json:"auto_route"`
	StrictRoute bool     `json:"strict_route"`
}

type sanitizedOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Flow       string `json:"flow"`
	TLS        struct {
		Enabled    bool   `json:"enabled"`
		ServerName string `json:"server_name"`
		UTLS       struct {
			Enabled     bool   `json:"enabled"`
			Fingerprint string `json:"fingerprint"`
		} `json:"utls"`
	} `json:"tls"`
}

type sanitizedSingBox struct {
	DNS struct {
		Strategy string               `json:"strategy"`
		Final    string               `json:"final"`
		Servers  []sanitizedDNSServer `json:"servers"`
	} `json:"dns"`
	Inbounds  []sanitizedInbound  `json:"inbounds"`
	Outbounds []sanitizedOutbound `json:"outbounds"`
	Route     struct {
		AutoDetectInterface bool   `json:"auto_detect_interface"`
		Final               string `json:"final"`
	} `json:"route"`
}

func runtimeConfiguration(cfg config.Config) model.RuntimeConfiguration {
	singBox := readSanitizedSingBox(cfg.SingBoxConfigFile)
	singExecutable, singArguments, singLabel := launchdCommand(
		"/Library/LaunchDaemons/com.lightnode.sing-box.plist",
		"/usr/local/libexec/sing-box", []string{"run", "-c", cfg.SingBoxConfigFile}, cfg.ProviderLaunchDaemonLabel,
	)
	guardExecutable, guardArguments, guardLabel := launchdCommand(
		"/Library/LaunchDaemons/com.lightnode.vless-guard.plist",
		"/usr/local/libexec/lightnode-guard", []string{"-config", "/Library/Application Support/LightNodeGuard/config.json"}, cfg.LaunchDaemonLabel,
	)
	tun := firstTunInbound(singBox, cfg)
	dns := firstDNSServer(singBox)
	outbound := firstVLESSOutbound(singBox, cfg)
	firewallExecutable, firewallArguments := splitCommand(cfg.FirewallRepairCommand)
	components := []model.RuntimeComponentConfiguration{
		{
			ID: "firewall", Name: "PF 防回落", Owner: "macOS PF", Executable: firewallExecutable, Arguments: firewallArguments,
			Settings: settings(
				"主配置", cfg.PFMainConfigFile, "Anchor", cfg.PFAnchor, "Anchor 文件", cfg.PFAnchorFile,
				"Endpoint 白名单", fmt.Sprintf("%s:%d/tcp", cfg.VPSAddress, cfg.VPSPort), "提交标记", cfg.FirewallCommittedFile,
			),
			HealthComponentIDs: []string{"firewall", "firewall_config", "firewall_binding"},
		},
		{
			ID: "tunnel", Name: "TUN", Owner: "sing-box inbound",
			Settings: settings(
				"类型", tun.Type, "Tag", tun.Tag, "地址", strings.Join(tun.Address, ", "), "MTU", strconv.Itoa(tun.MTU),
				"DNS 模式", tun.DNSMode, "auto_route", strconv.FormatBool(tun.AutoRoute), "strict_route", strconv.FormatBool(tun.StrictRoute),
			),
			HealthComponentIDs: []string{"tunnel"},
		},
		{
			ID: "sing-box", Name: "sing-box", Owner: "launchd", LaunchdLabel: singLabel,
			Executable: singExecutable, Arguments: singArguments,
			Settings:           settings("配置文件", cfg.SingBoxConfigFile, "KeepAlive", "true", "启动加载", "true"),
			HealthComponentIDs: []string{"provider", "provider_service", "config"},
		},
		{
			ID: "dns", Name: "DNS", Owner: "sing-box DNS",
			Settings: settings(
				"系统入口", strings.Join(cfg.ExpectedDNSServers, ", "), "上游", fmt.Sprintf("%s://%s:%d%s", dns.Type, dns.Server, dns.ServerPort, dns.Path),
				"TLS SNI", dns.TLS.ServerName, "strategy", singBox.DNS.Strategy, "final", singBox.DNS.Final, "detour", dns.Detour,
			),
			HealthComponentIDs: []string{"dns", "ipv6"},
		},
		{
			ID: "outbound", Name: "东京出站", Owner: "sing-box outbound",
			Settings: settings(
				"类型", outbound.Type, "Tag", outbound.Tag, "服务器", fmt.Sprintf("%s:%d", outbound.Server, outbound.ServerPort),
				"flow", outbound.Flow, "TLS", strconv.FormatBool(outbound.TLS.Enabled), "TLS SNI", outbound.TLS.ServerName,
				"uTLS 指纹", outbound.TLS.UTLS.Fingerprint, "route.final", singBox.Route.Final,
			),
			HealthComponentIDs: []string{"transport", "egress"},
		},
		{
			ID: "guard", Name: "LightNode Guard", Owner: "launchd", LaunchdLabel: guardLabel,
			Executable: guardExecutable, Arguments: guardArguments,
			Settings: settings(
				"监听", cfg.ListenAddress, "完整检查", fmt.Sprintf("%ds", cfg.CheckIntervalSeconds),
				"冲突巡检", fmt.Sprintf("%ds", cfg.ConflictCheckIntervalSeconds), "连接观测", fmt.Sprintf("%s / %ds", cfg.V2ClashAPIURL, cfg.V2PollIntervalSeconds),
			),
			HealthComponentIDs: []string{"guard_service"},
		},
	}
	return model.RuntimeConfiguration{
		Profile: "global_japan", Transport: "VLESS Reality / TCP",
		VPSAddress: cfg.VPSAddress, VPSPort: cfg.VPSPort, ExpectedEgress: cfg.ExpectedEgressIP,
		TunnelAddress: cfg.TunnelAddress, TunnelInterfacePrefix: cfg.TunnelInterfacePrefix,
		ExpectedDNSServers: append([]string(nil), cfg.ExpectedDNSServers...),
		PFAnchor:           cfg.PFAnchor, PFAnchorFile: cfg.PFAnchorFile, PFMainConfigFile: cfg.PFMainConfigFile,
		GuardService: cfg.LaunchDaemonLabel, ProviderService: cfg.ProviderLaunchDaemonLabel,
		ListenAddress: cfg.ListenAddress, AuthenticationEnabled: !cfg.DisableAuthentication,
		CheckIntervalSeconds: cfg.CheckIntervalSeconds, ConflictCheckIntervalSeconds: cfg.ConflictCheckIntervalSeconds,
		ObserverURL: cfg.V2ClashAPIURL, ObserverIntervalSeconds: cfg.V2PollIntervalSeconds,
		Components: components,
	}
}

func readSanitizedSingBox(path string) sanitizedSingBox {
	var value sanitizedSingBox
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &value)
	}
	return value
}

func firstTunInbound(value sanitizedSingBox, cfg config.Config) sanitizedInbound {
	for _, inbound := range value.Inbounds {
		if inbound.Type == "tun" {
			return inbound
		}
	}
	return sanitizedInbound{Type: "tun", Tag: "tun-in", Address: []string{cfg.TunnelAddress}, MTU: 1280, DNSMode: "hijack", AutoRoute: true, StrictRoute: true}
}

func firstDNSServer(value sanitizedSingBox) (server sanitizedDNSServer) {
	if len(value.DNS.Servers) > 0 {
		return value.DNS.Servers[0]
	}
	server.Type, server.Tag, server.Server, server.ServerPort, server.Path, server.Detour = "https", "dns-ai", "104.16.249.249", 443, "/dns-query", "tokyo"
	server.TLS.ServerName = "cloudflare-dns.com"
	return server
}

func firstVLESSOutbound(value sanitizedSingBox, cfg config.Config) (outbound sanitizedOutbound) {
	for _, candidate := range value.Outbounds {
		if candidate.Type == "vless" {
			return candidate
		}
	}
	outbound.Type, outbound.Tag, outbound.Server, outbound.ServerPort = "vless", "tokyo", cfg.VPSAddress, cfg.VPSPort
	return outbound
}

func launchdCommand(path, fallbackExecutable string, fallbackArguments []string, fallbackLabel string) (string, []string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallbackExecutable, append([]string(nil), fallbackArguments...), fallbackLabel
	}
	arguments := plistStringArray(data, "ProgramArguments")
	if len(arguments) == 0 {
		return fallbackExecutable, append([]string(nil), fallbackArguments...), plistString(data, "Label", fallbackLabel)
	}
	return arguments[0], arguments[1:], plistString(data, "Label", fallbackLabel)
}

func plistString(data []byte, key, fallback string) string {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	found := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return fallback
		}
		if err != nil {
			return fallback
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "key" {
				var name string
				if decoder.DecodeElement(&name, &value) == nil {
					found = name == key
				}
			} else if found && value.Name.Local == "string" {
				var result string
				if decoder.DecodeElement(&result, &value) == nil {
					return result
				}
			}
		}
	}
}

func plistStringArray(data []byte, key string) []string {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	foundKey, inArray := false, false
	var result []string
	for {
		token, err := decoder.Token()
		if err != nil {
			return result
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "key" {
				var name string
				if decoder.DecodeElement(&name, &value) == nil {
					foundKey = name == key
				}
			} else if foundKey && value.Name.Local == "array" {
				inArray = true
			} else if inArray && value.Name.Local == "string" {
				var item string
				if decoder.DecodeElement(&item, &value) == nil {
					result = append(result, item)
				}
			}
		case xml.EndElement:
			if inArray && value.Name.Local == "array" {
				return result
			}
		}
	}
}

func settings(values ...string) []model.RuntimeSetting {
	result := make([]model.RuntimeSetting, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		if values[index+1] != "" {
			result = append(result, model.RuntimeSetting{Name: values[index], Value: values[index+1]})
		}
	}
	return result
}

func splitCommand(command []string) (string, []string) {
	if len(command) == 0 {
		return "", nil
	}
	return command[0], append([]string(nil), command[1:]...)
}
