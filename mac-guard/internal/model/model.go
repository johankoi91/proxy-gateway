package model

import "time"

type Health string

const (
	HealthHealthy  Health = "healthy"
	HealthDegraded Health = "degraded"
	HealthFailed   Health = "failed"
	HealthUnknown  Health = "unknown"
)

type ControllerState string

const (
	StateBootLocked         ControllerState = "boot_locked"
	StateHealthy            ControllerState = "healthy"
	StateDegradedLocked     ControllerState = "degraded_locked"
	StateInconsistentLocked ControllerState = "inconsistent_locked"
	StateUnprotected        ControllerState = "unprotected"
	StateRecovering         ControllerState = "recovering"
	StateMaintenance        ControllerState = "maintenance"
)

type LifecycleAction struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Message    string     `json:"message"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type Component struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Health        Health    `json:"health"`
	Required      bool      `json:"required"`
	Summary       string    `json:"summary"`
	Detail        string    `json:"detail"`
	LastCheckedAt time.Time `json:"lastCheckedAt"`
	LatencyMS     int64     `json:"latencyMs"`
}

type Stage struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Health  Health `json:"health"`
	Summary string `json:"summary"`
}

type TestResult struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Health     Health    `json:"health"`
	Expected   string    `json:"expected"`
	Actual     string    `json:"actual"`
	Detail     string    `json:"detail"`
	DurationMS int64     `json:"durationMs"`
	RunAt      time.Time `json:"runAt"`
}

type Alert struct {
	ID        string    `json:"id"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Component string    `json:"component"`
	RaisedAt  time.Time `json:"raisedAt"`
}

type Event struct {
	ID        int64     `json:"id"`
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
}

type RuntimeConfiguration struct {
	VPSAddress                   string                          `json:"vpsAddress"`
	VPSPort                      int                             `json:"vpsPort"`
	ExpectedEgress               string                          `json:"expectedEgress"`
	TunnelAddress                string                          `json:"tunnelAddress"`
	TunnelInterfacePrefix        string                          `json:"tunnelInterfacePrefix"`
	ExpectedDNSServers           []string                        `json:"expectedDnsServers"`
	PFAnchor                     string                          `json:"pfAnchor"`
	PFAnchorFile                 string                          `json:"pfAnchorFile"`
	PFMainConfigFile             string                          `json:"pfMainConfigFile"`
	ControllerService            string                          `json:"controllerService"`
	ProviderService              string                          `json:"providerService"`
	ListenAddress                string                          `json:"listenAddress"`
	AuthenticationEnabled        bool                            `json:"authenticationEnabled"`
	CheckIntervalSeconds         int                             `json:"checkIntervalSeconds"`
	ConflictCheckIntervalSeconds int                             `json:"conflictCheckIntervalSeconds"`
	ObserverURL                  string                          `json:"observerUrl"`
	ObserverIntervalSeconds      int                             `json:"observerIntervalSeconds"`
	Components                   []RuntimeComponentConfiguration `json:"components"`
}

type RuntimeComponentConfiguration struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Owner        string           `json:"owner"`
	LaunchdLabel string           `json:"launchdLabel,omitempty"`
	Executable   string           `json:"executable,omitempty"`
	Arguments    []string         `json:"arguments,omitempty"`
	Settings     []RuntimeSetting `json:"settings"`
}

type RuntimeSetting struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Snapshot struct {
	Version        string                `json:"version"`
	State          ControllerState       `json:"state"`
	StateSince     time.Time             `json:"stateSince"`
	Protected      bool                  `json:"protected"`
	Connected      bool                  `json:"connected"`
	Provider       string                `json:"provider"`
	ExpectedEgress string                `json:"expectedEgress"`
	ObservedEgress string                `json:"observedEgress"`
	PhysicalIF     string                `json:"physicalInterface"`
	TunnelIF       string                `json:"tunnelInterface"`
	LastCheckAt    time.Time             `json:"lastCheckAt"`
	NextCheckAt    time.Time             `json:"nextCheckAt"`
	Checking       bool                  `json:"checking"`
	Recovering     bool                  `json:"recovering"`
	Components     []Component           `json:"components"`
	Stages         []Stage               `json:"stages"`
	Tests          []TestResult          `json:"tests"`
	Alerts         []Alert               `json:"alerts"`
	Events         []Event               `json:"events"`
	Connections    ConnectionObservation `json:"connections"`
	RuntimeConfig  RuntimeConfiguration  `json:"runtimeConfig"`
	Action         *LifecycleAction      `json:"action,omitempty"`
}
