package controller

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/model"
)

type checkOutput struct {
	components       []model.Component
	tests            []model.TestResult
	observedIP       string
	physicalIF       string
	tunnelIF         string
}

type checker struct {
	cfg                  config.Config
	runner               Runner
	client               *http.Client
	dialPhysicalEndpoint func(context.Context, string, string, time.Duration) (net.Conn, error)
}

func newChecker(cfg config.Config, runner Runner) *checker {
	return &checker{
		cfg:    cfg,
		runner: runner,
		client: &http.Client{
			Timeout:   cfg.CheckTimeout(),
			Transport: &http.Transport{Proxy: nil},
		},
		dialPhysicalEndpoint: dialPhysicalEndpoint,
	}
}

func (c *checker) run(ctx context.Context) checkOutput {
	now := time.Now().UTC()
	physicalIF := c.physicalInterface(ctx)
	components := []model.Component{
		c.checkNetworkConflict(ctx, now),
		c.checkProvider(ctx, now),
		c.checkProviderLaunchd(ctx, now),
		c.checkTunnel(ctx, now),
		c.checkPF(ctx, now),
		c.checkPFPersistence(now),
		c.checkPFBinding(ctx, now),
		c.checkPFBootstrapLaunchd(ctx, now),
		c.checkConfig(now),
		c.checkLaunchd(ctx, now),
	}
	vpsComponent, vpsTest := c.checkVPS(ctx, now, physicalIF)
	var egressComponent, dnsComponent, ipv6Component model.Component
	var egressTest, dnsTest, ipv6Test model.TestResult
	var observedIP string
	if c.maintenanceActive() {
		egressComponent, egressTest, observedIP = c.checkPausedEgress(ctx, now)
		dnsComponent, dnsTest = c.checkPausedDNS(ctx, now)
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		ipv6Component, ipv6Test = c.checkIPv6(probeCtx, now)
		cancel()
	} else {
		egressComponent, egressTest, observedIP = c.checkEgress(ctx, now)
		dnsComponent, dnsTest = c.checkDNS(ctx, now)
		ipv6Component, ipv6Test = c.checkIPv6(ctx, now)
	}
	components = append(components, vpsComponent, egressComponent, dnsComponent, ipv6Component)
	return checkOutput{
		components:       components,
		tests:            []model.TestResult{vpsTest, egressTest, dnsTest, ipv6Test},
		observedIP:       observedIP,
		physicalIF:       physicalIF,
		tunnelIF:         tunnelInterface(components),
	}
}

func (c *checker) maintenanceActive() bool {
	if c.cfg.MaintenanceFile == "" {
		return false
	}
	_, err := os.Stat(c.cfg.MaintenanceFile)
	return err == nil
}

func (c *checker) checkPausedEgress(ctx context.Context, now time.Time) (model.Component, model.TestResult, string) {
	started := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, c.cfg.EgressProbeURL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		comp := component("egress", "公网出口", model.HealthHealthy, true, "安全暂停期间公网访问已阻断", "blocked", now, started)
		return comp, test("egress", "公网阻断", model.HealthHealthy, "blocked", "blocked", "未发现本地公网回落", now, started), ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	actual := strings.TrimSpace(string(body))
	comp := component("egress", "公网出口", model.HealthFailed, true, "安全暂停期间仍可访问公网", actual, now, started)
	return comp, test("egress", "公网阻断", model.HealthFailed, "blocked", actual, "检测到安全暂停期间的公网访问", now, started), actual
}

func (c *checker) checkPausedDNS(ctx context.Context, now time.Time) (model.Component, model.TestResult) {
	started := time.Now()
	out, err := c.runner.Run(ctx, "scutil", "--dns")
	expected := strings.Join(c.cfg.ExpectedDNSServers, ", ")
	if err != nil {
		comp := component("dns", "隧道 DNS", model.HealthFailed, true, "无法确认暂停期间的 DNS 设置", err.Error(), now, started)
		return comp, test("dns", "DNS 防回落", model.HealthFailed, expected, "unknown", err.Error(), now, started)
	}
	if !defaultDNSUses(string(out), c.cfg.ExpectedDNSServers) {
		comp := component("dns", "隧道 DNS", model.HealthFailed, true, "DNS 已偏离隧道地址", expected, now, started)
		return comp, test("dns", "DNS 防回落", model.HealthFailed, expected, "changed", "检测到本地 DNS 回落风险", now, started)
	}
	comp := component("dns", "隧道 DNS", model.HealthHealthy, true, "安全暂停期间 DNS 未回落", expected, now, started)
	return comp, test("dns", "DNS 防回落", model.HealthHealthy, expected, expected, "DNS 仍固定为隧道地址", now, started)
}

