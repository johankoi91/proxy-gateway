package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lightnode-vpn-gateway/internal/gateway"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	cfg := gateway.DefaultConfig()
	cfg.RuntimeDir = filepath.Join(dir, "run")
	cfg.PublicIP = "203.0.113.10"
	cfg.EgressInterface = "eth0"
	cfg.DefaultClients = []string{"phone"}
	cfg.DryRun = true
	manager := gateway.NewManager(gateway.NewStore(filepath.Join(dir, "state.json")), gateway.ExecRunner{}, cfg)
	if err := manager.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(manager, "admin", "test-password", "test")
}

func request(t *testing.T, handler http.Handler, method, path string, body []byte, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if authenticated {
		req.SetBasicAuth("admin", "test-password")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAuthenticationAndHealth(t *testing.T) {
	handler := testServer(t)
	if got := request(t, handler, http.MethodGet, "/api/clients", nil, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", got)
	}
	if got := request(t, handler, http.MethodGet, "/healthz", nil, false).Code; got != http.StatusOK {
		t.Fatalf("health status = %d", got)
	}
}

func TestClientAPIAndArtifacts(t *testing.T) {
	handler := testServer(t)
	recorder := request(t, handler, http.MethodPost, "/api/clients", []byte(`{"name":"tablet"}`), true)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var client gateway.PublicClient
	if err := json.Unmarshal(recorder.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/clients/" + client.ID + "/wireguard.conf",
		"/api/clients/" + client.ID + "/wireguard.png",
		"/api/clients/" + client.ID + "/vless.txt",
		"/api/clients/" + client.ID + "/vless.png",
		"/api/clients/" + client.ID + "/clash.yaml",
		"/api/clients/" + client.ID + "/vps-config.json",
	} {
		if got := request(t, handler, http.MethodGet, path, nil, true).Code; got != http.StatusOK {
			t.Errorf("GET %s status=%d", path, got)
		}
	}

	vpsConfigResponse := request(t, handler, http.MethodGet, "/api/clients/"+client.ID+"/vps-config.json", nil, true)
	if got := vpsConfigResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("vps config cache control = %q", got)
	}
	if got := vpsConfigResponse.Header().Get("Content-Disposition"); got != `attachment; filename="tablet.vps-config.json"` {
		t.Fatalf("vps config content disposition = %q", got)
	}
	var configuration gateway.VPSConfig
	if err := json.Unmarshal(vpsConfigResponse.Body.Bytes(), &configuration); err != nil {
		t.Fatal(err)
	}
	if configuration.Schema != gateway.VPSConfigSchema || configuration.Version != gateway.VPSConfigVersion {
		t.Fatalf("unexpected vps config identity: %#v", configuration)
	}
	if configuration.Client.ID != client.ID || configuration.Client.Name != "tablet" {
		t.Fatalf("unexpected vps config client: %#v", configuration.Client)
	}
	if configuration.Endpoint.Address != "203.0.113.10" || configuration.Endpoint.ExpectedEgress != "203.0.113.10" {
		t.Fatalf("unexpected vps config endpoint: %#v", configuration.Endpoint)
	}
	if configuration.VLESS.UUID == "" || configuration.VLESS.TLS.Reality.PublicKey == "" || configuration.VLESS.TLS.Reality.ShortID == "" {
		t.Fatal("vps config is missing client credentials")
	}
	for _, forbidden := range []string{"wireGuardPrivateKey", "privateKey", "adminPassword"} {
		if strings.Contains(vpsConfigResponse.Body.String(), forbidden) {
			t.Fatalf("vps config leaked forbidden field %q", forbidden)
		}
	}
}
