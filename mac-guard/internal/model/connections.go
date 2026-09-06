package model

import "time"

type ObservedConnection struct {
	ID               string    `json:"id"`
	ProcessPath      string    `json:"processPath"`
	Host             string    `json:"host,omitempty"`
	DestinationIP    string    `json:"destinationIp,omitempty"`
	DestinationPort  string    `json:"destinationPort"`
	Network          string    `json:"network"`
	ObservedOutbound string    `json:"observedOutbound,omitempty"`
	Compliant        bool      `json:"compliant"`
	Upload           int64     `json:"upload"`
	Download         int64     `json:"download"`
	StartedAt        time.Time `json:"startedAt"`
}

type ConnectionObservation struct {
	Health             Health               `json:"health"`
	Message            string               `json:"message"`
	LastPollAt         time.Time            `json:"lastPollAt,omitempty"`
	LastSuccessfulPoll time.Time            `json:"lastSuccessfulPollAt,omitempty"`
	ActiveConnections  []ObservedConnection `json:"activeConnections"`
	ViolationCount     int                  `json:"violationCount"`
}