func (c *checker) checkPFBootstrapLaunchd(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	const label = "com.lightnode.pf-bootstrap"
	if _, err := c.runner.Run(ctx, "launchctl", "print", "system/"+label); err != nil {
		return component("pf_bootstrap_service", "开机 PF 引导", model.HealthFailed, true, "开机防泄漏服务未加载", err.Error(), now, started)
	}
	return component("pf_bootstrap_service", "开机 PF 引导", model.HealthHealthy, true, "开机防泄漏服务已加载", label, now, started)
}

func (c *checker) checkNetworkConflict(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	conflicts := make([]string, 0)

	proxyOutput, proxyErr := c.runner.Run(ctx, "scutil", "--proxy")
	if proxyErr != nil {
		return component("network_conflict", "代理/VPN 冲突", model.HealthFailed, true, "无法检查系统代理", proxyErr.Error(), now, started)
	}
	if enabledSystemProxies(string(proxyOutput)) {
		conflicts = append(conflicts, "系统 HTTP/HTTPS/SOCKS/PAC 代理已启用")
	}

	for _, processName := range c.cfg.ConflictProcessNames {
		if output, err := c.runner.Run(ctx, "pgrep", "-x", processName); err == nil && strings.TrimSpace(string(output)) != "" {
			conflicts = append(conflicts, "冲突进程 "+processName)
		}
	}

	vpnOutput, vpnErr := c.runner.Run(ctx, "scutil", "--nc", "list")
	if vpnErr != nil {
		return component("network_conflict", "代理/VPN 冲突", model.HealthFailed, true, "无法检查系统 VPN", vpnErr.Error(), now, started)
	}
	if names := connectedVPNNames(string(vpnOutput)); len(names) > 0 {
		conflicts = append(conflicts, "系统 VPN 已连接: "+strings.Join(names, ", "))
	}

	tunnelIF, tunnelFound := c.currentTunnelInterface(ctx)
	if c.cfg.ConflictRouteProbeIP != "" && tunnelFound {
		routeOutput, routeErr := c.runner.Run(ctx, "route", "-n", "get", c.cfg.ConflictRouteProbeIP)
		if routeErr != nil {
			return component("network_conflict", "代理/VPN 冲突", model.HealthFailed, true, "无法验证公网路由", routeErr.Error(), now, started)
		}
		actualIF := routeInterface(string(routeOutput))
		if actualIF != tunnelIF {
			conflicts = append(conflicts, fmt.Sprintf("公网路由 %s 未指向 %s", actualIF, tunnelIF))
		}
	}

	if len(conflicts) > 0 {
		return component("network_conflict", "代理/VPN 冲突", model.HealthFailed, true, "检测到其他网络通道", strings.Join(conflicts, "; "), now, started)
	}
	return component("network_conflict", "代理/VPN 冲突", model.HealthHealthy, true, "未检测到其他代理或 VPN", "系统代理关闭，公网路由由 LightNode TUN 接管", now, started)
}

func enabledSystemProxies(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && isProxyEnableKey(fields[0]) && fields[2] == "1" {
			return true
		}
	}
	return false
}

func isProxyEnableKey(key string) bool {
	switch key {
	case "HTTPEnable", "HTTPSEnable", "SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable":
		return true
	default:
		return false
	}
}

func connectedVPNNames(output string) []string {
	names := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "* (Connected)") {
			continue
		}
		firstQuote := strings.Index(line, `"`)
		if firstQuote < 0 {
			names = append(names, "unknown")
			continue
		}
		rest := line[firstQuote+1:]
		secondQuote := strings.Index(rest, `"`)
		if secondQuote < 0 {
			names = append(names, "unknown")
			continue
		}
		names = append(names, rest[:secondQuote])
	}
	return names
}

func routeInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "interface:" {
			return fields[1]
		}
	}
	return "unknown"
}

func (c *checker) checkProvider(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	for _, name := range c.cfg.ProviderProcesses {
		if out, err := c.runner.Run(ctx, "pgrep", "-x", name); err == nil && strings.TrimSpace(string(out)) != "" {
			return component("provider", c.cfg.ProviderName, model.HealthHealthy, true, "进程运行中", "PID "+strings.Fields(string(out))[0], now, started)
		}
	}
	return component("provider", c.cfg.ProviderName, model.HealthFailed, true, "隧道进程未运行", strings.Join(c.cfg.ProviderProcesses, ", "), now, started)
}

func (c *checker) checkTunnel(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	out, err := c.runner.Run(ctx, "ifconfig")
	if err != nil {
		return component("tunnel", "TUN 接口", model.HealthUnknown, true, "无法读取网络接口", err.Error(), now, started)
	}
	iface, found := findTunnel(string(out), c.cfg.TunnelInterfacePrefix, c.cfg.TunnelAddress)
	if !found {
		return component("tunnel", "TUN 接口", model.HealthFailed, true, "未找到预期 TUN", c.cfg.TunnelInterfacePrefix+" / "+c.cfg.TunnelAddress, now, started)
	}
	return component("tunnel", "TUN 接口", model.HealthHealthy, true, iface+" 已建立", c.cfg.TunnelAddress, now, started)
}

func (c *checker) checkPF(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	out, err := c.runner.Run(ctx, "pfctl", "-s", "info")
	if err != nil {
		return component("firewall", "PF 实时阻断", model.HealthFailed, true, "无法确认 PF 状态", err.Error(), now, started)
	}
	if !strings.Contains(string(out), "Status: Enabled") {
		return component("firewall", "PF 实时阻断", model.HealthFailed, true, "PF 未启用", strings.TrimSpace(string(out)), now, started)
	}
	rules, err := c.runner.Run(ctx, "pfctl", "-a", c.cfg.PFAnchor, "-sr")
	if err != nil || strings.TrimSpace(string(rules)) == "" {
		detail := "Anchor 无规则"
		if err != nil {
			detail = err.Error()
		}
		return component("firewall", "PF 实时阻断", model.HealthFailed, true, "PF 已启用但 Anchor 缺失", detail, now, started)
	}
	ruleText := string(rules)
	if !strings.Contains(ruleText, "block drop out quick inet all") || !strings.Contains(ruleText, "block drop out quick inet6 all") {
		return component("firewall", "PF 实时阻断", model.HealthFailed, true, "PF 全局阻断规则不完整", strings.TrimSpace(ruleText), now, started)
	}
	return component("firewall", "PF 实时阻断", model.HealthHealthy, true, "IPv4/IPv6 回落已阻断", c.cfg.PFAnchor, now, started)
}

func (c *checker) checkPFPersistence(now time.Time) model.Component {
	started := time.Now()
	anchor, err := os.ReadFile(c.cfg.PFAnchorFile)
	if err != nil || !strings.Contains(string(anchor), "block drop out quick inet all") || !strings.Contains(string(anchor), "block drop out quick inet6 all") {
		detail := c.cfg.PFAnchorFile
		if err != nil {
			detail = err.Error()
		}
		return component("firewall_config", "PF 持久化配置", model.HealthFailed, true, "PF Anchor 文件缺失或不完整", detail, now, started)
	}
	mainConfig, err := os.ReadFile(c.cfg.PFMainConfigFile)
	if err != nil || !strings.Contains(string(mainConfig), `anchor "`+c.cfg.PFAnchor+`"`) || !strings.Contains(string(mainConfig), c.cfg.PFAnchorFile) || !strings.Contains(string(mainConfig), c.cfg.VPSAddress) {
		detail := c.cfg.PFMainConfigFile
		if err != nil {
			detail = err.Error()
		}
		return component("firewall_config", "PF 持久化配置", model.HealthFailed, true, "主 PF 配置未引用受控 Anchor", detail, now, started)
	}
	return component("firewall_config", "PF 持久化配置", model.HealthHealthy, true, "Anchor 可在启动与重载后恢复", c.cfg.PFAnchorFile, now, started)
}

