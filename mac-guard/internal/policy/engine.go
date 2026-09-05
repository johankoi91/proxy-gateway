package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lightnode-mac-guard/internal/model"
)

type Options struct {
	PolicyFile     string
	ClashAPIURL    string
	ClashAPISecret string
	PollInterval   time.Duration
	OnViolation    func(model.PolicyConnection)
}

type Document struct {
	Version               int                 `json:"version"`
	Mode                  model.PolicyMode    `json:"mode"`
	Profile               model.PolicyProfile `json:"profile"`
	AIProcessNames        []string            `json:"aiProcessNames"`
	AIProcessPathPrefixes []string            `json:"aiProcessPathPrefixes"`
	AIDomainSuffixes      []string            `json:"aiDomainSuffixes"`
}

type Engine struct {
	options Options
	doc     Document
	client  *clashClient

	mu                 sync.RWMutex
	active             []model.PolicyConnection
	observerHealth     model.Health
	observerMessage    string
	lastPollAt         time.Time
	lastSuccessfulPoll time.Time
	reportedViolations map[string]struct{}
}

type classification struct {
	Disposition model.PolicyRoute
	Reason      string
	IsAIProcess bool
}

func New(options Options) (*Engine, error) {
	if strings.TrimSpace(options.ClashAPISecret) == "" {
		return nil, errors.New("V2 Clash API secret is required")
	}
	if options.PollInterval < time.Second {
		options.PollInterval = time.Second
	}
	document, err := loadDocument(options.PolicyFile)
	if err != nil {
		return nil, err
	}
	client, err := newClashClient(options.ClashAPIURL, options.ClashAPISecret)
	if err != nil {
		return nil, err
	}
	return &Engine{
		options:            options,
		doc:                document,
		client:             client,
		observerHealth:     model.HealthUnknown,
		observerMessage:    "等待首次连接采集",
		reportedViolations: make(map[string]struct{}),
	}, nil
}

func loadDocument(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("read V2 policy: %w", err)
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, fmt.Errorf("decode V2 policy: %w", err)
	}
	if document.Version != 1 {
		return Document{}, fmt.Errorf("unsupported V2 policy version %d", document.Version)
	}
	if document.Mode != model.PolicyModeEnforce {
		return Document{}, fmt.Errorf("unsupported V2 policy mode %q", document.Mode)
	}
	if document.Profile != model.PolicyProfileGlobalJapan {
		return Document{}, fmt.Errorf("unsupported V2 policy profile %q", document.Profile)
	}
	normalizeSuffixes(document.AIDomainSuffixes)
	if len(document.AIProcessNames) == 0 || len(document.AIProcessPathPrefixes) == 0 || len(document.AIDomainSuffixes) == 0 {
		return Document{}, errors.New("V2 policy requires AI process and domain rules")
	}
	return document, nil
}

func normalizeSuffixes(values []string) {
	for index := range values {
		values[index] = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(values[index])), ".")
	}
}

func (e *Engine) classify(connection rawConnection) classification {
	processPath := sanitizeProcessPath(connection.Metadata.ProcessPath)
	host := normalizeHost(connection.Metadata.Host)
	isAIProcess := e.matchesAIProcess(processPath)

	if isAIProcess {
		return classification{Disposition: model.PolicyRouteAIFixed, Reason: "AI 进程强制固定东京", IsAIProcess: true}
	}
	if matchesDomain(host, e.doc.AIDomainSuffixes) {
		return classification{Disposition: model.PolicyRouteAIFixed, Reason: "AI 域名强制固定东京"}
	}
	return classification{Disposition: model.PolicyRouteJapan, Reason: "整机流量固定日本"}
}

func (e *Engine) matchesAIProcess(processPath string) bool {
	for _, prefix := range e.doc.AIProcessPathPrefixes {
		if strings.HasPrefix(processPath, prefix) {
			return true
		}
	}
	name := filepath.Base(processPath)
	for _, expected := range e.doc.AIProcessNames {
		if name == expected || strings.HasPrefix(name, expected+" ") {
			return true
		}
	}
	return false
}

func matchesDomain(host string, suffixes []string) bool {
	if host == "" || net.ParseIP(host) != nil {
		return false
	}
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func sanitizeProcessPath(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/") {
		if suffix := strings.LastIndex(value, " ("); suffix > 0 && strings.HasSuffix(value, ")") {
			value = value[:suffix]
		}
	}
	if strings.HasPrefix(value, "/Users/") {
		parts := strings.SplitN(strings.TrimPrefix(value, "/Users/"), "/", 2)
		if len(parts) == 2 {
			return "/Users/*/" + parts[1]
		}
	}
	return value
}

func (e *Engine) Snapshot() model.PolicyV2Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	routeCounts := make(map[model.PolicyRoute]int)
	violations := 0
	for _, connection := range e.active {
		routeCounts[connection.Disposition]++
		if !connection.Compliant {
			violations++
		}
	}
	routes := make([]model.PolicyRouteSummary, 0, len(routeCounts))
	for _, route := range []model.PolicyRoute{model.PolicyRouteAIFixed, model.PolicyRouteJapan} {
		if count := routeCounts[route]; count > 0 {
			routes = append(routes, model.PolicyRouteSummary{Route: route, Count: count})
		}
	}
	return model.PolicyV2Snapshot{
		Enabled: true, Mode: e.doc.Mode, Profile: e.doc.Profile,
		ObserverHealth: e.observerHealth, ObserverMessage: e.observerMessage,
		LastPollAt: e.lastPollAt, LastSuccessfulPoll: e.lastSuccessfulPoll,
		ActiveConnections: append([]model.PolicyConnection(nil), e.active...),
		RouteSummary:      routes, ViolationCount: violations,
	}
}
