package model

import "time"

type VPSConfig struct {
	Schema   string            `json:"schema"`
	Version  int               `json:"version"`
	Client   VPSConfigClient   `json:"client"`
	Endpoint VPSConfigEndpoint `json:"endpoint"`
	VLESS    VPSConfigVLESS    `json:"vless"`
}

type VPSConfigClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type VPSConfigEndpoint struct {
	Address        string `json:"address"`
	Port           int    `json:"port"`
	ExpectedEgress string `json:"expectedEgress"`
}

type VPSConfigVLESS struct {
	UUID       string       `json:"uuid"`
	Flow       string       `json:"flow"`
	Transport  string       `json:"transport"`
	Encryption string       `json:"encryption"`
	TLS        VPSConfigTLS `json:"tls"`
}

type VPSConfigTLS struct {
	Enabled         bool             `json:"enabled"`
	ServerName      string           `json:"serverName"`
	UTLSFingerprint string           `json:"utlsFingerprint"`
	Reality         VPSConfigReality `json:"reality"`
}

type VPSConfigReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey"`
	ShortID   string `json:"shortId"`
}

type BackupMetadata struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	Reason         string    `json:"reason"`
	Server         string    `json:"server"`
	Port           int       `json:"port"`
	ExpectedEgress string    `json:"expectedEgress"`
}
