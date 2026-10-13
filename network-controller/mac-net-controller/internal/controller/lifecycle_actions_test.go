package controller

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"lightnode-mac-controller/internal/config"
	"lightnode-mac-controller/internal/model"
)

type blockingActionRunner struct {
	command   string
	started   chan context.Context
	release   chan struct{}
	once      sync.Once
	resultErr error
}

func (r *blockingActionRunner) Run(ctx context.Context, name string, _ ...string) ([]byte, error) {
	if name != r.command {
		return nil, errors.New("diagnostic command unavailable")
	}
	r.once.Do(func() { r.started <- ctx })
	select {
	case <-r.release:
		return []byte("INFO: detached action completed\n"), r.resultErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func actionTestConfig(t *testing.T) config.Config {
	t.Helper()
	directory := t.TempDir()
	cfg := testControllerConfig()
	cfg.ConfigFilePath = filepath.Join(directory, "config.json")
	cfg.EventLogFile = filepath.Join(directory, "events.jsonl")
	cfg.LifecycleActionFile = filepath.Join(directory, "lifecycle-action.json")
	cfg.MaintenanceFile = filepath.Join(directory, "safely-paused")
	cfg.StartCommand = []string{"test-start"}
	cfg.PauseCommand = []string{"test-pause"}
	cfg.RecoveryCommand = []string{"test-restart"}
	cfg.CheckTimeoutSeconds = 1
	cfg.LifecycleActionTimeoutSeconds = 30
	return cfg
}

func TestLifecycleActionIsDetachedAndSerialized(t *testing.T) {
	cfg := actionTestConfig(t)
	runner := &blockingActionRunner{command: "test-start", started: make(chan context.Context, 1), release: make(chan struct{})}
	controller := NewController(cfg, runner, "test")
	action, err := controller.QueueLifecycleAction("start")
	if err != nil {
		t.Fatal(err)
	}
	actionContext := <-runner.started
	if actionContext.Err() != nil {
		t.Fatalf("detached action context is already canceled: %v", actionContext.Err())
	}
	if _, err := controller.QueueLifecycleAction("start"); err == nil {
		t.Fatal("a concurrent lifecycle action was accepted")
	}
	data, err := os.ReadFile(cfg.LifecycleActionFile)
	if err != nil {
		t.Fatal(err)
	}
	var persisted lifecycleActionJournal
	if err := json.Unmarshal(data, &persisted); err != nil || len(persisted.Actions) != 1 || persisted.Actions[0].ID != action.ID || persisted.Actions[0].Status != actionRunning {
		t.Fatalf("persisted journal=%#v err=%v", persisted, err)
	}
	close(runner.release)
	waitForActionTerminal(t, controller, action.ID)
}

func TestInterruptedLifecycleActionResumes(t *testing.T) {
	cfg := actionTestConfig(t)
	interrupted := model.LifecycleAction{
		ID: "interrupted-1", Kind: "start", Status: actionRunning,
		Phase: "executing", Message: "interrupted", StartedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(lifecycleActionJournal{Version: 1, Actions: []model.LifecycleAction{interrupted}})
	if err := os.WriteFile(cfg.LifecycleActionFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &blockingActionRunner{command: "test-start", started: make(chan context.Context, 1), release: make(chan struct{})}
	controller := NewController(cfg, runner, "test")
	controller.resumeLifecycleAction()
	select {
	case actionContext := <-runner.started:
		if actionContext.Err() != nil {
			t.Fatalf("resumed action context is canceled: %v", actionContext.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("interrupted action was not resumed")
	}
	close(runner.release)
	waitForActionTerminal(t, controller, interrupted.ID)
}

func TestLifecycleActionHistoryRemainsQueryable(t *testing.T) {
	cfg := actionTestConfig(t)
	controller := NewController(cfg, &blockingActionRunner{}, "test")
	first := model.LifecycleAction{ID: "first", Kind: "start", Status: actionSucceeded, StartedAt: time.Now().UTC()}
	second := model.LifecycleAction{ID: "second", Kind: "pause", Status: actionSucceeded, StartedAt: time.Now().UTC()}
	if err := controller.setLifecycleAction(first); err != nil {
		t.Fatal(err)
	}
	if err := controller.setLifecycleAction(second); err != nil {
		t.Fatal(err)
	}
	if action, err := controller.LifecycleAction(first.ID); err != nil || action.ID != first.ID {
		t.Fatalf("first action was replaced: %#v err=%v", action, err)
	}
	reloaded := NewController(cfg, &blockingActionRunner{}, "test")
	if action, err := reloaded.LifecycleAction(first.ID); err != nil || action.ID != first.ID {
		t.Fatalf("persisted first action unavailable: %#v err=%v", action, err)
	}
}

func TestPausedStateStillRepairsFirewall(t *testing.T) {
	cfg := actionTestConfig(t)
	if err := os.WriteFile(cfg.MaintenanceFile, []byte("paused\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join(filepath.Dir(cfg.MaintenanceFile), "firewall.committed")
	if err := os.WriteFile(committed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.FirewallCommittedFile = committed
	cfg.FirewallRepairCommand = []string{"repair-firewall"}
	started := make(chan context.Context, 1)
	runner := &blockingActionRunner{command: "repair-firewall", started: started, release: make(chan struct{}), resultErr: errors.New("simulated repair failure")}
	controller := NewController(cfg, runner, "test")
	output := checkOutput{components: []model.Component{
		{ID: "firewall", Health: model.HealthFailed, Required: true},
		{ID: "provider", Health: model.HealthFailed, Required: true},
		{ID: "tunnel", Health: model.HealthFailed, Required: true},
		{ID: "egress", Health: model.HealthFailed, Required: true},
	}}
	controller.apply(output)
	controller.reconcileAfterCheck(output)
	select {
	case <-started:
		close(runner.release)
	case <-time.After(time.Second):
		t.Fatal("paused state skipped PF repair")
	}
}

func TestInconsistentPauseQueuesEnforcement(t *testing.T) {
	cfg := actionTestConfig(t)
	if err := os.WriteFile(cfg.MaintenanceFile, []byte("paused\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &blockingActionRunner{command: "test-pause", started: make(chan context.Context, 1), release: make(chan struct{})}
	controller := NewController(cfg, runner, "test")
	output := checkOutput{components: []model.Component{
		{ID: "firewall", Health: model.HealthHealthy, Required: true},
		{ID: "provider", Health: model.HealthHealthy, Required: true},
		{ID: "tunnel", Health: model.HealthHealthy, Required: true},
		{ID: "egress", Health: model.HealthHealthy, Required: true},
	}}
	controller.apply(output)
	controller.reconcileAfterCheck(output)
	select {
	case <-runner.started:
		action := controller.CurrentLifecycleAction()
		close(runner.release)
		if action == nil {
			t.Fatal("pause action was not recorded")
		}
		waitForActionTerminal(t, controller, action.ID)
	case <-time.After(time.Second):
		t.Fatal("inconsistent paused runtime was not re-paused")
	}
}

func waitForActionTerminal(t *testing.T, controller *Controller, id string) model.LifecycleAction {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		action, err := controller.LifecycleAction(id)
		if err == nil && action.Status != actionRunning {
			return action
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("lifecycle action did not finish")
	return model.LifecycleAction{}
}
