package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	Version int                 `json:"version"`
	Mode    model.PolicyMode    `json:"mode"`
	Profile model.PolicyProfile `json:"profile"`
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
	return document, nil
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
	violations := 0
	for _, connection := range e.active {
		if !connection.Compliant {
			violations++
		}
	}
	return model.PolicyV2Snapshot{
		Enabled: true, Mode: e.doc.Mode, Profile: e.doc.Profile,
		ObserverHealth: e.observerHealth, ObserverMessage: e.observerMessage,
		LastPollAt: e.lastPollAt, LastSuccessfulPoll: e.lastSuccessfulPoll,
		ActiveConnections: append([]model.PolicyConnection(nil), e.active...),
		ViolationCount:    violations,
	}
}
