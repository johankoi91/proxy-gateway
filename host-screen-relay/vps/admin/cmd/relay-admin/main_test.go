package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T) *app {
	t.Helper()
	root := t.TempDir()
	clientsDir := filepath.Join(root, "clients")
	if err := os.MkdirAll(clientsDir, 0700); err != nil {
		t.Fatal(err)
	}
	add := filepath.Join(root, "add")
	remove := filepath.Join(root, "remove")
	addScript := `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  case "$1" in --name) name="$2";; --address) address="$2";; esac
  shift 2
done
mkdir -p "` + clientsDir + `/$(printf %s "$name")"
printf 'test-public-key\n' > "` + clientsDir + `/$(printf %s "$name")/public.key"
printf '[Interface]\nPrivateKey = secret\nAddress = %s/24\n' "$address" > "` + clientsDir + `/$(printf %s "$name")/client.conf"
`
	removeScript := `#!/bin/sh
set -eu
rm -rf "` + clientsDir + `/$(printf %s "$1")"
`
	if err := os.WriteFile(add, []byte(addScript), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remove, []byte(removeScript), 0700); err != nil {
		t.Fatal(err)
	}
	hostConfig := filepath.Join(root, "host.conf")
	hostPackage := filepath.Join(root, "HostScreenRelay.pkg")
	if err := os.WriteFile(hostConfig, []byte("PrivateKey = host-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPackage, []byte("test-pkg"), 0600); err != nil {
		t.Fatal(err)
	}
	return &app{username: "admin", password: "password", clientsDir: clientsDir, addCommand: add, removeCommand: remove, endpoint: "example.test:443", hostConfig: hostConfig, hostPackage: hostPackage}
}

func request(a *app, method, path, body string, authenticate bool) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authenticate {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:password")))
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/api/clients", a.auth(a.clientsRoute))
	handler.HandleFunc("/api/clients/", a.auth(a.clientRoute))
	noStore(handler).ServeHTTP(recorder, req)
	return recorder
}

func TestHostBundle(t *testing.T) {
	a := testApp(t)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/host/bundle.zip", nil)
	req.SetBasicAuth("admin", "password")
	a.auth(method(http.MethodGet, a.downloadHostBundle))(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bundle status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	reader, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]os.FileMode{}
	for _, file := range reader.File {
		entries[file.Name] = file.Mode()
	}
	for _, name := range []string{"HostScreenRelay.pkg", "host.conf", "安装 Host Screen Relay.command"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("bundle is missing %q", name)
		}
	}
	if entries["安装 Host Screen Relay.command"]&0100 == 0 {
		t.Fatal("installer command is not executable")
	}
}

func TestClientLifecycle(t *testing.T) {
	a := testApp(t)
	if got := request(a, http.MethodGet, "/api/clients", "", false).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", got)
	}
	created := request(a, http.MethodPost, "/api/clients", `{"name":"alice-mac"}`, true)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), "10.88.0.10") {
		t.Fatalf("create status/body = %d %s", created.Code, created.Body.String())
	}
	download := request(a, http.MethodGet, "/api/clients/alice-mac/client.conf", "", true)
	if download.Code != http.StatusOK || !strings.Contains(download.Body.String(), "PrivateKey = secret") {
		t.Fatalf("download status/body = %d %s", download.Code, download.Body.String())
	}
	if got := download.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	removed := request(a, http.MethodDelete, "/api/clients/alice-mac", "", true)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("delete status/body = %d %s", removed.Code, removed.Body.String())
	}
}

func TestRejectsInvalidAndDuplicateNames(t *testing.T) {
	a := testApp(t)
	bad := request(a, http.MethodPost, "/api/clients", `{"name":"../escape"}`, true)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid name status = %d", bad.Code)
	}
	request(a, http.MethodPost, "/api/clients", `{"name":"same"}`, true)
	duplicate := request(a, http.MethodPost, "/api/clients", `{"name":"same"}`, true)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.Code)
	}
	traversal := request(a, http.MethodGet, "/api/clients/../client.conf", "", true)
	if traversal.Code == http.StatusOK {
		t.Fatal("path traversal unexpectedly succeeded")
	}
}

func TestRejectsCrossOriginMutation(t *testing.T) {
	a := testApp(t)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://relay.example/api/clients", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:password")))
	req.Header.Set("Origin", "http://evil.example")
	a.auth(a.clientsRoute)(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", recorder.Code)
	}
}
