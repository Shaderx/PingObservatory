package catalogue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

type receiptBatch struct {
	Method     string    `json:"method"`
	Family     string    `json:"family"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"batch_started_at"`
	ObservedAt time.Time `json:"observed_at"`
	Sent       int       `json:"batch_sent"`
	Received   int       `json:"batch_received"`
}

type receiptRound struct {
	Destination string    `json:"destination"`
	StartedAt   time.Time `json:"started_at"`
	ObservedAt  time.Time `json:"observed_at"`
	Sent        int       `json:"sent"`
	Received    int       `json:"received"`
}

type publishedReceipt struct {
	TargetID            string         `json:"target_id"`
	Method              string         `json:"method"`
	Family              string         `json:"family"`
	Destination         string         `json:"destination"`
	ObservedAt          time.Time      `json:"observed_at"`
	SourceURL           string         `json:"source_url,omitempty"`
	DefinitionSourceURL string         `json:"definition_source_url,omitempty"`
	Basis               string         `json:"basis"`
	ConfiguredLiteral   string         `json:"configured_literal,omitempty"`
	SourceObservation   *receiptBatch  `json:"source_observation,omitempty"`
	ValidationRounds    []receiptRound `json:"validation_rounds,omitempty"`
}

// Verify the released data artifacts together: parsing one valid manifest is
// insufficient if its evidence object is missing, mismatched or silently newer.
func TestPublishedV2MatchesHistoricalReceipts(t *testing.T) {
	read := func(parts ...string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "catalogue"}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	c, err := Decode(read("v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		SchemaVersion    int                `json:"schema_version"`
		CatalogueVersion uint64             `json:"catalogue_version"`
		ExportedAt       time.Time          `json:"exported_at"`
		Scope            string             `json:"scope"`
		Records          []publishedReceipt `json:"records"`
	}
	if err := decodeStrict(read("evidence", "v2.json"), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || document.CatalogueVersion != c.Version || document.ExportedAt.After(c.PublishedAt) || document.Scope == "" {
		t.Fatal("inconsistent evidence document")
	}
	receipts := map[string]publishedReceipt{}
	derived, explicit := 0, 0
	for _, r := range document.Records {
		if _, exists := receipts[r.TargetID]; exists {
			t.Fatal("duplicate evidence identity", r.TargetID)
		}
		if r.Method != model.MethodICMP || r.Family != model.FamilyIPv4 || r.ObservedAt.UTC().Format("2006-01-02") != "2026-10-09" {
			t.Fatal("historical method/family/date changed", r.TargetID)
		}
		switch r.Basis {
		case "literal_definition_and_contemporaneous_success":
			b := r.SourceObservation
			if r.ConfiguredLiteral != r.Destination || b == nil || b.Method != r.Method || b.Family != r.Family || b.Status != "ok" || !b.ObservedAt.Equal(r.ObservedAt) || b.StartedAt.After(b.ObservedAt) || b.Sent != 3 || b.Received != 3 || len(r.ValidationRounds) != 0 {
				t.Fatal("literal receipt does not match its captured batch", r.TargetID)
			}
			if r.SourceURL != "https://lg.pwnation.net/data/v1/generations/g-1791540828176961371/overview.json" {
				t.Fatal("literal receipt is not bound to its contemporaneous snapshot", r.TargetID)
			}
			derived++
		case "saved_numeric_destination_and_success":
			if len(r.ValidationRounds) != 3 || r.SourceObservation != nil || validatePublicHTTPSURL(r.DefinitionSourceURL) != nil {
				t.Fatal("incomplete explicit numeric receipt", r.TargetID)
			}
			for _, round := range r.ValidationRounds {
				if round.Destination != r.Destination || round.Sent != 3 || round.Received != 3 || round.StartedAt.After(round.ObservedAt) || round.ObservedAt.After(r.ObservedAt) {
					t.Fatal("round does not support the explicit receipt", r.TargetID)
				}
			}
			if !r.ValidationRounds[2].ObservedAt.Equal(r.ObservedAt) {
				t.Fatal("receipt timestamp is not the final recorded round", r.TargetID)
			}
			explicit++
		default:
			t.Fatal("unknown evidence basis", r.TargetID)
		}
		receipts[r.TargetID] = r
	}
	verified, pending, slots := 0, 0, 0
	evidenceURL := ""
	semanticTags := map[string]bool{"regional-anchor": true, "cloud-network": true, "icmp": true, "gaming-infrastructure": true, "udp-beacon": true, "cdn-delivery": true, "https-head": true, "diagnostic-reachability": true, "edge-diagnostic": true, "https-get": true, "everyday-service": true, "status-api": true, "fortnite": true, "mudfish-relay": true, "icmp-only": true}
	for _, target := range c.Targets {
		for _, tag := range target.Tags {
			if !semanticTags[tag] {
				t.Fatal("source workflow tag is not an endpoint property", target.ID, tag)
			}
		}
		for _, family := range target.Measurement.Families {
			slots++
			a := target.Admission[family]
			if a.State != "verified" {
				pending++
				continue
			}
			r, ok := receipts[target.ID]
			if !ok || a.Evidence == nil {
				t.Fatal("verified family has no receipt", target.ID)
			}
			e := a.Evidence
			if e.Method != r.Method || e.Family != r.Family || e.Destination != r.Destination || target.Host != r.Destination || !e.ObservedAt.Equal(r.ObservedAt) || !strings.HasPrefix(e.SourceURL, "https://raw.githubusercontent.com/Shaderx/PingObservatory/") || !strings.HasSuffix(e.SourceURL, "/catalogue/evidence/v2.json") {
				t.Fatal("manifest and receipt disagree", target.ID)
			}
			ref := strings.TrimSuffix(strings.TrimPrefix(e.SourceURL, "https://raw.githubusercontent.com/Shaderx/PingObservatory/"), "/catalogue/evidence/v2.json")
			if len(ref) != 40 || strings.Trim(ref, "0123456789abcdef") != "" {
				t.Fatal("evidence URL must pin an immutable Git commit", target.ID)
			}
			if evidenceURL != "" && evidenceURL != e.SourceURL {
				t.Fatal("manifest must pin one consistent evidence object", target.ID)
			}
			evidenceURL = e.SourceURL
			verified++
		}
	}
	if len(c.Targets) != 171 || slots != 196 || verified != 116 || pending != 80 || len(receipts) != verified || derived != 101 || explicit != 15 {
		t.Fatalf("unexpected immutable v2 population: targets=%d slots=%d verified=%d pending=%d receipts=%d derived=%d explicit=%d", len(c.Targets), slots, verified, pending, len(receipts), derived, explicit)
	}
}
