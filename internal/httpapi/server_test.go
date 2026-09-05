package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	} {
		if got := request(t, handler, http.MethodGet, path, nil, true).Code; got != http.StatusOK {
			t.Errorf("GET %s status=%d", path, got)
		}
	}
}
