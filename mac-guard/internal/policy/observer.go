package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"lightnode-mac-guard/internal/model"
)

type rawConnection struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
		ProcessPath     string `json:"processPath"`
	} `json:"metadata"`
	Upload   int64     `json:"upload"`
	Download int64     `json:"download"`
	Start    time.Time `json:"start"`
	Chains   []string  `json:"chains"`
	Rule     string    `json:"rule"`
}

type clashConnections struct {
	Connections []rawConnection `json:"connections"`
}

type clashClient struct {
	url    string
	secret string
	client *http.Client
}

func newClashClient(rawURL, secret string) (*clashClient, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Clash API URL: %w", err)
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if parsed.Scheme != "http" || (hostname != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, fmt.Errorf("Clash API must use loopback HTTP, got %q", rawURL)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/connections"
	return &clashClient{
		url:    parsed.String(),
		secret: secret,
		client: &http.Client{
			Timeout:   4 * time.Second,
			Transport: &http.Transport{Proxy: nil},
		},
	}, nil
}

func (c *clashClient) connections(ctx context.Context) ([]rawConnection, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Clash API returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var payload clashConnections
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Clash connections: %w", err)
	}
	return payload.Connections, nil
}

func (e *Engine) Start(ctx context.Context) {
	go func() {
		e.poll(ctx)
		ticker := time.NewTicker(e.options.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.poll(ctx)
			}
		}
	}()
}

func (e *Engine) poll(ctx context.Context) {
	now := time.Now().UTC()
	connections, err := e.client.connections(ctx)
	if err != nil {
		e.mu.Lock()
		e.lastPollAt = now
		e.observerHealth = model.HealthFailed
		e.observerMessage = err.Error()
		e.mu.Unlock()
		return
	}
	e.observe(connections, now)
}

func (e *Engine) observe(connections []rawConnection, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := make([]model.PolicyConnection, 0, len(connections))
	for _, connection := range connections {
		processPath := sanitizeProcessPath(connection.Metadata.ProcessPath)
		host := normalizeHost(connection.Metadata.Host)
		observedOutbound := strings.Join(connection.Chains, " > ")
		active = append(active, model.PolicyConnection{
			ID:               connection.ID,
			ProcessPath:      processPath,
			Host:             host,
			DestinationIP:    connection.Metadata.DestinationIP,
			DestinationPort:  connection.Metadata.DestinationPort,
			Network:          connection.Metadata.Network,
			ObservedOutbound: observedOutbound,
			Compliant:        e.connectionCompliant(observedOutbound),
			Upload:           connection.Upload,
			Download:         connection.Download,
			StartedAt:        connection.Start,
		})
		activeConnection := active[len(active)-1]
		if !activeConnection.Compliant {
			if _, reported := e.reportedViolations[connection.ID]; !reported {
				e.reportedViolations[connection.ID] = struct{}{}
				if e.options.OnViolation != nil {
					go e.options.OnViolation(activeConnection)
				}
			}
		}
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].StartedAt.After(active[j].StartedAt) })
	if len(active) > 200 {
		active = active[:200]
	}
	e.active = active
	e.observerHealth = model.HealthHealthy
	e.observerMessage = fmt.Sprintf("已采集 %d 条活动连接", len(connections))
	e.lastPollAt = now
	e.lastSuccessfulPoll = now
}

func (e *Engine) connectionCompliant(observedOutbound string) bool {
	return strings.Contains(observedOutbound, "tokyo")
}