func (c *checker) checkPFBinding(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	rules, err := c.runner.Run(ctx, "pfctl", "-sr")
	if err != nil {
		return component("firewall_binding", "PF 接口白名单", model.HealthFailed, true, "无法读取 PF 接口规则", err.Error(), now, started)
	}
	ruleText := string(rules)
	if !strings.Contains(ruleText, c.cfg.VPSAddress) {
		return component("firewall_binding", "PF 接口白名单", model.HealthFailed, true, "VPS Endpoint 未放行", c.cfg.VPSAddress, now, started)
	}
	physicalIF := c.physicalInterface(ctx)
	if physicalIF == "" {
		return component("firewall_binding", "PF 接口白名单", model.HealthHealthy, true, "当前无物理出口，保持全局阻断", "fail-closed", now, started)
	}
	if !strings.Contains(ruleText, " on "+physicalIF+" ") {
		return component("firewall_binding", "PF 接口白名单", model.HealthFailed, true, "PF 未绑定当前物理接口", physicalIF, now, started)
	}
	if tunnelIF, found := c.currentTunnelInterface(ctx); found && !strings.Contains(ruleText, " on "+tunnelIF+" ") {
		return component("firewall_binding", "PF 接口白名单", model.HealthFailed, true, "PF 未放行当前 TUN 接口", tunnelIF, now, started)
	}
	return component("firewall_binding", "PF 接口白名单", model.HealthHealthy, true, "当前接口与 VPS 白名单正确", physicalIF, now, started)
}

func (c *checker) currentTunnelInterface(ctx context.Context) (string, bool) {
	out, err := c.runner.Run(ctx, "ifconfig")
	if err != nil {
		return "", false
	}
	return findTunnel(string(out), c.cfg.TunnelInterfacePrefix, c.cfg.TunnelAddress)
}

func (c *checker) checkConfig(now time.Time) model.Component {
	started := time.Now()
	info, err := os.Stat(c.cfg.SingBoxConfigFile)
	if err != nil {
		return component("config", "sing-box 配置", model.HealthFailed, true, "配置文件不可用", err.Error(), now, started)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return component("config", "sing-box 配置", model.HealthDegraded, true, "配置权限过宽", info.Mode().Perm().String(), now, started)
	}
	return component("config", "sing-box 配置", model.HealthHealthy, true, "配置文件权限正常", info.Mode().Perm().String(), now, started)
}

func (c *checker) checkLaunchd(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	if _, err := c.runner.Run(ctx, "launchctl", "print", "system/"+c.cfg.LaunchDaemonLabel); err != nil {
		return component("controller_service", "Controller launchd", model.HealthFailed, true, "控制服务未加载", err.Error(), now, started)
	}
	return component("controller_service", "Controller launchd", model.HealthHealthy, true, "控制服务已加载", c.cfg.LaunchDaemonLabel, now, started)
}

func (c *checker) checkProviderLaunchd(ctx context.Context, now time.Time) model.Component {
	started := time.Now()
	if c.cfg.ProviderLaunchDaemonLabel == "" {
		return component("provider_service", "sing-box launchd", model.HealthUnknown, true, "服务标签未配置", "providerLaunchDaemonLabel", now, started)
	}
	if _, err := c.runner.Run(ctx, "launchctl", "print", "system/"+c.cfg.ProviderLaunchDaemonLabel); err != nil {
		return component("provider_service", "sing-box launchd", model.HealthFailed, true, "隧道守护服务未加载", err.Error(), now, started)
	}
	return component("provider_service", "sing-box launchd", model.HealthHealthy, true, "隧道守护服务已加载", c.cfg.ProviderLaunchDaemonLabel, now, started)
}

