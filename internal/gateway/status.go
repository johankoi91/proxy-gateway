package gateway

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) Status(ctx context.Context) Status {
	state := m.State()
	status := Status{
		PublicIP: state.PublicIP, EgressIF: state.EgressIF,
		WireGuardPort: state.WireGuard.Port, RealityPort: state.Reality.Port,
		Peers: make([]PeerStatus, 0, len(state.Clients)),
	}
	if m.cfg.DryRun {
		status.WireGuardUp, status.XrayUp, status.DNSUp, status.IPv4Forward = true, true, true, true
		for _, client := range state.Clients {
			status.Peers = append(status.Peers, PeerStatus{ID: client.ID, Name: client.Name})
		}
		return status
	}
	status.WireGuardUp = commandOK(ctx, m.runner, "wg", "show", "wg0")
	status.XrayUp = commandOK(ctx, m.runner, "supervisorctl", "status", "xray")
	status.DNSUp = commandOK(ctx, m.runner, "supervisorctl", "status", "dnsmasq")
	if out, err := m.runner.Run(ctx, "sysctl", "-n", "net.ipv4.ip_forward"); err == nil {
		status.IPv4Forward = strings.TrimSpace(string(out)) == "1"
	}
	byKey := make(map[string]Client, len(state.Clients))
	for _, client := range state.Clients {
		byKey[client.WireGuardPublic] = client
	}
	if out, err := m.runner.Run(ctx, "wg", "show", "wg0", "dump"); err == nil {
		now := time.Now()
		for index, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if index == 0 || line == "" {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 8 {
				continue
			}
			client, ok := byKey[fields[0]]
			if !ok {
				continue
			}
			handshake, _ := strconv.ParseInt(fields[4], 10, 64)
			rx, _ := strconv.ParseInt(fields[5], 10, 64)
			tx, _ := strconv.ParseInt(fields[6], 10, 64)
			activity := m.recordPeerActivity(fields[0], rx, tx, now)
			recentHandshake := handshake > 0 && now.Unix()-handshake < 180
			status.Peers = append(status.Peers, PeerStatus{
				ID: client.ID, Name: client.Name, Endpoint: normalizeNone(fields[2]),
				LatestHandshake: handshake, ReceiveBytes: rx, TransmitBytes: tx,
				Connected: recentHandshake || (!activity.IsZero() && now.Sub(activity) < 90*time.Second),
			})
		}
	}
	return status
}

func (m *Manager) recordPeerActivity(key string, receiveBytes, transmitBytes int64, now time.Time) time.Time {
	m.activityMu.Lock()
	defer m.activityMu.Unlock()
	previous, ok := m.peerActivity[key]
	if !ok {
		m.peerActivity[key] = peerActivity{receiveBytes: receiveBytes, transmitBytes: transmitBytes}
		return time.Time{}
	}
	if previous.receiveBytes != receiveBytes || previous.transmitBytes != transmitBytes {
		previous.lastChange = now
		previous.receiveBytes = receiveBytes
		previous.transmitBytes = transmitBytes
		m.peerActivity[key] = previous
	}
	return previous.lastChange
}

func commandOK(ctx context.Context, runner Runner, name string, args ...string) bool {
	_, err := runner.Run(ctx, name, args...)
	return err == nil
}

func normalizeNone(value string) string {
	if value == "(none)" {
		return ""
	}
	return value
}
