package main

import (
	"archive/zip"
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed webdist/*
var webFiles embed.FS

//go:embed bundle/install.command
var hostInstallCommand []byte

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,47}$`)

type app struct {
	username      string
	password      string
	clientsDir    string
	addCommand    string
	removeCommand string
	endpoint      string
	hostConfig    string
	hostPackage   string
	mu            sync.Mutex
}

type client struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	LastHandshake int64  `json:"lastHandshake"`
	Received      int64  `json:"received"`
	Sent          int64  `json:"sent"`
}

type runtimePeer struct {
	Handshake int64
	Received  int64
	Sent      int64
}

func main() {
	a := &app{
		username:      env("RELAY_ADMIN_USER", "admin"),
		password:      env("RELAY_ADMIN_PASSWORD", "admin"),
		clientsDir:    env("RELAY_CLIENTS_DIR", "/etc/wireguard/clients"),
		addCommand:    env("RELAY_ADD_COMMAND", "/usr/local/sbin/lightnode-add-remote-peer"),
		removeCommand: env("RELAY_REMOVE_COMMAND", "/usr/local/sbin/lightnode-remove-remote-peer"),
		endpoint:      env("RELAY_ENDPOINT", "106.53.243.63:443"),
		hostConfig:    env("RELAY_HOST_CONFIG", "/etc/wireguard/host/host.conf"),
		hostPackage:   env("RELAY_HOST_PACKAGE", "/opt/lightnode-relay-admin/HostScreenRelay.pkg"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", a.auth(method(http.MethodGet, a.status)))
	mux.HandleFunc("/api/clients", a.auth(a.clientsRoute))
	mux.HandleFunc("/api/clients/", a.auth(a.clientRoute))
	mux.HandleFunc("/api/host/config", a.auth(method(http.MethodGet, a.downloadHostConfig)))
	mux.HandleFunc("/api/host/bundle.zip", a.auth(method(http.MethodGet, a.downloadHostBundle)))
	static, err := fs.Sub(webFiles, "webdist")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", a.auth(http.FileServer(http.FS(static)).ServeHTTP))

	addr := env("RELAY_LISTEN", ":9185")
	server := &http.Server{
		Addr:              addr,
		Handler:           noStore(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Guangzhou relay admin listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func method(allowed string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed {
			w.Header().Set("Allow", allowed)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func (a *app) clientsRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/clients" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.listClients(w, r)
	case http.MethodPost:
		a.createClient(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) clientRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/clients/")
	if rest == r.URL.Path || rest == "" || strings.Contains(rest, "..") {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/client.conf") {
		name := strings.TrimSuffix(rest, "/client.conf")
		a.downloadConfig(w, r, name)
		return
	}
	if r.Method == http.MethodDelete && !strings.Contains(rest, "/") {
		a.deleteClient(w, r, rest)
		return
	}
	http.NotFound(w, r)
}

func (a *app) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.username)) == 1
		passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
		if !ok || !userOK || !passwordOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="Guangzhou Remote Access"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			if !sameOrigin(r) {
				http.Error(w, "invalid request origin", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}

func (a *app) status(w http.ResponseWriter, _ *http.Request) {
	wgActive := exec.Command("systemctl", "is-active", "--quiet", "wg-quick@wg0").Run() == nil
	hostOnline := hostRelayOnline()
	_, configErr := os.Stat(a.hostConfig)
	_, packageErr := os.Stat(a.hostPackage)
	writeJSON(w, http.StatusOK, map[string]any{
		"wireGuard":  wgActive,
		"hostRelay":  hostOnline,
		"hostBundle": configErr == nil && packageErr == nil,
	})
}

func hostRelayOnline() bool {
	output, err := exec.Command("wg", "show", "wg0", "dump").Output()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 8 || !strings.Contains(fields[3], "10.211.55.4/32") {
			continue
		}
		stamp, _ := strconv.ParseInt(fields[4], 10, 64)
		return stamp > 0 && time.Since(time.Unix(stamp, 0)) < 3*time.Minute
	}
	return false
}

func (a *app) listClients(w http.ResponseWriter, _ *http.Request) {
	clients, err := a.clients()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (a *app) clients() ([]client, error) {
	entries, err := os.ReadDir(a.clientsDir)
	if errors.Is(err, os.ErrNotExist) {
		return []client{}, nil
	}
	if err != nil {
		return nil, err
	}
	runtime := wireGuardRuntime()
	result := make([]client, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validName.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(a.clientsDir, entry.Name())
		config, err := os.ReadFile(filepath.Join(dir, "client.conf"))
		if err != nil {
			continue
		}
		address := configValue(string(config), "Address")
		address = strings.TrimSuffix(address, "/24")
		publicKeyBytes, _ := os.ReadFile(filepath.Join(dir, "public.key"))
		peer := runtime[strings.TrimSpace(string(publicKeyBytes))]
		result = append(result, client{entry.Name(), address, peer.Handshake, peer.Received, peer.Sent})
	}
	sort.Slice(result, func(i, j int) bool {
		return addressLastOctet(result[i].Address) < addressLastOctet(result[j].Address)
	})
	return result, nil
}

func wireGuardRuntime() map[string]runtimePeer {
	result := map[string]runtimePeer{}
	output, err := exec.Command("wg", "show", "wg0", "dump").Output()
	if err != nil {
		return result
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			continue
		}
		handshake, _ := strconv.ParseInt(fields[4], 10, 64)
		received, _ := strconv.ParseInt(fields[5], 10, 64)
		sent, _ := strconv.ParseInt(fields[6], 10, 64)
		result[fields[0]] = runtimePeer{handshake, received, sent}
	}
	return result
}

func (a *app) createClient(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !validName.MatchString(request.Name) {
		writeError(w, http.StatusBadRequest, errors.New("名称只能包含字母、数字、点、横线和下划线，最长 48 个字符"))
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	clients, err := a.clients()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	used := map[int]bool{}
	for _, item := range clients {
		if item.Name == request.Name {
			writeError(w, http.StatusConflict, errors.New("客户端名称已存在"))
			return
		}
		used[addressLastOctet(item.Address)] = true
	}
	for octet := range wireGuardUsedAddresses() {
		used[octet] = true
	}
	octet := 0
	for candidate := 10; candidate <= 253; candidate++ {
		if !used[candidate] {
			octet = candidate
			break
		}
	}
	if octet == 0 {
		writeError(w, http.StatusConflict, errors.New("没有可分配的客户端地址"))
		return
	}
	address := fmt.Sprintf("10.88.0.%d", octet)
	command := exec.Command(a.addCommand, "--name", request.Name, "--address", address, "--endpoint", a.endpoint)
	if output, err := command.CombinedOutput(); err != nil {
		log.Printf("create client %q failed: %v", request.Name, err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("新增失败: %s", strings.TrimSpace(string(output))))
		return
	}
	writeJSON(w, http.StatusCreated, client{Name: request.Name, Address: address})
}

func wireGuardUsedAddresses() map[int]bool {
	used := map[int]bool{}
	output, err := exec.Command("wg", "show", "wg0", "allowed-ips").Output()
	if err != nil {
		return used
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		for _, allowedIP := range fields[1:] {
			address := strings.TrimSuffix(allowedIP, "/32")
			if strings.HasPrefix(address, "10.88.0.") {
				used[addressLastOctet(address)] = true
			}
		}
	}
	return used
}

func (a *app) downloadConfig(w http.ResponseWriter, r *http.Request, name string) {
	if !validName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	configPath := filepath.Join(a.clientsDir, name, "client.conf")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/x-wireguard-profile")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, name))
	w.Write(contents)
}

func (a *app) deleteClient(w http.ResponseWriter, r *http.Request, name string) {
	if !validName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	command := exec.Command(a.removeCommand, name)
	if output, err := command.CombinedOutput(); err != nil {
		log.Printf("revoke client %q failed: %v", name, err)
		writeError(w, http.StatusNotFound, fmt.Errorf("撤销失败: %s", strings.TrimSpace(string(output))))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) downloadHostConfig(w http.ResponseWriter, r *http.Request) {
	a.downloadFile(w, r, a.hostConfig, "host.conf", "application/x-wireguard-profile")
}

func (a *app) downloadHostBundle(w http.ResponseWriter, r *http.Request) {
	config, err := os.ReadFile(a.hostConfig)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("宿主配置尚未就绪"))
		return
	}
	pkg, err := os.ReadFile(a.hostPackage)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("宿主安装包尚未就绪"))
		return
	}

	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	files := []struct {
		name     string
		contents []byte
		mode     fs.FileMode
	}{
		{"HostScreenRelay.pkg", pkg, 0644},
		{"host.conf", config, 0600},
		{"安装 Host Screen Relay.command", hostInstallCommand, 0755},
	}
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetMode(file.mode)
		entry, createErr := writer.CreateHeader(header)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, createErr)
			return
		}
		if _, writeErr := entry.Write(file.contents); writeErr != nil {
			writeError(w, http.StatusInternalServerError, writeErr)
			return
		}
	}
	if err := writer.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="HostScreenRelay-arm64.zip"`)
	w.Write(output.Bytes())
}

func (a *app) downloadFile(w http.ResponseWriter, r *http.Request, path, name, contentType string) {
	contents, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Write(contents)
}

func configValue(config, key string) string {
	for _, line := range strings.Split(config, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func addressLastOctet(address string) int {
	parts := strings.Split(address, ".")
	if len(parts) != 4 {
		return 0
	}
	value, _ := strconv.Atoi(parts[3])
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
