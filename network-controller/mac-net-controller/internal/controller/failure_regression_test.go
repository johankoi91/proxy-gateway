package controller

import (
	"path/filepath"
	"testing"
	"time"

	"lightnode-mac-controller/internal/model"
)

func TestFailureScenariosRemainFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		failed []string
	}{
		{name: "Wi-Fi or interface switch", failed: []string{"firewall_binding"}},
		{name: "VPS unavailable", failed: []string{"transport", "egress"}},
		{name: "sing-box crash", failed: []string{"provider", "provider_service", "tunnel"}},
		{name: "DNS corruption", failed: []string{"dns"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			cfg := testControllerConfig()
			cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
			cfg.MaintenanceFile = filepath.Join(directory, "paused")
			cfg.FirewallRepairCommand = nil
			cfg.DNSRepairCommand = nil
			cfg.ConflictAutoRemediate = false
			controller := NewController(cfg, ipv6Runner{}, "test")
			output := healthyRegressionOutput()
			controller.apply(output)
			if snapshot := controller.Snapshot(); snapshot.State != model.StateHealthy || !snapshot.Protected {
				t.Fatalf("healthy baseline = %#v", snapshot)
			}
			for _, id := range test.failed {
				setRegressionHealth(&output, id, model.HealthFailed)
			}
			controller.apply(output)
			snapshot := controller.Snapshot()
			if snapshot.State != model.StateDegradedLocked || !snapshot.Protected || snapshot.Connected {
				t.Fatalf("failure state=%s protected=%t connected=%t", snapshot.State, snapshot.Protected, snapshot.Connected)
			}
			controller.apply(healthyRegressionOutput())
			if recovered := controller.Snapshot(); recovered.State != model.StateHealthy || !recovered.Protected || !recovered.Connected {
				t.Fatalf("recovered state = %#v", recovered)
			}
		})
	}
}

func healthyRegressionOutput() checkOutput {
	now := time.Now().UTC()
	ids := []string{
		"network_conflict", "provider", "provider_service", "controller_service", "tunnel",
		"firewall", "firewall_config", "firewall_binding", "pf_bootstrap_service", "config",
		"transport", "egress", "dns", "ipv6",
	}
	components := make([]model.Component, 0, len(ids))
	for _, id := range ids {
		components = append(components, model.Component{ID: id, Name: id, Health: model.HealthHealthy, Required: true, Summary: "ok", LastCheckedAt: now})
	}
	return checkOutput{components: components, observedIP: "149.104.78.121", physicalIF: "en0", tunnelIF: "utun8"}
}

func setRegressionHealth(output *checkOutput, id string, health model.Health) {
	for index := range output.components {
		if output.components[index].ID == id {
			output.components[index].Health = health
			output.components[index].Summary = "simulated failure"
		}
	}
}
