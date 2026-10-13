package model

type NodeConfiguration struct {
	Server                     string `json:"server"`
	Port                       int    `json:"port"`
	ExpectedEgress             string `json:"expectedEgress"`
	Flow                       string `json:"flow"`
	TLSServerName              string `json:"tlsServerName"`
	UTLSFingerprint            string `json:"utlsFingerprint"`
	UUIDConfigured             bool   `json:"uuidConfigured"`
	RealityPublicKeyConfigured bool   `json:"realityPublicKeyConfigured"`
	RealityShortIDConfigured   bool   `json:"realityShortIdConfigured"`
}

type NodeUpdate struct {
	Server           string `json:"server"`
	Port             int    `json:"port"`
	ExpectedEgress   string `json:"expectedEgress"`
	Flow             string `json:"flow"`
	TLSServerName    string `json:"tlsServerName"`
	UTLSFingerprint  string `json:"utlsFingerprint"`
	UUID             string `json:"uuid,omitempty"`
	RealityPublicKey string `json:"realityPublicKey,omitempty"`
	RealityShortID   string `json:"realityShortId,omitempty"`
}
