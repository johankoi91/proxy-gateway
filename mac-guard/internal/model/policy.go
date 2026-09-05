package model

import "time"

type PolicyMode string
type PolicyProfile string
type PolicyRoute string

const (
	PolicyModeEnforce PolicyMode = "enforce"
)

const (
	PolicyProfileGlobalJapan PolicyProfile = "global_japan"
)

const (
	PolicyRouteAIFixed PolicyRoute = "ai_fixed"
	PolicyRouteJapan   PolicyRoute = "japan"
)

type PolicyRouteSummary struct {
	Route PolicyRoute `json:"route"`
	Count int         `json:"count"`
}

type PolicyConnection struct {
	ID               string      `json:"id"`
	ProcessPath      string      `json:"processPath"`
	Host             string      `json:"host,omitempty"`
	DestinationIP    string      `json:"destinationIp,omitempty"`
	DestinationPort  string      `json:"destinationPort"`
	Network          string      `json:"network"`
	Disposition      PolicyRoute `json:"disposition"`
	ObservedOutbound string      `json:"observedOutbound,omitempty"`
	Reason           string      `json:"reason"`
	IsAIProcess      bool        `json:"isAiProcess"`
	Compliant        bool        `json:"compliant"`
	Upload           int64       `json:"upload"`
	Download         int64       `json:"download"`
	StartedAt        time.Time   `json:"startedAt"`
}

type PolicyV2Snapshot struct {
	Enabled            bool                 `json:"enabled"`
	Mode               PolicyMode           `json:"mode"`
	Profile            PolicyProfile        `json:"profile"`
	ObserverHealth     Health               `json:"observerHealth"`
	ObserverMessage    string               `json:"observerMessage"`
	LastPollAt         time.Time            `json:"lastPollAt,omitempty"`
	LastSuccessfulPoll time.Time            `json:"lastSuccessfulPollAt,omitempty"`
	ActiveConnections  []PolicyConnection   `json:"activeConnections"`
	RouteSummary       []PolicyRouteSummary `json:"routeSummary"`
	ViolationCount     int                  `json:"violationCount"`
}
