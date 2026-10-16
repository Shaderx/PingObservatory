package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/catalogue"
	"github.com/Shaderx/PingObservatory/internal/model"
)

type destinationFixtureTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (f destinationFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.URL.Scheme, copy.URL.Host = f.base.Scheme, f.base.Host
	response, err := f.transport.RoundTrip(copy)
	if response != nil {
		response.Request = r
	}
	return response, err
}

func TestCatalogueProvenanceAndFailuresPreserveLocalDestinations(t *testing.T) {
	// All observations and curator changes here are synthetic test fixtures.
	// The production GitHub URL is routed to this owned loopback server.
	var candidate model.Catalogue
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		json.NewEncoder(w).Encode(candidate)
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	original := http.DefaultTransport
	http.DefaultTransport = destinationFixtureTransport{base: base, transport: original}
	defer func() { http.DefaultTransport = original }()
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Collect = false
	data, err := os.ReadFile(filepath.Join("..", "..", "catalogue", "v3.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = catalogue.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	index := -1
	for i, target := range candidate.Targets {
		if target.Admission[model.FamilyIPv4].Evidence != nil && target.Admission[model.FamilyIPv4].Evidence.Scope == "hostname" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("missing hostname fixture")
	}
	target := candidate.Targets[index]
	now := time.Now().UTC()
	for i, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		at := now.Add(time.Duration(i-3) * time.Minute)
		latency := 10.0
		m := model.Measurement{TargetID: target.ID, TargetRevision: target.Revision, At: at, BatchStartedAt: at, BatchEndedAt: at, Method: target.Measurement.Method, Family: model.FamilyIPv4, Status: model.StatusOK, Attempted: 1, Sent: 1, Received: 1, HasObservation: true, LatencyMS: &latency, Samples: []model.RTTSample{{At: at, LatencyMS: latency, Sequence: 1}}, DestinationIP: ip}
		if err := a.store.Add(m); err != nil {
			t.Fatal(err)
		}
	}
	before, err := a.store.Statistics([]model.Target{target}, now)
	if err != nil {
		t.Fatal(err)
	}
	want := before[0].Observation.Destination
	if want == nil || want.ChangeCount != 1 {
		t.Fatal("fixture not seeded")
	}
	candidate.Version++
	candidate.PublishedAt = now
	// A curator's numeric observation is provenance, not this client's peer.
	e := candidate.Targets[index].Admission[model.FamilyIPv4].Evidence
	e.Scope, e.Hostname, e.Port, e.Destination = "numeric_destination", "", 0, "9.9.9.9:7770"
	e.ObservedAt = now.Add(-time.Minute)
	e.SourceURL = "https://example.com/synthetic-curator-receipt"
	if err := a.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		actual, err := a.store.Statistics([]model.Target{target}, now)
		if err != nil || !reflect.DeepEqual(want, actual[0].Observation.Destination) {
			t.Fatal("catalogue update changed local peer baseline/history", err)
		}
	}
	check()
	cacheBefore, err := os.ReadFile(filepath.Join(dir, "catalogue.json"))
	if err != nil {
		t.Fatal(err)
	}
	status = http.StatusServiceUnavailable
	if err := a.refresh(context.Background()); err == nil {
		t.Fatal("failed catalogue accepted")
	}
	check()
	status = http.StatusOK
	candidate.Version = 3
	if err := a.refresh(context.Background()); err == nil {
		t.Fatal("stale catalogue accepted")
	}
	check()
	cacheAfter, _ := os.ReadFile(filepath.Join(dir, "catalogue.json"))
	if string(cacheAfter) != string(cacheBefore) {
		t.Fatal("failed/rollback fetch changed cache")
	}
	candidate.Version = 5
	candidate.Targets[index].Admission[model.FamilyIPv4] = model.Admission{State: "unverified", Reason: "Synthetic evidence withdrawn."}
	if err := a.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	check()
	for _, job := range a.Jobs() {
		if job.Target.ID == target.ID && job.Family == model.FamilyIPv4 {
			t.Fatal("pending family remained scheduled")
		}
	}
	if len(a.Targets()) != 171 || a.Targets()[index].Revision != target.Revision {
		t.Fatal("provenance fragmented target identity")
	}
}
