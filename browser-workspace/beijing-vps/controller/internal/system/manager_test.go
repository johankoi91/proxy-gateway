package system

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"lightnode-linux-controller/internal/config"
)

type fakeRunner struct {
	outputs map[string]string
	calls   []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	output, ok := r.outputs[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
	return []byte(output), nil
}

func TestDNSConfigured(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]string{
		"/usr/bin/resolvectl dns enp0s1":           "Link 2 (enp0s1): 172.19.0.2\n",
		"/usr/bin/resolvectl domain enp0s1":        "Link 2 (enp0s1): ~.\n",
		"/usr/bin/resolvectl default-route enp0s1": "Link 2 (enp0s1): yes\n",
	}}
	manager := New(runner)
	ok, err := manager.dnsConfigured(context.Background(), config.Controller{PhysicalInterface: "enp0s1", TunnelDNS: "172.19.0.2"})
	if err != nil || !ok {
		t.Fatalf("expected configured DNS, ok=%v err=%v", ok, err)
	}
}

func TestDNSConfiguredRejectsDrift(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]string{
		"/usr/bin/resolvectl dns enp0s1": "Link 2 (enp0s1): 10.211.55.1\n",
	}}
	manager := New(runner)
	ok, err := manager.dnsConfigured(context.Background(), config.Controller{PhysicalInterface: "enp0s1", TunnelDNS: "172.19.0.2"})
	if err != nil || ok {
		t.Fatalf("expected DNS drift, ok=%v err=%v", ok, err)
	}
}

func TestPriorityRuleCount(t *testing.T) {
	count, err := priorityRuleCount([]byte(`[{"priority":0},{"priority":1},{"priority":9000},{"priority":1}]`), 1)
	if err != nil || count != 2 {
		t.Fatalf("unexpected priority rule count: count=%d err=%v", count, err)
	}
}

func TestRecoveryThresholdCooldownAndLimit(t *testing.T) {
	runtime := &ContainerRuntime{desiredRunning: true, provider: &exec.Cmd{}}
	now := time.Unix(1_000_000, 0)
	for i := 1; i < recoveryFailureThreshold; i++ {
		decision, _, _ := runtime.recordRecoveryProbe(false, now)
		if decision != recoveryNone {
			t.Fatalf("unexpected recovery before threshold: %v", decision)
		}
	}
	decision, failures, attempts := runtime.recordRecoveryProbe(false, now)
	if decision != recoveryRestart || failures != recoveryFailureThreshold || attempts != 1 {
		t.Fatalf("unexpected first recovery: decision=%v failures=%d attempts=%d", decision, failures, attempts)
	}
	for i := 0; i < recoveryFailureThreshold; i++ {
		decision, _, _ = runtime.recordRecoveryProbe(false, now.Add(time.Minute))
	}
	if decision != recoveryNone {
		t.Fatal("cooldown should suppress recovery")
	}
	decision, _, attempts = runtime.recordRecoveryProbe(false, now.Add(recoveryCooldown))
	if decision != recoveryRestart || attempts != 2 {
		t.Fatalf("unexpected second recovery: decision=%v attempts=%d", decision, attempts)
	}
	for i := 0; i < recoveryFailureThreshold; i++ {
		decision, _, attempts = runtime.recordRecoveryProbe(false, now.Add(2*recoveryCooldown))
	}
	if decision != recoveryRestart || attempts != 3 {
		t.Fatalf("unexpected third recovery: decision=%v attempts=%d", decision, attempts)
	}
	for i := 0; i < recoveryFailureThreshold; i++ {
		decision, _, _ = runtime.recordRecoveryProbe(false, now.Add(3*recoveryCooldown))
	}
	if decision != recoveryExhausted {
		t.Fatalf("expected exhausted decision, got %v", decision)
	}
	runtime.recordRecoveryProbe(true, now.Add(5*recoveryCooldown))
	if runtime.recoveryAttempts != 0 || runtime.recoveryFailures != 0 {
		t.Fatal("healthy probe did not reset recovery state")
	}
}
