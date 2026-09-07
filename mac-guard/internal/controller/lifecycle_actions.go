package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lightnode-mac-controller/internal/model"
)

const (
	actionRunning   = "running"
	actionSucceeded = "succeeded"
	actionFailed    = "failed"
)

const lifecycleActionHistoryLimit = 20

type lifecycleActionJournal struct {
	Version int                     `json:"version"`
	Actions []model.LifecycleAction `json:"actions"`
}

type lifecycleSpec struct {
	command   []string
	started   string
	succeeded string
	failed    string
}

func (c *Controller) QueueLifecycleAction(kind string) (model.LifecycleAction, error) {
	spec, err := c.lifecycleSpec(kind)
	if err != nil {
		return model.LifecycleAction{}, err
	}
	if kind == "pause" && !c.snapshotProtected() {
		return model.LifecycleAction{}, errors.New("PF Kill Switch 未生效，拒绝暂停隧道")
	}
	if kind == "restart" && c.safelyPaused() {
		return model.LifecycleAction{}, errors.New("保护已安全暂停，请使用启动保护")
	}
	if !c.operationMu.TryLock() {
		return model.LifecycleAction{}, errors.New("另一个网络操作正在执行")
	}
	now := time.Now().UTC()
	action := model.LifecycleAction{
		ID: fmt.Sprintf("%d-%d", now.UnixMilli(), c.actionID.Add(1)), Kind: kind,
		Status: actionRunning, Phase: "queued", Message: spec.started, StartedAt: now,
	}
	if err := c.setLifecycleAction(action); err != nil {
		c.operationMu.Unlock()
		return model.LifecycleAction{}, err
	}
	go c.executeLifecycleAction(action, spec, false)
	return action, nil
}

func (c *Controller) CurrentLifecycleAction() *model.LifecycleAction {
	c.actionMu.RLock()
	defer c.actionMu.RUnlock()
	if c.currentAction == nil {
		return nil
	}
	copy := *c.currentAction
	return &copy
}

func (c *Controller) LifecycleAction(id string) (model.LifecycleAction, error) {
	c.actionMu.RLock()
	defer c.actionMu.RUnlock()
	action, ok := c.actions[id]
	if !ok {
		return model.LifecycleAction{}, errors.New("操作记录不存在")
	}
	return action, nil
}

func (c *Controller) executeLifecycleAction(action model.LifecycleAction, spec lifecycleSpec, resumed bool) {
	defer c.operationMu.Unlock()
	if resumed {
		action.Message = "Controller 重启后继续执行: " + spec.started
	}
	action.Phase = "executing"
	_ = c.setLifecycleAction(action)
	c.mu.Lock()
	c.snapshot.Recovering = true
	c.transitionLocked(model.StateRecovering, "controller", action.Message)
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.LifecycleActionTimeout())
	output, err := c.runner.Run(ctx, spec.command[0], spec.command[1:]...)
	cancel()
	c.recordLifecycleOutput(output)

	action.Phase = "verifying"
	action.Message = "正在验证操作结果"
	_ = c.setLifecycleAction(action)
	c.CheckNow(context.Background())

	if err == nil {
		snapshot := c.Snapshot()
		switch action.Kind {
		case "start", "restart":
			if snapshot.State != model.StateHealthy {
				err = fmt.Errorf("操作后状态为 %s", snapshot.State)
			}
		case "pause":
			if snapshot.State != model.StateMaintenance {
				err = fmt.Errorf("操作后状态为 %s", snapshot.State)
			}
		}
	}

	finishedAt := time.Now().UTC()
	action.FinishedAt = &finishedAt
	action.Phase = "finished"
	if err != nil {
		action.Status = actionFailed
		action.Message = spec.failed + ": " + err.Error()
	} else {
		action.Status = actionSucceeded
		action.Message = spec.succeeded
	}
	_ = c.setLifecycleAction(action)
	c.mu.Lock()
	c.snapshot.Recovering = false
	severity := "info"
	if err != nil {
		severity = "error"
	}
	c.addEventLocked(severity, "provider", action.Message)
	c.mu.Unlock()
}

func (c *Controller) lifecycleSpec(kind string) (lifecycleSpec, error) {
	var spec lifecycleSpec
	switch kind {
	case "start":
		spec = lifecycleSpec{c.cfg.StartCommand, "正在启动东京保护", "东京保护启动完成", "东京保护启动失败"}
	case "pause":
		spec = lifecycleSpec{c.cfg.PauseCommand, "正在安全暂停东京隧道", "东京隧道已安全暂停，公网保持阻断", "安全暂停失败"}
	case "restart":
		spec = lifecycleSpec{c.cfg.RecoveryCommand, "正在重启东京线路", "东京线路重启完成", "东京线路重启失败"}
	default:
		return lifecycleSpec{}, fmt.Errorf("不支持的生命周期操作 %q", kind)
	}
	if len(spec.command) == 0 {
		return lifecycleSpec{}, errors.New("生命周期命令未配置")
	}
	return spec, nil
}

func (c *Controller) recordLifecycleOutput(output []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if warning, found := strings.CutPrefix(line, "WARNING: "); found {
			c.addEventLocked("warning", "provider", warning)
		} else if information, found := strings.CutPrefix(line, "INFO: "); found {
			c.addEventLocked("info", "provider", information)
		}
	}
}

func (c *Controller) setLifecycleAction(action model.LifecycleAction) error {
	c.actionMu.Lock()
	if _, exists := c.actions[action.ID]; !exists {
		c.actionOrder = append([]string{action.ID}, c.actionOrder...)
		if len(c.actionOrder) > lifecycleActionHistoryLimit {
			for _, id := range c.actionOrder[lifecycleActionHistoryLimit:] {
				delete(c.actions, id)
			}
			c.actionOrder = c.actionOrder[:lifecycleActionHistoryLimit]
		}
	}
	c.actions[action.ID] = action
	c.currentAction = &action
	journal := lifecycleActionJournal{Version: 1, Actions: make([]model.LifecycleAction, 0, len(c.actionOrder))}
	for _, id := range c.actionOrder {
		journal.Actions = append(journal.Actions, c.actions[id])
	}
	c.actionMu.Unlock()

	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	if c.cfg.LifecycleActionFile != "" {
		if err := os.MkdirAll(filepath.Dir(c.cfg.LifecycleActionFile), 0o700); err != nil {
			return err
		}
		if err := writeNodeFileAtomic(c.cfg.LifecycleActionFile, append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) loadLifecycleAction() {
	if c.cfg.LifecycleActionFile == "" {
		return
	}
	data, err := os.ReadFile(c.cfg.LifecycleActionFile)
	if err != nil {
		return
	}
	var journal lifecycleActionJournal
	if json.Unmarshal(data, &journal) != nil || journal.Version != 1 || len(journal.Actions) == 0 {
		return
	}
	for _, action := range journal.Actions {
		if action.ID == "" {
			continue
		}
		c.actions[action.ID] = action
		c.actionOrder = append(c.actionOrder, action.ID)
	}
	if len(c.actionOrder) > 0 {
		action := c.actions[c.actionOrder[0]]
		c.currentAction = &action
	}
}

func (c *Controller) resumeLifecycleAction() {
	action := c.CurrentLifecycleAction()
	if action == nil || action.Status != actionRunning {
		return
	}
	spec, err := c.lifecycleSpec(action.Kind)
	if err != nil || !c.operationMu.TryLock() {
		return
	}
	go c.executeLifecycleAction(*action, spec, true)
}
