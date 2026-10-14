package model

import "time"

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Target struct {
	ID                      string     `json:"id"`
	Revision                uint64     `json:"revision"`
	Label                   string     `json:"label"`
	Host                    string     `json:"host"`
	Location                *Point     `json:"location,omitempty"`
	VerifiedAt              *time.Time `json:"verified_at,omitempty"`
	VerificationSource      string     `json:"verification_source,omitempty"`
	VerificationMethod      string     `json:"verification_method,omitempty"`
	VerificationFamily      string     `json:"verification_family,omitempty"`
	VerificationDestination string     `json:"verification_destination,omitempty"`
}

type Measurement struct {
	TargetID       string    `json:"target_id"`
	TargetRevision uint64    `json:"target_revision"`
	At             time.Time `json:"at"`
	Status         string    `json:"status"`
	LatencyMS      *float64  `json:"latency_ms,omitempty"`
	Detail         string    `json:"detail,omitempty"`
}

const (
	StatusOK          = "ok"
	StatusTimeout     = "timeout"
	StatusUnreachable = "unreachable"
	StatusDNS         = "dns_error"
	StatusPermission  = "permission_denied"
	StatusUnsupported = "unsupported"
	StatusError       = "error"
)

type Bucket struct {
	Minute      time.Time    `json:"minute"`
	Status      string       `json:"status"`
	Measurement *Measurement `json:"measurement,omitempty"`
}

type Catalogue struct {
	SchemaVersion int       `json:"schema_version"`
	Version       uint64    `json:"version"`
	PublishedAt   time.Time `json:"published_at"`
	Targets       []Target  `json:"targets"`
}

type Config struct {
	Origin        *Point   `json:"origin,omitempty"`
	ManualTargets []Target `json:"manual_targets"`
	CatalogueURL  string   `json:"catalogue_url"`
	RetentionDays int      `json:"retention_days"`
}
