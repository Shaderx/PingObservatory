package catalogue

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublishedV3MatchesHostnameReceipts(t *testing.T) {
	read := func(parts ...string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "catalogue"}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	c, err := Decode(read("v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := Decode(read("v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	type hostnameReceipt struct {
		TargetID            string       `json:"target_id"`
		Scope               string       `json:"scope"`
		Hostname            string       `json:"hostname"`
		Port                int          `json:"port,omitempty"`
		Scheme              string       `json:"scheme,omitempty"`
		Path                string       `json:"path,omitempty"`
		HTTPMethod          string       `json:"http_method,omitempty"`
		Method              string       `json:"method"`
		Family              string       `json:"family"`
		ObservedAt          time.Time    `json:"observed_at"`
		NumericPeerRecorded bool         `json:"numeric_peer_recorded"`
		Basis               string       `json:"basis"`
		SourceURL           string       `json:"source_url"`
		DefinitionSourceURL string       `json:"definition_source_url"`
		SourceObservation   receiptBatch `json:"source_observation"`
	}
	var doc struct {
		SchemaVersion    int               `json:"schema_version"`
		CatalogueVersion uint64            `json:"catalogue_version"`
		ExportedAt       time.Time         `json:"exported_at"`
		Scope            string            `json:"scope"`
		Records          []hostnameReceipt `json:"records"`
	}
	data := read("evidence", "v3.json")
	if err := decodeStrict(data, &doc); err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != 3 || c.Version != 3 || doc.SchemaVersion != 1 || doc.CatalogueVersion != c.Version || doc.ExportedAt.After(c.PublishedAt) || doc.Scope == "" {
		t.Fatal("inconsistent version3 evidence document")
	}
	var raw struct {
		Records []map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	receipts := map[string]hostnameReceipt{}
	for i, r := range doc.Records {
		b := r.SourceObservation
		key := r.TargetID + "/" + r.Family
		if _, exists := receipts[key]; exists {
			t.Fatal("duplicate hostname receipt", key)
		}
		if r.Scope != "hostname" || r.NumericPeerRecorded || !bytes.Equal(raw.Records[i]["numeric_peer_recorded"], []byte("false")) || r.Basis != "configured_hostname_and_contemporaneous_success" || r.ObservedAt.UTC().Format("2006-01-02") != "2026-10-09" || r.SourceURL != "https://lg.pwnation.net/data/v1/generations/g-1791540828176961371/overview.json" || validatePublicHTTPSURL(r.DefinitionSourceURL) != nil {
			t.Fatal("invalid historical hostname receipt", key)
		}
		if _, exists := raw.Records[i]["destination"]; exists {
			t.Fatal("hostname receipt claims a numeric destination", key)
		}
		if b.Method != r.Method || b.Family != r.Family || b.Status != "ok" || b.StartedAt.After(b.ObservedAt) || !b.ObservedAt.Equal(r.ObservedAt) || b.Sent < 1 || b.Received != b.Sent {
			t.Fatal("receipt does not match its genuine successful batch", key)
		}
		receipts[key] = r
	}
	numeric, hostname, pending, slots := 0, 0, 0, 0
	groups := map[string]int{}
	for i, target := range c.Targets {
		previous := old.Targets[i]
		if target.ID != previous.ID || target.Revision != previous.Revision || !sameProbeDefinition(target, previous) {
			t.Fatal("evidence release changed probe identity", target.ID)
		}
		for _, family := range target.Measurement.Families {
			slots++
			a := target.Admission[family]
			if a.State != "verified" {
				pending++
				continue
			}
			e := a.Evidence
			if e.Scope == "numeric_destination" {
				copy := *e
				copy.Scope = ""
				if !reflect.DeepEqual(&copy, previous.Admission[family].Evidence) {
					t.Fatal("historical numeric receipt or evidence pin changed", target.ID)
				}
				numeric++
				continue
			}
			r, exists := receipts[target.ID+"/"+family]
			if !exists || e.Scope != "hostname" || e.Hostname != target.Host || e.Hostname != r.Hostname || e.Port != r.Port || e.Port != evidencePort(target) || e.Method != r.Method || e.Family != r.Family || !e.ObservedAt.Equal(r.ObservedAt) || e.Destination != "" || target.Scheme != r.Scheme || target.Path != r.Path || target.Measurement.HTTPMethod != r.HTTPMethod || target.SourceURL != r.DefinitionSourceURL {
				t.Fatal("manifest and hostname receipt disagree", target.ID, family)
			}
			prefix, suffix := "https://raw.githubusercontent.com/Shaderx/PingObservatory/", "/catalogue/evidence/v3.json"
			ref := strings.TrimSuffix(strings.TrimPrefix(e.SourceURL, prefix), suffix)
			if !strings.HasPrefix(e.SourceURL, prefix) || !strings.HasSuffix(e.SourceURL, suffix) || len(ref) != 40 || strings.Trim(ref, "0123456789abcdef") != "" {
				t.Fatal("hostname evidence is not commit pinned", target.ID)
			}
			hostname++
			groups[e.Method+"/"+family]++
		}
	}
	if len(c.Targets) != 171 || slots != 196 || numeric != 116 || hostname != 22 || pending != 58 || len(receipts) != hostname || !reflect.DeepEqual(groups, map[string]int{"udp_echo/ipv4": 8, "udp_echo/ipv6": 8, "icmp/ipv4": 4, "https/ipv4": 1, "https/ipv6": 1}) {
		t.Fatalf("unexpected v3 population: numeric=%d hostname=%d pending=%d groups=%v", numeric, hostname, pending, groups)
	}
	if !bytes.Equal(read("v3.json"), read("latest.json")) {
		t.Fatal("stable alias differs from immutable snapshot")
	}
}
