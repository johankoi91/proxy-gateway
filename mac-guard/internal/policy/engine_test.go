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
		Version:               1,
		Mode:                  model.PolicyModeEnforce,
		Profile:               profile,
		AIProcessNames:        []string{"Claude", "ChatGPT"},
		AIProcessPathPrefixes: []string{"/Applications/Claude.app/", "/Applications/ChatGPT.app/"},
		AIDomainSuffixes:      []string{"claude.ai", "openai.com"},
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

func connection(processPath, host, ip string) rawConnection {
	var value rawConnection
	value.ID = "connection-1"
	value.Metadata.ProcessPath = processPath
	value.Metadata.Host = host
	value.Metadata.DestinationIP = ip
	value.Metadata.DestinationPort = "443"
	value.Metadata.Network = "tcp"
	value.Start = time.Now().UTC()
	value.Chains = []string{"ai-fixed"}
	return value
}

func TestGlobalJapanClassification(t *testing.T) {
	engine := testEngine(t, model.PolicyProfileGlobalJapan, "")
	tests := []struct {
		name       string
		connection rawConnection
		route      model.PolicyRoute
		ai         bool
	}{
		{name: "Claude process", connection: connection("/Applications/Claude.app/Contents/MacOS/Claude (501)", "new.example", "203.0.113.1"), route: model.PolicyRouteAIFixed, ai: true},
		{name: "browser AI domain", connection: connection("/Applications/Safari.app/Contents/MacOS/Safari", "chat.openai.com", "104.18.0.2"), route: model.PolicyRouteAIFixed},
		{name: "China domain", connection: connection("/Applications/WeChat.app/Contents/MacOS/WeChat", "res.qq.com", "1.1.1.1"), route: model.PolicyRouteJapan},
		{name: "foreign default", connection: connection("/Applications/Safari.app/Contents/MacOS/Safari", "www.youtube.com", "142.251.0.1"), route: model.PolicyRouteJapan},
		{name: "IP-only default", connection: connection("/usr/bin/curl", "", "203.0.113.20"), route: model.PolicyRouteJapan},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := engine.classify(test.connection)
			if actual.Disposition != test.route || actual.IsAIProcess != test.ai {
				t.Fatalf("classification = %#v", actual)
			}
		})
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
				"chains": []string{"ai-fixed"}, "rule": "final",
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
  "profile": "global_japan",
  "aiProcessNames": ["Claude"],
  "aiProcessPathPrefixes": ["/Applications/Claude.app/"],
  "aiDomainSuffixes": ["claude.ai"]
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
	result := engine.classify(connection("/usr/bin/curl", "example.com", "203.0.113.2"))
	if engine.connectionCompliant(result, "vless-out") {
		t.Fatal("legacy outbound must not satisfy the global Japan policy")
	}
}
