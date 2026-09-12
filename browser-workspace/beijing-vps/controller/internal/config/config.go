package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
)

const (
	Schema  = "lightnode.vps-config"
	Version = 1
)

var (
	interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	secretPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)
	shortIDPattern   = regexp.MustCompile(`^[0-9a-fA-F]{2,32}$`)
)

type VPSConfig struct {
	Schema   string      `json:"schema"`
	Version  int         `json:"version"`
	Client   VPSClient   `json:"client"`
	Endpoint VPSEndpoint `json:"endpoint"`
	VLESS    VLESS       `json:"vless"`
}

type VPSClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type VPSEndpoint struct {
	Address        string `json:"address"`
	Port           int    `json:"port"`
	ExpectedEgress string `json:"expectedEgress"`
}

type VLESS struct {
	UUID       string   `json:"uuid"`
	Flow       string   `json:"flow"`
	Transport  string   `json:"transport"`
	Encryption string   `json:"encryption"`
	TLS        VLESSTLS `json:"tls"`
}

type VLESSTLS struct {
	Enabled         bool         `json:"enabled"`
	ServerName      string       `json:"serverName"`
	UTLSFingerprint string       `json:"utlsFingerprint"`
	Reality         VLESSReality `json:"reality"`
}

type VLESSReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey"`
	ShortID   string `json:"shortId"`
}

type Controller struct {
	Version               int      `json:"version"`
	PhysicalInterface     string   `json:"physicalInterface"`
	ManagementAddress     string   `json:"managementAddress"`
	ManagementCIDR        string   `json:"managementCidr"`
	ManagementSource      string   `json:"managementSource"`
	ManagementTCPPorts    []int    `json:"managementTcpPorts"`
	PublicInboundTCPPorts []int    `json:"publicInboundTcpPorts,omitempty"`
	PublicInboundUDPPorts []int    `json:"publicInboundUdpPorts,omitempty"`
	DashboardAddress      string   `json:"dashboardAddress"`
	TunnelInterface       string   `json:"tunnelInterface"`
	TunnelAddress         string   `json:"tunnelAddress"`
	TunnelDNS             string   `json:"tunnelDns"`
	VPSAddress            string   `json:"vpsAddress"`
	VPSPort               int      `json:"vpsPort"`
	ExpectedEgress        string   `json:"expectedEgress"`
	EgressProbeURL        string   `json:"egressProbeUrl"`
	SingBoxBinary         string   `json:"singBoxBinary"`
	SingBoxConfig         string   `json:"singBoxConfig"`
	FirewallConfig        string   `json:"firewallConfig"`
	CheckIntervalSeconds  int      `json:"checkIntervalSeconds"`
	AllowedInboundNote    []string `json:"allowedInboundNote"`
}

func DefaultController(vps VPSConfig) Controller {
	return Controller{
		Version: 1, PhysicalInterface: "enp0s1", ManagementAddress: "10.211.55.7", ManagementCIDR: "10.211.55.0/24",
		ManagementSource: "10.211.55.1", ManagementTCPPorts: []int{22},
		DashboardAddress: "10.211.55.7",
		TunnelInterface:  "lightnode-tun",
		TunnelAddress:    "172.19.0.1/30", TunnelDNS: "172.19.0.2",
		VPSAddress: vps.Endpoint.Address, VPSPort: vps.Endpoint.Port,
		ExpectedEgress: vps.Endpoint.ExpectedEgress, EgressProbeURL: "https://api.ipify.org",
		SingBoxBinary:        "/usr/local/libexec/lightnode-linux-controller/sing-box",
		SingBoxConfig:        "/etc/lightnode-linux-controller/sing-box.json",
		FirewallConfig:       "/etc/lightnode-linux-controller/lockdown.nft",
		CheckIntervalSeconds: 10,
		AllowedInboundNote:   []string{"host SSH"},
	}
}

func ReadVPS(path string) (VPSConfig, error) {
	var value VPSConfig
	if err := decodeStrict(path, &value); err != nil {
		return value, err
	}
	if err := value.Validate(); err != nil {
		return value, err
	}
	return value, nil
}

func ReadController(path string) (Controller, error) {
	var value Controller
	if err := decodeStrict(path, &value); err != nil {
		return value, err
	}
	if err := value.Validate(); err != nil {
		return value, err
	}
	return value, nil
}

func decodeStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: trailing JSON data", path)
	}
	return nil
}

func (v VPSConfig) Validate() error {
	if v.Schema != Schema || v.Version != Version {
		return fmt.Errorf("unsupported VPS configuration %q version %d", v.Schema, v.Version)
	}
	if net.ParseIP(v.Endpoint.Address).To4() == nil || net.ParseIP(v.Endpoint.ExpectedEgress).To4() == nil {
		return errors.New("VPS endpoint and expected egress must be IPv4 addresses")
	}
	if v.Endpoint.Port < 1 || v.Endpoint.Port > 65535 {
		return errors.New("VPS endpoint port is invalid")
	}
	if v.VLESS.Transport != "tcp" || v.VLESS.Encryption != "none" || v.VLESS.Flow != "xtls-rprx-vision" {
		return errors.New("only VLESS Reality TCP with xtls-rprx-vision is supported")
	}
	if !v.VLESS.TLS.Enabled || !v.VLESS.TLS.Reality.Enabled || v.VLESS.TLS.ServerName == "" {
		return errors.New("TLS Reality configuration is incomplete")
	}
	if !uuidPattern.MatchString(v.VLESS.UUID) || !secretPattern.MatchString(v.VLESS.TLS.Reality.PublicKey) {
		return errors.New("VLESS UUID or Reality public key is invalid")
	}
	if !shortIDPattern.MatchString(v.VLESS.TLS.Reality.ShortID) || len(v.VLESS.TLS.Reality.ShortID)%2 != 0 {
		return errors.New("Reality short ID is invalid")
	}
	if strings.TrimSpace(v.VLESS.TLS.UTLSFingerprint) == "" {
		return errors.New("uTLS fingerprint is required")
	}
	return nil
}

func (c Controller) Validate() error {
	if c.Version != 1 || !interfacePattern.MatchString(c.PhysicalInterface) || !interfacePattern.MatchString(c.TunnelInterface) {
		return errors.New("controller version or interface name is invalid")
	}
	if _, _, err := net.ParseCIDR(c.ManagementCIDR); err != nil {
		return errors.New("management CIDR is invalid")
	}
	if net.ParseIP(c.ManagementAddress).To4() == nil || net.ParseIP(c.DashboardAddress).To4() == nil || net.ParseIP(c.ManagementSource).To4() == nil || net.ParseIP(c.VPSAddress).To4() == nil || net.ParseIP(c.TunnelDNS).To4() == nil {
		return errors.New("controller IPv4 configuration is invalid")
	}
	if c.VPSPort < 1 || c.VPSPort > 65535 || c.SingBoxBinary == "" || c.SingBoxConfig == "" || c.FirewallConfig == "" {
		return errors.New("controller paths or VPS port are invalid")
	}
	for _, port := range c.ManagementTCPPorts {
		if port < 1 || port > 65535 {
			return errors.New("management port is invalid")
		}
	}
	for _, port := range c.PublicInboundTCPPorts {
		if port < 1 || port > 65535 {
			return errors.New("public inbound port is invalid")
		}
	}
	for _, port := range c.PublicInboundUDPPorts {
		if port < 1 || port > 65535 {
			return errors.New("public inbound UDP port is invalid")
		}
	}
	return nil
}
