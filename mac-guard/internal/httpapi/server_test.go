package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lightnode-mac-guard/internal/config"
	"lightnode-mac-guard/internal/guard"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, context.DeadlineExceeded
}

func TestAuthenticationAndHealth(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := guard.NewController(cfg, failingRunner{}, "test")
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
	controller := guard.NewController(cfg, failingRunner{}, "test")
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
	controller := guard.NewController(cfg, failingRunner{}, "test")
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

func TestV2PolicyEndpoint(t *testing.T) {
	directory := t.TempDir()
	policyFile := filepath.Join(directory, "policy.json")
	policyJSON := `{
  "version": 1,
  "mode": "enforce",
  "profile": "global_japan",
  "aiProcessNames": ["Claude"],
  "aiProcessPathPrefixes": ["/Applications/Claude.app/"],
  "aiDomainSuffixes": ["claude.ai"]
}`
	if err := os.WriteFile(policyFile, []byte(policyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.V2Enabled = true
	cfg.V2PolicyFile = policyFile
	cfg.V2ClashAPIURL = "http://127.0.0.1:39090"
	cfg.V2ClashAPISecret = "test-secret"
	controller := guard.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/policy", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestLifecycleEndpointsArePostOnly(t *testing.T) {
	cfg := config.Default()
	cfg.EventLogFile = filepath.Join(t.TempDir(), "events.jsonl")
	controller := guard.NewController(cfg, failingRunner{}, "test")
	handler := New(controller, "", "", true)
	for _, path := range []string{"/api/actions/start", "/api/actions/pause"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d", path, response.Code)
		}
	}
}
