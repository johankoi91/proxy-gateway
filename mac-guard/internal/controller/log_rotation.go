package controller

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

func (c *Controller) rotateLogs() {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	for _, path := range []string{
		c.cfg.EventLogFile,
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "controller.log"),
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "controller.error.log"),
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "sing-box.log"),
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "sing-box.error.log"),
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "pf-bootstrap.log"),
		filepath.Join(filepath.Dir(c.cfg.EventLogFile), "pf-bootstrap.error.log"),
	} {
		if path == "" {
			continue
		}
		if err := rotateFile(path, c.cfg.LogMaxBytes, c.cfg.LogBackupCount); err != nil {
			log.Printf("[warning] log rotation for %s failed: %v", filepath.Base(path), err)
		}
	}
}

func rotateFile(path string, maxBytes int64, backups int) error {
	if maxBytes < 1 || backups < 1 {
		return nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= maxBytes {
		return nil
	}
	for index := backups; index >= 2; index-- {
		older := fmt.Sprintf("%s.%d", path, index-1)
		newer := fmt.Sprintf("%s.%d", path, index)
		if err := os.Rename(older, newer); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	source, err := os.OpenFile(path, os.O_RDWR, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(path+".1", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return err
	}
	if err := destination.Sync(); err != nil {
		destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	if err := source.Truncate(0); err != nil {
		return err
	}
	_, err = source.Seek(0, 0)
	return err
}
