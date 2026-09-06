package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lightnode-mac-guard/internal/model"
)

func testEngine(t *testing.T, profile model.PolicyProfile, apiURL string) *Engine {
	t.Helper()
	directory := t.TempDir()
	document := Document{
		Version: 1,
		Mode:    model.PolicyModeEnforce,
		Profile: profile,
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	policyFile := filepath.Join(directory, "policy.json")
	if err := os.WriteFile(policyFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if apiURL == "" {
		apiURL = "http://127.0.0.1:9090"
	}
	engine, err := New(Options{
		PolicyFile:     policyFile,
		ClashAPIURL:    apiURL,
		ClashAPISecret: "test-secret",
		PollInterval:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestGlobalJapanHasNoConnectionClassification(t *testing.T) {
	engine := testEngine(t, model.PolicyProfileGlobalJapan, "")
	if !engine.connectionCompliant("tokyo") {
		t.Fatal("the single Tokyo outbound must be compliant")
	}
	if engine.connectionCompliant("direct") {
		t.Fatal("any non-Tokyo outbound must be a violation")
	}
}

func TestClashAPIPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connections": []any{map[string]any{
				"id": "one",
				"metadata": map[string]string{
					"network": "tcp", "destinationIP": "203.0.113.9",
					"destinationPort": "443", "host": "unknown.example",
					"processPath": "/Applications/Claude.app/Contents/MacOS/Claude (501)",
				},
				"upload": 12, "download": 34, "start": time.Now().UTC(),
				"chains": []string{"tokyo"}, "rule": "final",
			}},
		})
	}))
	defer server.Close()
	engine := testEngine(t, model.PolicyProfileGlobalJapan, server.URL)
	engine.poll(context.Background())
	snapshot := engine.Snapshot()
	if snapshot.ObserverHealth != model.HealthHealthy || len(snapshot.ActiveConnections) != 1 || snapshot.ViolationCount != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.ActiveConnections[0].ProcessPath != "/Applications/Claude.app/Contents/MacOS/Claude" {
		t.Fatalf("process path was not sanitized: %q", snapshot.ActiveConnections[0].ProcessPath)
	}
}

func TestClashAPIRejectsNonLoopbackURL(t *testing.T) {
	if _, err := newClashClient("http://example.com:9090", ""); err == nil {
		t.Fatal("non-loopback Clash API URL must be rejected")
	}
}

func TestClashAPISecretIsRequired(t *testing.T) {
	if _, err := New(Options{PolicyFile: "unused", ClashAPIURL: "http://127.0.0.1:9090"}); err == nil {
		t.Fatal("empty Clash API secret must be rejected")
	}
}

func TestLegacyObserveModeIsRejected(t *testing.T) {
	directory := t.TempDir()
	policyFile := filepath.Join(directory, "policy.json")
	data := []byte(`{
  "version": 1,
  "mode": "observe_safe",
  "profile": "global_japan"
}`)
	if err := os.WriteFile(policyFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{
		PolicyFile: policyFile, ClashAPIURL: "http://127.0.0.1:9090",
		ClashAPISecret: "test-secret", PollInterval: time.Second,
	}); err == nil {
		t.Fatal("legacy observe mode must be rejected")
	}
}

func TestLegacyOutboundIsViolation(t *testing.T) {
	engine := testEngine(t, model.PolicyProfileGlobalJapan, "")
	if engine.connectionCompliant("ai-fixed") || engine.connectionCompliant("vless-out") {
		t.Fatal("legacy outbound must not satisfy the global Japan policy")
	}
}