func (c *checker) checkVPS(ctx context.Context, now time.Time, physicalIF string) (model.Component, model.TestResult) {
	started := time.Now()
	address := net.JoinHostPort(c.cfg.VPSAddress, strconv.Itoa(c.cfg.VPSPort))
	if physicalIF == "" {
		detail := "无法识别当前物理网络接口"
		comp := component("transport", "VPS 物理 Endpoint", model.HealthFailed, true, "物理 Endpoint 未验证", detail, now, started)
		return comp, test("vps_tcp", "VPS 物理 Endpoint", model.HealthFailed, address, "interface unavailable", detail, now, started)
	}
	conn, err := c.dialPhysicalEndpoint(ctx, physicalIF, address, c.cfg.CheckTimeout())
	if err != nil {
		detail := fmt.Sprintf("%s -> %s: %v", physicalIF, address, err)
		comp := component("transport", "VPS 物理 Endpoint", model.HealthFailed, true, "物理 Endpoint 不可达", detail, now, started)
		return comp, test("vps_tcp", "VPS 物理 Endpoint", model.HealthFailed, address, "unavailable via "+physicalIF, detail, now, started)
	}
	localAddress := conn.LocalAddr().String()
	_ = conn.Close()
	detail := fmt.Sprintf("%s / %s -> %s", physicalIF, localAddress, address)
	comp := component("transport", "VPS 物理 Endpoint", model.HealthHealthy, true, "物理 Endpoint 可达", detail, now, started)
	return comp, test("vps_tcp", "VPS 物理 Endpoint", model.HealthHealthy, address, physicalIF+" / "+localAddress, "物理接口 TCP 建连成功", now, started)
}

func (c *checker) checkEgress(ctx context.Context, now time.Time) (model.Component, model.TestResult, string) {
	started := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.EgressProbeURL, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		comp := component("egress", "公网出口", model.HealthFailed, true, "出口检测失败", err.Error(), now, started)
		return comp, test("egress", "公网出口", model.HealthFailed, c.cfg.ExpectedEgressIP, "unavailable", err.Error(), now, started), ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
		comp := component("egress", "公网出口", model.HealthFailed, true, "出口探针返回异常", detail, now, started)
		return comp, test("egress", "公网出口", model.HealthFailed, c.cfg.ExpectedEgressIP, detail, detail, now, started), ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	actual := strings.TrimSpace(string(body))
	if net.ParseIP(actual) == nil {
		comp := component("egress", "公网出口", model.HealthFailed, true, "出口探针响应无效", actual, now, started)
		return comp, test("egress", "公网出口", model.HealthFailed, c.cfg.ExpectedEgressIP, actual, "响应不是有效 IP", now, started), ""
	}
	if actual != c.cfg.ExpectedEgressIP {
		detail := fmt.Sprintf("观察到 %s", actual)
		comp := component("egress", "公网出口", model.HealthFailed, true, "出口 IP 不匹配", detail, now, started)
		return comp, test("egress", "公网出口", model.HealthFailed, c.cfg.ExpectedEgressIP, actual, detail, now, started), actual
	}
	comp := component("egress", "公网出口", model.HealthHealthy, true, "固定出口正确", actual, now, started)
	return comp, test("egress", "公网出口", model.HealthHealthy, c.cfg.ExpectedEgressIP, actual, "出口一致", now, started), actual
}

func (c *checker) checkDNS(ctx context.Context, now time.Time) (model.Component, model.TestResult) {
	started := time.Now()
	addresses, err := net.DefaultResolver.LookupHost(ctx, c.cfg.DNSProbeHost)
	if err != nil {
		comp := component("dns", "隧道 DNS", model.HealthFailed, true, "DNS 解析失败", err.Error(), now, started)
		return comp, test("dns", "DNS 解析", model.HealthFailed, "resolved", "failed", err.Error(), now, started)
	}
	out, routeErr := c.runner.Run(ctx, "scutil", "--dns")
	detail := strings.Join(addresses, ", ")
	serverOK := len(c.cfg.ExpectedDNSServers) == 0
	if routeErr == nil {
		serverOK = defaultDNSUses(string(out), c.cfg.ExpectedDNSServers)
	}
	if !serverOK {
		comp := component("dns", "隧道 DNS", model.HealthDegraded, true, "DNS 可用但未确认经隧道", detail, now, started)
		return comp, test("dns", "DNS 解析", model.HealthDegraded, strings.Join(c.cfg.ExpectedDNSServers, ", "), detail, "解析成功，DNS 路径待确认", now, started)
	}
	comp := component("dns", "隧道 DNS", model.HealthHealthy, true, "DNS 解析与路径正常", detail, now, started)
	return comp, test("dns", "DNS 解析", model.HealthHealthy, "tunnel DNS", detail, "解析成功", now, started)
}

