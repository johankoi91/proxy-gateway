package gateway

import "time"

const StateVersion = 1

type State struct {
	Version   int       `json:"version"`
	PublicIP  string    `json:"publicIp"`
	EgressIF  string    `json:"egressInterface"`
	WireGuard WireGuard `json:"wireGuard"`
	Reality   Reality   `json:"reality"`
	Clients   []Client  `json:"clients"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type WireGuard struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Port       int    `json:"port"`
	Network    string `json:"network"`
	IPv6Prefix string `json:"ipv6Prefix"`
	DNS        string `json:"dns"`
	MTU        int    `json:"mtu"`
}

type Reality struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
	Port       int    `json:"port"`
	ServerName string `json:"serverName"`
	Target     string `json:"target"`
}

type Client struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CreatedAt        time.Time `json:"createdAt"`
	WireGuardPrivate string    `json:"wireGuardPrivateKey"`
	WireGuardPublic  string    `json:"wireGuardPublicKey"`
	IPv4             string    `json:"ipv4"`
	IPv6             string    `json:"ipv6"`
	VLESSUUID        string    `json:"vlessUuid"`
	RealityShortID   string    `json:"realityShortId"`
}

type PeerStatus struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Endpoint        string `json:"endpoint"`
	LatestHandshake int64  `json:"latestHandshake"`
	ReceiveBytes    int64  `json:"receiveBytes"`
	TransmitBytes   int64  `json:"transmitBytes"`
	Connected       bool   `json:"connected"`
}

type Status struct {
	PublicIP      string       `json:"publicIp"`
	EgressIF      string       `json:"egressInterface"`
	WireGuardPort int          `json:"wireGuardPort"`
	RealityPort   int          `json:"realityPort"`
	WireGuardUp   bool         `json:"wireGuardUp"`
	XrayUp        bool         `json:"xrayUp"`
	DNSUp         bool         `json:"dnsUp"`
	IPv4Forward   bool         `json:"ipv4Forwarding"`
	Peers         []PeerStatus `json:"peers"`
}

type PublicClient struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	IPv4      string    `json:"ipv4"`
	IPv6      string    `json:"ipv6"`
}

func (c Client) Public() PublicClient {
	return PublicClient{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, IPv4: c.IPv4, IPv6: c.IPv6}
}
