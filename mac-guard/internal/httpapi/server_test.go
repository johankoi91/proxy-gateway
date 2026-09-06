package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/controller"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, context.DeadlineExceeded
}

func TestAuthenticationAndHealth(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := controller.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "admin", "test-password", false)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/snapshot", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}

	authenticated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	req.SetBasicAuth("admin", "test-password")
	handler.ServeHTTP(authenticated, req)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d", authenticated.Code)
	}
}

func TestAuthenticationCanBeDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := controller.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/snapshot", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestCrossOriginWriteIsRejected(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := controller.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)

	request := httptest.NewRequest(http.MethodPost, "/api/tests/run", nil)
	request.Host = "127.0.0.1:9180"
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestLifecycleEndpointsArePostOnly(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := controller.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)
	for _, path := range []string{"/api/actions/start", "/api/actions/pause"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d", path, response.Code)
		}
	}
}

func TestNodeEndpointReturnsSanitizedConfiguration(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	singBoxPath := filepath.Join(directory, "sing-box.json")
	cfg := config.Default()
	cfg.ConfigFilePath = configPath
	cfg.SingBoxConfigFile = singBoxPath
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.BackupDir = filepath.Join(directory, "backups")
	configData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	singBox := `{"outbounds":[{"type":"vless","server":"149.104.78.121","server_port":443,"uuid":"must-not-leak","flow":"xtls-rprx-vision","tls":{"server_name":"www.cloudflare.com","reality":{"public_key":"must-not-leak-key","short_id":"aabbccdd"},"utls":{"fingerprint":"chrome"}}}]}`
	if err := os.WriteFile(singBoxPath, []byte(singBox), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := controller.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/node", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "must-not-leak") {
		t.Fatalf("node response leaked credentials: %s", response.Body.String())
	}

	backupResponse := httptest.NewRecorder()
	handler.ServeHTTP(backupResponse, httptest.NewRequest(http.MethodPost, "/api/backups", nil))
	if backupResponse.Code != http.StatusCreated || strings.Contains(backupResponse.Body.String(), "must-not-leak") {
		t.Fatalf("backup response status=%d body=%s", backupResponse.Code, backupResponse.Body.String())
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/backups", nil))
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), "must-not-leak") {
		t.Fatalf("backup list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	invalidImport := httptest.NewRecorder()
	handler.ServeHTTP(invalidImport, httptest.NewRequest(http.MethodPost, "/api/node/import", strings.NewReader(`{"schema":"wrong","version":1}`)))
	if invalidImport.Code != http.StatusBadGateway {
		t.Fatalf("invalid import status=%d body=%s", invalidImport.Code, invalidImport.Body.String())
	}
}
