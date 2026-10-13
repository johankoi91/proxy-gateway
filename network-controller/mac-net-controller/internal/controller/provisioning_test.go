package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lightnode-mac-controller/internal/model"
)

func TestVPSConfigImportMapping(t *testing.T) {
	document := model.VPSConfig{
		Schema: "lightnode.vps-config", Version: 1,
		Endpoint: model.VPSConfigEndpoint{Address: "149.104.78.121", Port: 443, ExpectedEgress: "149.104.78.121"},
		VLESS: model.VPSConfigVLESS{
			UUID: "8853a21d-d0c5-4047-8c8d-4843e92c5c89", Flow: "xtls-rprx-vision", Transport: "tcp", Encryption: "none",
			TLS: model.VPSConfigTLS{
				Enabled: true, ServerName: "www.cloudflare.com", UTLSFingerprint: "chrome",
				Reality: model.VPSConfigReality{Enabled: true, PublicKey: "VEnbXRyOqO98TYo4khJlxFSy0oPzTJsuepq2BNs4oiY", ShortID: "0123456789abcdef"},
			},
		},
	}
	update, err := nodeUpdateFromVPSConfig(document)
	if err != nil {
		t.Fatal(err)
	}
	if update.Server != document.Endpoint.Address || update.UUID != document.VLESS.UUID || update.RealityShortID != document.VLESS.TLS.Reality.ShortID {
		t.Fatalf("mapped update = %#v", update)
	}
	document.Version = 2
	if _, err := nodeUpdateFromVPSConfig(document); err == nil {
		t.Fatal("unsupported provisioning version was accepted")
	}
}

func TestBackupsAreBoundedAndMetadataIsSanitized(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	singBoxPath := filepath.Join(directory, "sing-box.json")
	backupDir := filepath.Join(directory, "backups")
	cfg := testControllerConfig()
	cfg.ConfigFilePath = configPath
	cfg.SingBoxConfigFile = singBoxPath
	cfg.BackupDir = backupDir
	cfg.BackupLimit = 3
	configData, _ := json.Marshal(cfg)
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	singBox := `{"outbounds":[{"type":"vless","server":"149.104.78.121","server_port":443,"uuid":"secret-uuid","flow":"xtls-rprx-vision","tls":{"server_name":"www.cloudflare.com","reality":{"public_key":"secret-key","short_id":"aabbccdd"},"utls":{"fingerprint":"chrome"}}}]}`
	if err := os.WriteFile(singBoxPath, []byte(singBox), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := NewController(cfg, ipv6Runner{}, "test")
	for index := 0; index < 5; index++ {
		if _, err := controller.CreateBackup("test"); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := controller.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("backup count = %d", len(backups))
	}
	encoded, _ := json.Marshal(backups)
	for _, secret := range []string{"secret-uuid", "secret-key", "aabbccdd"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("backup metadata leaked %q", secret)
		}
	}
}
