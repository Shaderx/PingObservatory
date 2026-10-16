package model

import "time"

type Point struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Target struct {
	ID                      string               `json:"id"`
	Revision                uint64               `json:"revision"`
	Label                   string               `json:"label"`
	Host                    string               `json:"host"`
	Location                *Point               `json:"location,omitempty"`
	VerifiedAt              *time.Time           `json:"verified_at,omitempty"`
	VerificationSource      string               `json:"verification_source,omitempty"`
	VerificationMethod      string               `json:"verification_method,omitempty"`
	VerificationFamily      string               `json:"verification_family,omitempty"`
	VerificationDestination string               `json:"verification_destination,omitempty"`
	Provider                string               `json:"provider,omitempty"`
	Category                string               `json:"category,omitempty"`
	Region                  string               `json:"region,omitempty"`
	WorldArea               string               `json:"world_area,omitempty"`
	CountryCode             string               `json:"country_code,omitempty"`
	Role                    string               `json:"role,omitempty"`
	Tags                    []string             `json:"tags,omitempty"`
	Port                    int                  `json:"port,omitempty"`
	Scheme                  string               `json:"scheme,omitempty"`
	Path                    string               `json:"path,omitempty"`
	Measurement             ProbeSettings        `json:"measurement"`
	SourceURL               string               `json:"source_url,omitempty"`
	SourceReviewedAt        string               `json:"source_reviewed_at,omitempty"`
	Interpretation          string               `json:"interpretation,omitempty"`
	LocationEvidence        string               `json:"location_evidence,omitempty"`
	LocationNote            string               `json:"location_note,omitempty"`
	Admission               map[string]Admission `json:"admission,omitempty"`
}

type ProbeSettings struct {
	Method              string   `json:"method"`
	Families            []string `json:"families"`
	IntervalSeconds     int      `json:"interval_seconds"`
	PacketsPerCycle     int      `json:"packets_per_cycle"`
	PacketSpacingMS     int      `json:"packet_spacing_ms"`
	TimeoutMS           int      `json:"timeout_ms"`
	PayloadBytes        int      `json:"payload_bytes"`
	HTTPMethod          string   `json:"http_method,omitempty"`
	ExpectedStatusCodes []int    `json:"expected_status_codes,omitempty"`
	DNSQueryName        string   `json:"dns_query_name,omitempty"`
	DNSQueryType        string   `json:"dns_query_type,omitempty"`
	DNSExpectedAnswers  []string `json:"dns_expected_answers,omitempty"`
	ProviderIPv6Support *bool    `json:"provider_ipv6_support,omitempty"`
}

