package observer

import (
	"errors"
	"strings"
	"sync"
	"time"

	"lightnode-mac-controller/internal/model"
)

type Options struct {
	APIURL       string
	APISecret    string
	PollInterval time.Duration
	OnViolation  func(model.ObservedConnection)
}

type Observer struct {
	options Options
	client  *connectionAPIClient

	mu                 sync.RWMutex
	active             []model.ObservedConnection
	observerHealth     model.Health
	observerMessage    string
	lastPollAt         time.Time
	lastSuccessfulPoll time.Time
	reportedViolations map[string]struct{}
}

func New(options Options) (*Observer, error) {
	if strings.TrimSpace(options.APISecret) == "" {
		return nil, errors.New("connection API secret is required")
	}
	if options.PollInterval < time.Second {
		options.PollInterval = time.Second
	}
	client, err := newConnectionAPIClient(options.APIURL, options.APISecret)
	if err != nil {
		return nil, err
	}
	return &Observer{
		options:            options,
		client:             client,
		observerHealth:     model.HealthUnknown,
		observerMessage:    "等待首次连接采集",
		reportedViolations: make(map[string]struct{}),
	}, nil
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

func (e *Observer) Snapshot() model.ConnectionObservation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	violations := 0
	for _, connection := range e.active {
		if !connection.Compliant {
			violations++
		}
	}
	return model.ConnectionObservation{
		Health: e.observerHealth, Message: e.observerMessage,
		LastPollAt: e.lastPollAt, LastSuccessfulPoll: e.lastSuccessfulPoll,
		ActiveConnections: append([]model.ObservedConnection(nil), e.active...),
		ViolationCount:    violations,
	}
}
