package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lightnode-mac-controller/internal/model"
)

const (
	backupSchema  = "lightnode.controller-backup"
	backupVersion = 1
)

var backupIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

type backupDocument struct {
	Schema           string               `json:"schema"`
	Version          int                  `json:"version"`
	Metadata         model.BackupMetadata `json:"metadata"`
	ControllerConfig json.RawMessage      `json:"controllerConfig"`
	SingBoxConfig    json.RawMessage      `json:"singBoxConfig"`
}

func (c *Controller) ListBackups() ([]model.BackupMetadata, error) {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()
	return c.listBackupsLocked()
}

func (c *Controller) CreateBackup(reason string) (model.BackupMetadata, error) {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()
	return c.createBackupLocked(reason)
}

func (c *Controller) createBackupLocked(reason string) (model.BackupMetadata, error) {
	controllerConfig, err := os.ReadFile(c.cfg.ConfigFilePath)
	if err != nil {
		return model.BackupMetadata{}, err
	}
	singBoxConfig, err := os.ReadFile(c.cfg.SingBoxConfigFile)
	if err != nil {
		return model.BackupMetadata{}, err
	}
	return c.createBackupFromDocumentsLocked(reason, controllerConfig, singBoxConfig)
}

func (c *Controller) createBackupFromDocumentsLocked(reason string, controllerConfig, singBoxConfig []byte) (model.BackupMetadata, error) {
	configuration, err := nodeConfigurationFromDocuments(controllerConfig, singBoxConfig)
	if err != nil {
		return model.BackupMetadata{}, err
	}
	id, err := newBackupID()
	if err != nil {
		return model.BackupMetadata{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	if len(reason) > 80 {
		reason = reason[:80]
	}
	metadata := model.BackupMetadata{
		ID: id, CreatedAt: time.Now().UTC(), Reason: reason,
		Server: configuration.Server, Port: configuration.Port, ExpectedEgress: configuration.ExpectedEgress,
	}
	document := backupDocument{
		Schema: backupSchema, Version: backupVersion, Metadata: metadata,
		ControllerConfig: append(json.RawMessage(nil), controllerConfig...),
		SingBoxConfig:    append(json.RawMessage(nil), singBoxConfig...),
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return model.BackupMetadata{}, err
	}
	if err := os.MkdirAll(c.cfg.BackupDir, 0o700); err != nil {
		return model.BackupMetadata{}, err
	}
	if err := writeNodeFileAtomic(filepath.Join(c.cfg.BackupDir, id+".json"), append(data, '\n')); err != nil {
		return model.BackupMetadata{}, err
	}
	if err := c.pruneBackupsLocked(); err != nil {
		return model.BackupMetadata{}, err
	}
	return metadata, nil
}

func (c *Controller) listBackupsLocked() ([]model.BackupMetadata, error) {
	entries, err := os.ReadDir(c.cfg.BackupDir)
	if os.IsNotExist(err) {
		return []model.BackupMetadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]model.BackupMetadata, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !backupIDPattern.MatchString(id) {
			continue
		}
		document, err := readBackup(filepath.Join(c.cfg.BackupDir, entry.Name()))
		if err == nil {
			result = append(result, document.Metadata)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (c *Controller) pruneBackupsLocked() error {
	backups, err := c.listBackupsLocked()
	if err != nil {
		return err
	}
	if len(backups) <= c.cfg.BackupLimit {
		return nil
	}
	for _, backup := range backups[c.cfg.BackupLimit:] {
		if err := os.Remove(filepath.Join(c.cfg.BackupDir, backup.ID+".json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *Controller) RestoreBackup(parent context.Context, id string) (model.NodeConfiguration, error) {
	if !backupIDPattern.MatchString(id) {
		return model.NodeConfiguration{}, errors.New("备份 ID 无效")
	}
	if !c.operationMu.TryLock() {
		return model.NodeConfiguration{}, errors.New("另一个网络操作正在执行")
	}
	defer c.operationMu.Unlock()
	if err := c.authorizeNodeUpdate(parent); err != nil {
		return model.NodeConfiguration{}, err
	}
	c.checkMu.Lock()
	defer c.checkMu.Unlock()

	document, err := readBackup(filepath.Join(c.cfg.BackupDir, id+".json"))
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	if err := c.validateSingBoxCandidate(parent, document.SingBoxConfig, c.cfg.SingBoxConfigFile); err != nil {
		return model.NodeConfiguration{}, err
	}
	currentController, err := os.ReadFile(c.cfg.ConfigFilePath)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	currentSingBox, err := os.ReadFile(c.cfg.SingBoxConfigFile)
	if err != nil {
		return model.NodeConfiguration{}, err
	}
	if _, err := c.createBackupFromDocumentsLocked("before-restore", currentController, currentSingBox); err != nil {
		return model.NodeConfiguration{}, fmt.Errorf("create restore safety backup: %w", err)
	}

	rollback := func(cause error) (model.NodeConfiguration, error) {
		_ = writeNodeFileAtomic(c.cfg.SingBoxConfigFile, currentSingBox)
		_ = writeNodeFileAtomic(c.cfg.ConfigFilePath, currentController)
		_, _ = c.runner.Run(context.Background(), "/usr/local/libexec/lightnode-controller", "firewall", "refresh")
		_, _ = c.runner.Run(context.Background(), "/usr/local/libexec/lightnode-controller", "tunnel", "start")
		return model.NodeConfiguration{}, cause
	}
	if err := writeNodeFileAtomic(c.cfg.SingBoxConfigFile, document.SingBoxConfig); err != nil {
		return rollback(err)
	}
	if err := writeNodeFileAtomic(c.cfg.ConfigFilePath, document.ControllerConfig); err != nil {
		return rollback(err)
	}
	if _, err := c.runner.Run(parent, "/usr/local/libexec/lightnode-controller", "firewall", "refresh"); err != nil {
		return rollback(fmt.Errorf("refresh PF after restore: %w", err))
	}
	if _, err := c.runner.Run(parent, "/usr/local/libexec/lightnode-controller", "tunnel", "start"); err != nil {
		return rollback(fmt.Errorf("activate restored configuration: %w", err))
	}
	result, err := nodeConfigurationFromDocuments(document.ControllerConfig, document.SingBoxConfig)
	if err != nil {
		return rollback(err)
	}
	c.scheduleControllerRestart()
	return result, nil
}

func readBackup(path string) (backupDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return backupDocument{}, err
	}
	var document backupDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return backupDocument{}, err
	}
	if document.Schema != backupSchema || document.Version != backupVersion || !backupIDPattern.MatchString(document.Metadata.ID) {
		return backupDocument{}, errors.New("备份格式无效")
	}
	return document, nil
}

func newBackupID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
}