func defaultDNSUses(output string, expectedServers []string) bool {
	mainSection := strings.SplitN(output, "DNS configuration (for scoped queries)", 2)[0]
	for _, block := range strings.Split(mainSection, "resolver #") {
		if strings.Contains(block, "Supplemental") {
			continue
		}
		for _, server := range expectedServers {
			if strings.Contains(block, ": "+server) {
				return true
			}
		}
	}
	return false
}

func (c *checker) checkIPv6(ctx context.Context, now time.Time) (model.Component, model.TestResult) {
	started := time.Now()
	out, err := c.runner.Run(ctx, "curl", "--noproxy", "*", "-6", "-fsS", "--connect-timeout", "2", "--max-time", strconv.Itoa(c.cfg.CheckTimeoutSeconds), c.cfg.IPv6ProbeURL)
	actual := strings.TrimSpace(string(out))
	parsed := net.ParseIP(actual)
	if err == nil && parsed != nil && parsed.To4() == nil {
		comp := component("ipv6", "IPv6 防泄漏", model.HealthFailed, true, "检测到公网 IPv6", actual, now, started)
		return comp, test("ipv6", "IPv6 旁路", model.HealthFailed, "unavailable", actual, "原生 IPv6 可达", now, started)
	}
	if err == nil && parsed != nil && parsed.To4() != nil {
		comp := component("ipv6", "IPv6 防泄漏", model.HealthHealthy, true, "IPv6 探针已被隧道接管", actual, now, started)
		return comp, test("ipv6", "IPv6 旁路", model.HealthHealthy, "no public IPv6", actual, "请求返回 IPv4，未发现原生 IPv6 旁路", now, started)
	}
	if err == nil && actual != "" {
		comp := component("ipv6", "IPv6 防泄漏", model.HealthDegraded, true, "IPv6 探针返回无法识别的结果", actual, now, started)
		return comp, test("ipv6", "IPv6 旁路", model.HealthDegraded, "IP address", actual, "探针响应不是有效 IP", now, started)
	}
	comp := component("ipv6", "IPv6 防泄漏", model.HealthHealthy, true, "公网 IPv6 不可用", "fail-closed", now, started)
	return comp, test("ipv6", "IPv6 旁路", model.HealthHealthy, "unavailable", "unavailable", "未检测到原生 IPv6", now, started)
}

func (c *checker) physicalInterface(ctx context.Context) string {
	if out, err := c.runner.Run(ctx, "scutil", "--nwi"); err == nil {
		if interfaceName := physicalInterfaceFromNWI(string(out)); interfaceName != "" {
			return interfaceName
		}
	}
	out, err := c.runner.Run(ctx, "route", "-n", "get", c.cfg.VPSAddress)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "interface:" && !strings.HasPrefix(fields[1], c.cfg.TunnelInterfacePrefix) {
			return fields[1]
		}
	}
	return ""
}

func physicalInterfaceFromNWI(output string) string {
	for _, line := range strings.Split(output, "\n") {
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

func component(id, name string, health model.Health, required bool, summary, detail string, checkedAt, started time.Time) model.Component {
	return model.Component{ID: id, Name: name, Health: health, Required: required, Summary: summary, Detail: detail, LastCheckedAt: checkedAt, LatencyMS: time.Since(started).Milliseconds()}
}

func test(id, name string, health model.Health, expected, actual, detail string, runAt, started time.Time) model.TestResult {
	return model.TestResult{ID: id, Name: name, Health: health, Expected: expected, Actual: actual, Detail: detail, DurationMS: time.Since(started).Milliseconds(), RunAt: runAt}
}

func findTunnel(output, prefix, address string) (string, bool) {
	current := ""
	for _, line := range strings.Split(output, "\n") {
		if line != "" && line[0] != '\t' && strings.Contains(line, ":") {
			current = strings.SplitN(line, ":", 2)[0]
		}
		if strings.HasPrefix(current, prefix) && (address == "" || strings.Contains(line, address)) {
			return current, true
		}
	}
	return "", false
}

func tunnelInterface(components []model.Component) string {
	for _, c := range components {
		if c.ID == "tunnel" && c.Health == model.HealthHealthy {
			return strings.Fields(c.Summary)[0]
		}
	}
	return ""
}
