package gateway

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	RuntimeDir      string
	PublicIP        string
	EgressInterface string
	WireGuardPort   int
	RealityPort     int
	RealityListen   int
	RealityTarget   string
	RealityName     string
	DefaultClients  []string
	DryRun          bool
}

func DefaultConfig() Config {
	return Config{
		RuntimeDir:     "/run/vpn-gateway",
		WireGuardPort:  443,
		RealityPort:    443,
		RealityListen:  443,
		RealityTarget:  "www.cloudflare.com:443",
		RealityName:    "www.cloudflare.com",
		DefaultClients: []string{"mac", "iphone", "android"},
	}
}

type Manager struct {
	mu           sync.RWMutex
	store        *Store
	runner       Runner
	cfg          Config
	state        *State
	activityMu   sync.Mutex
	peerActivity map[string]peerActivity
}

type peerActivity struct {
	receiveBytes  int64
	transmitBytes int64
	lastChange    time.Time
}

func NewManager(store *Store, runner Runner, cfg Config) *Manager {
	return &Manager{store: store, runner: runner, cfg: cfg, peerActivity: make(map[string]peerActivity)}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load()
	if err != nil {
		return err
	}
	m.state = state
	return nil
}

func (m *Manager) Bootstrap(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load()
	if err == nil {
		m.state = state
		changed := false
		if m.cfg.PublicIP != "" && m.state.PublicIP != m.cfg.PublicIP {
			m.state.PublicIP = m.cfg.PublicIP
			changed = true
		}
		if m.cfg.EgressInterface != "" && m.state.EgressIF != m.cfg.EgressInterface {
			m.state.EgressIF = m.cfg.EgressInterface
			changed = true
		}
		if m.state.WireGuard.Port != m.cfg.WireGuardPort {
			m.state.WireGuard.Port = m.cfg.WireGuardPort
			changed = true
		}
		if m.state.Reality.Port != m.cfg.RealityPort || m.state.Reality.Target != m.cfg.RealityTarget || m.state.Reality.ServerName != m.cfg.RealityName {
			m.state.Reality.Port = m.cfg.RealityPort
			m.state.Reality.Target = m.cfg.RealityTarget
			m.state.Reality.ServerName = m.cfg.RealityName
			changed = true
		}
		if changed {
			m.state.UpdatedAt = time.Now().UTC()
			if err := m.store.Save(m.state); err != nil {
				return err
			}
		}
		return m.applyLocked(ctx, false)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if m.cfg.PublicIP == "" {
		return errors.New("PUBLIC_IP is required on first boot")
	}
	if m.cfg.EgressInterface == "" {
		return errors.New("EGRESS_INTERFACE is required on first boot")
	}
	wgPrivate, wgPublic, err := m.wireGuardKeypair(ctx)
	if err != nil {
		return err
	}
	realityPrivate, realityPublic, err := m.realityKeypair(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	state = &State{
		Version:  StateVersion,
		PublicIP: m.cfg.PublicIP,
		EgressIF: m.cfg.EgressInterface,
		WireGuard: WireGuard{
			PrivateKey: wgPrivate,
			PublicKey:  wgPublic,
			Port:       m.cfg.WireGuardPort,
			Network:    "10.77.0.0/24",
			IPv6Prefix: "fd77:77:77::/64",
			DNS:        "10.77.0.1",
			MTU:        1280,
		},
		Reality: Reality{
			PrivateKey: realityPrivate,
			PublicKey:  realityPublic,
			Port:       m.cfg.RealityPort,
			ServerName: m.cfg.RealityName,
			Target:     m.cfg.RealityTarget,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.state = state
	for _, name := range m.cfg.DefaultClients {
		if _, err := m.addClientLocked(ctx, name); err != nil {
			return err
		}
	}
	if err := m.store.Save(m.state); err != nil {
		return err
	}
	return m.applyLocked(ctx, false)
}

func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyLocked(ctx, true)
}

func (m *Manager) applyLocked(ctx context.Context, reloadXray bool) error {
	if m.state == nil {
		return errors.New("gateway state is not loaded")
	}
	if err := m.renderLocked(); err != nil {
		return err
	}
	if m.cfg.DryRun {
		return nil
	}
	_, _ = m.runner.Run(ctx, "wg-quick", "down", filepath.Join(m.cfg.RuntimeDir, "wg0.conf"))
	if _, err := m.runner.Run(ctx, "wg-quick", "up", filepath.Join(m.cfg.RuntimeDir, "wg0.conf")); err != nil {
		return err
	}
	if reloadXray {
		if _, err := m.runner.Run(ctx, "supervisorctl", "restart", "xray"); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) State() *State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state == nil {
		return nil
	}
	b, _ := json.Marshal(m.state)
	var out State
	_ = json.Unmarshal(b, &out)
	return &out
}

func (m *Manager) ListClients() []PublicClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PublicClient, 0, len(m.state.Clients))
	for _, client := range m.state.Clients {
		out = append(out, client.Public())
	}
	return out
}

func (m *Manager) Client(id string) (Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, client := range m.state.Clients {
		if client.ID == id {
			return client, true
		}
	}
	return Client{}, false
}

func (m *Manager) AddClient(ctx context.Context, name string) (PublicClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 48 {
		return PublicClient{}, errors.New("client name must contain 1-48 characters")
	}
	for _, client := range m.state.Clients {
		if strings.EqualFold(client.Name, name) {
			return PublicClient{}, errors.New("client name already exists")
		}
	}
	before := append([]Client(nil), m.state.Clients...)
	client, err := m.addClientLocked(ctx, name)
	if err != nil {
		return PublicClient{}, err
	}
	m.state.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(m.state); err != nil {
		m.state.Clients = before
		return PublicClient{}, err
	}
	if err := m.applyLocked(ctx, true); err != nil {
		m.state.Clients = before
		_ = m.store.Save(m.state)
		_ = m.applyLocked(ctx, true)
		return PublicClient{}, err
	}
	return client.Public(), nil
}

func (m *Manager) addClientLocked(ctx context.Context, name string) (Client, error) {
	if len(m.state.Clients) >= 253 {
		return Client{}, errors.New("WireGuard address pool is full")
	}
	used := map[int]bool{1: true}
	for _, client := range m.state.Clients {
		addr, err := netip.ParseAddr(client.IPv4)
		if err == nil {
			used[int(addr.As4()[3])] = true
		}
	}
	octet := 2
	for used[octet] {
		octet++
	}
	wgPrivate, wgPublic, err := m.wireGuardKeypair(ctx)
	if err != nil {
		return Client{}, err
	}
	id, err := randomHex(8)
	if err != nil {
		return Client{}, err
	}
	shortID, err := randomHex(8)
	if err != nil {
		return Client{}, err
	}
	uuid, err := randomUUID()
	if err != nil {
		return Client{}, err
	}
	client := Client{
		ID:               id,
		Name:             name,
		CreatedAt:        time.Now().UTC(),
		WireGuardPrivate: wgPrivate,
		WireGuardPublic:  wgPublic,
		IPv4:             "10.77.0." + strconv.Itoa(octet),
		IPv6:             "fd77:77:77::" + strconv.Itoa(octet),
		VLESSUUID:        uuid,
		RealityShortID:   shortID,
	}
	m.state.Clients = append(m.state.Clients, client)
	return client, nil
}

func (m *Manager) DeleteClient(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.state.Clients) == 1 {
		return errors.New("at least one client must remain")
	}
	index := -1
	for i, client := range m.state.Clients {
		if client.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return os.ErrNotExist
	}
	before := append([]Client(nil), m.state.Clients...)
	m.state.Clients = append(m.state.Clients[:index], m.state.Clients[index+1:]...)
	m.state.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(m.state); err != nil {
		m.state.Clients = before
		return err
	}
	if err := m.applyLocked(ctx, true); err != nil {
		m.state.Clients = before
		_ = m.store.Save(m.state)
		_ = m.applyLocked(ctx, true)
		return err
	}
	return nil
}

func (m *Manager) wireGuardKeypair(ctx context.Context) (string, string, error) {
	_ = ctx
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(privateKey.Bytes()), base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

func (m *Manager) realityKeypair(ctx context.Context) (string, string, error) {
	_ = ctx
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(privateKey.Bytes()), base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

func randomHex(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