type Verification struct {
	Scope       string    `json:"scope,omitempty"`
	Method      string    `json:"method"`
	Family      string    `json:"family"`
	Destination string    `json:"destination,omitempty"`
	Hostname    string    `json:"hostname,omitempty"`
	Port        int       `json:"port,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
	SourceURL   string    `json:"source_url"`
}

type Admission struct {
	State    string        `json:"state"`
	Reason   string        `json:"reason,omitempty"`
	Evidence *Verification `json:"evidence,omitempty"`
}

type Measurement struct {
	DestinationIP   string             `json:"destination_ip,omitempty"`
	TargetID        string             `json:"target_id"`
	TargetRevision  uint64             `json:"target_revision"`
	At              time.Time          `json:"at"`
	Status          string             `json:"status"`
	LatencyMS       *float64           `json:"latency_ms,omitempty"`
	Detail          string             `json:"detail,omitempty"`
	Method          string             `json:"method"`
	Family          string             `json:"family"`
	BatchStartedAt  time.Time          `json:"batch_started_at"`
	BatchEndedAt    time.Time          `json:"batch_ended_at"`
	Attempted       int                `json:"attempted"`
	Sent            int                `json:"sent"`
	UnobservedSent  int                `json:"unobserved_sent"`
	Received        int                `json:"received"`
	UnableToMeasure int                `json:"unable_to_measure"`
	HasObservation  bool               `json:"has_observation"`
	Samples         []RTTSample        `json:"samples"`
	FailureStage    string             `json:"failure_stage,omitempty"`
	HTTPStatus      int                `json:"http_status,omitempty"`
	Timings         map[string]float64 `json:"timings,omitempty"`
	POPHint         string             `json:"pop_hint,omitempty"`
	Discarded       bool               `json:"discarded,omitempty"`
}

const DestinationHistoryLimit = 64

// DestinationState is local observation metadata, never curator provenance.
// Changes are informational; DNS/CDN rotation does not imply a health failure.
type DestinationState struct {
	BaselineIP         string              `json:"baseline_ip"`
	BaselineObservedAt time.Time           `json:"baseline_observed_at"`
	LastIP             string              `json:"last_ip"`
	LastObservedAt     time.Time           `json:"last_observed_at"`
	ChangeCount        uint64              `json:"change_count"`
	HistoryLimit       int                 `json:"history_limit"`
	History            []DestinationChange `json:"history"`
}

type DestinationChange struct {
	ObservedAt time.Time `json:"observed_at"`
	OldIP      string    `json:"old_ip"`
	NewIP      string    `json:"new_ip"`
}

type RTTSample struct {
	At        time.Time `json:"at"`
	LatencyMS float64   `json:"latency_ms"`
	Sequence  int       `json:"sequence"`
}

const (
	StatusOK          = "ok"
	StatusTimeout     = "timeout"
	StatusUnreachable = "unreachable"
	StatusDNS         = "dns_error"
	StatusPermission  = "permission_denied"
	StatusUnsupported = "unsupported"
	StatusError       = "error"
	StatusSuspended   = "suspended"
	StatusUnavailable = "unavailable"
	StatusDegraded    = "degraded"
	MethodICMP        = "icmp"
	MethodUDP         = "udp_echo"
	MethodTCP         = "tcp_connect"
	MethodHTTPS       = "https"
	MethodDNS         = "dns"
	FamilyIPv4        = "ipv4"
	FamilyIPv6        = "ipv6"
)

type Bucket struct {
	Minute           time.Time    `json:"minute"`
	Status           string       `json:"status"`
	Measurement      *Measurement `json:"measurement,omitempty"`
	End              time.Time    `json:"end"`
	Sent             int          `json:"sent"`
	UnobservedSent   int          `json:"unobserved_sent"`
	Received         int          `json:"received"`
	Attempted        int          `json:"attempted"`
	UnableToMeasure  int          `json:"unable_to_measure"`
	MedianMS         *float64     `json:"median_ms"`
	MinMS            *float64     `json:"min_ms"`
	MaxMS            *float64     `json:"max_ms"`
	P95MS            *float64     `json:"p95_ms"`
	StddevMS         *float64     `json:"stddev_ms"`
	LossPct          *float64     `json:"loss_pct"`
	FailurePct       *float64     `json:"failure_pct"`
	RetentionExpired bool         `json:"retention_expired"`
}

type Observation struct {
	DestinationIP           string             `json:"destination_ip,omitempty"`
	Destination             *DestinationState  `json:"destination,omitempty"`
	Method                  string             `json:"method"`
	Family                  string             `json:"family"`
	Status                  string             `json:"status"`
	ObservedAt              *time.Time         `json:"observed_at"`
	BatchStartedAt          *time.Time         `json:"batch_started_at"`
	BatchEndedAt            *time.Time         `json:"batch_ended_at"`
	Sent                    int                `json:"sent"`
	UnobservedSent          int                `json:"unobserved_sent"`
	Received                int                `json:"received"`
	Attempted               int                `json:"attempted"`
	UnableToMeasure         int                `json:"unable_to_measure"`
	MedianMS                *float64           `json:"median_ms"`
	MinMS                   *float64           `json:"min_ms"`
	MaxMS                   *float64           `json:"max_ms"`
	Rolling1hSent           int                `json:"rolling_1h_sent"`
	Rolling1hUnobservedSent int                `json:"rolling_1h_unobserved_sent"`
	Rolling1hReceived       int                `json:"rolling_1h_received"`
	LossPct                 *float64           `json:"loss_pct"`
	FailurePct              *float64           `json:"failure_pct"`
	P95MS                   *float64           `json:"p95_ms"`
	StddevMS                *float64           `json:"stddev_ms"`
	RollingAsOf             *time.Time         `json:"rolling_as_of"`
	RollingWindowEndedAt    *time.Time         `json:"rolling_window_ended_at"`
	LastSuccessAt           *time.Time         `json:"last_success_at"`
	BaselineMS              *float64           `json:"baseline_ms"`
	BaselineAsOf            *time.Time         `json:"baseline_as_of"`
	BaselineSampleCount     int                `json:"baseline_sample_count"`
	Detail                  string             `json:"detail"`
	FailureStage            string             `json:"failure_stage,omitempty"`
	HTTPStatus              int                `json:"http_status,omitempty"`
	Timings                 map[string]float64 `json:"timings,omitempty"`
	POPHint                 string             `json:"pop_hint,omitempty"`
}

type SeriesStats struct {
	TargetID       string      `json:"target_id"`
	TargetRevision uint64      `json:"target_revision"`
	Observation    Observation `json:"observation"`
}

type History struct {
	Destination    *DestinationState `json:"destination,omitempty"`
	TargetID       string            `json:"target_id"`
	TargetRevision uint64            `json:"target_revision"`
	Method         string            `json:"method"`
	Family         string            `json:"family"`
	Window         string            `json:"window"`
	StepSeconds    int               `json:"step_seconds"`
	Start          time.Time         `json:"start"`
	End            time.Time         `json:"end"`
	AsOf           time.Time         `json:"as_of"`
	RetentionDays  int               `json:"retention_days"`
	RetainedFrom   time.Time         `json:"retained_from"`
	Buckets        []Bucket          `json:"buckets"`
}

func Settings(t Target) ProbeSettings {
	p := t.Measurement
	if p.Method == "" {
		p.Method = MethodICMP
	}
	if len(p.Families) == 0 {
		p.Families = []string{FamilyIPv4}
	}
	if p.IntervalSeconds == 0 {
		p.IntervalSeconds = 60
	}
	if p.PacketsPerCycle == 0 {
		p.PacketsPerCycle = 1
	}
	if p.TimeoutMS == 0 {
		p.TimeoutMS = 3000
	}
	if p.PayloadBytes == 0 {
		p.PayloadBytes = 32
	}
	return p
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
