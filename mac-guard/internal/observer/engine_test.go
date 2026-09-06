package observer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lightnode-mac-controller/internal/model"
)

func testObserver(t *testing.T, apiURL string) *Observer {
	t.Helper()
	if apiURL == "" {
		apiURL = "http://127.0.0.1:9090"
	}
	observer, err := New(Options{APIURL: apiURL, APISecret: "test-secret", PollInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return observer
}

func TestOnlyTokyoOutboundIsCompliant(t *testing.T) {
	if !connectionCompliant([]string{"tokyo"}) {
		t.Fatal("the single Tokyo outbound must be compliant")
	}
	for _, chains := range [][]string{{"direct"}, {"other"}, {"not-tokyo"}, {"tokyo-backup"}, nil} {
		if connectionCompliant(chains) {
			t.Fatalf("non-Tokyo chain must be a violation: %#v", chains)
		}
	}
	if !connectionCompliant([]string{"dns", "tokyo"}) {
		t.Fatal("an exact Tokyo hop in a multi-hop chain must be compliant")
	}
}

func TestResolvedViolationsAreForgotten(t *testing.T) {
	observer := testObserver(t, "http://127.0.0.1:9090")
	now := time.Now().UTC()
	connection := rawConnection{ID: "connection-1", Chains: []string{"direct"}}
	observer.observe([]rawConnection{connection}, now)
	if len(observer.reportedViolations) != 1 {
		t.Fatalf("active violation count = %d", len(observer.reportedViolations))
	}
	observer.observe(nil, now.Add(time.Second))
	if len(observer.reportedViolations) != 0 {
		t.Fatalf("resolved violations retained in memory: %d", len(observer.reportedViolations))
	}
}

func TestConnectionAPIPoll(t *testing.T) {
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
					"destinationPort": "443", "host": "service.example",
					"processPath": "/Applications/Browser.app/Contents/MacOS/Browser (501)",
				},
				"upload": 12, "download": 34, "start": time.Now().UTC(),
				"chains": []string{"tokyo"}, "rule": "final",
			}},
		})
	}))
	defer server.Close()
	observer := testObserver(t, server.URL)
	observer.poll(context.Background())
	snapshot := observer.Snapshot()
	if snapshot.Health != model.HealthHealthy || len(snapshot.ActiveConnections) != 1 || snapshot.ViolationCount != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.ActiveConnections[0].ProcessPath != "/Applications/Browser.app/Contents/MacOS/Browser" {
		t.Fatalf("process path was not sanitized: %q", snapshot.ActiveConnections[0].ProcessPath)
	}
}

func TestConnectionAPIRejectsNonLoopbackURL(t *testing.T) {
	if _, err := newConnectionAPIClient("http://example.com:9090", ""); err == nil {
		t.Fatal("non-loopback connection API URL must be rejected")
	}
}

func TestConnectionAPISecretIsRequired(t *testing.T) {
	if _, err := New(Options{APIURL: "http://127.0.0.1:9090"}); err == nil {
		t.Fatal("empty connection API secret must be rejected")
	}
}
