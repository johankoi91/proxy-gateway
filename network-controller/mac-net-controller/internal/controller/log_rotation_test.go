package controller

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRotateFileUsesBoundedCopyTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.log")
	payload := bytes.Repeat([]byte("x"), 2048)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateFile(path, 1024, 2); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 0 || !bytes.Equal(rotated, payload) {
		t.Fatalf("current=%d rotated=%d", len(current), len(rotated))
	}
}
