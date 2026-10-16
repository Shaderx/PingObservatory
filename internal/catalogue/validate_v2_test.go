package catalogue

import (
	"strings"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func fixtureV2(version uint64) model.Catalogue {
	c := fixture(version)
	c.SchemaVersion = 2
	t := &c.Targets[0]
	t.VerifiedAt = nil
	t.VerificationSource = ""
	t.VerificationMethod = ""
	t.VerificationFamily = ""
	t.VerificationDestination = ""
	t.Provider = "Test provider"
	t.Category = "regional"
	t.Region = "Test region"
	t.CountryCode = "US"
	t.Tags = []string{"test"}
	t.LocationEvidence = "provider_documented_region"
	t.SourceURL = "https://example.com/endpoints"
	t.SourceReviewedAt = "2026-10-13"
	t.Measurement = model.ProbeSettings{Method: model.MethodICMP, Families: []string{"ipv4", "ipv6"}, IntervalSeconds: 60, PacketsPerCycle: 3, PacketSpacingMS: 1000, TimeoutMS: 1000, PayloadBytes: 32}
	t.Admission = map[string]model.Admission{"ipv4": {State: "verified", Evidence: &model.Verification{Method: model.MethodICMP, Family: "ipv4", Destination: "1.1.1.1", ObservedAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), SourceURL: "https://example.com/current/test-endpoint"}}, "ipv6": {State: "unverified", Reason: "No destination-bound successful observation is available."}}
	return c
}

func TestSchema2FullCoverageAndExplicitAdmissions(t *testing.T) {
	c := fixtureV2(2)
	for len(c.Targets) < 171 {
		tgt := cloneTarget(c.Targets[0])
		tgt.ID = "endpoint-" + strings.Repeat("x", len(c.Targets)/100) + time.Unix(int64(len(c.Targets)), 0).UTC().Format("150405")
		tgt.Admission = map[string]model.Admission{"ipv4": {State: "unverified", Reason: "Research definition only."}, "ipv6": {State: "unsupported", Reason: "Provider documents no IPv6 support."}}
		c.Targets = append(c.Targets, tgt)
	}
	decoded, err := Decode([]byte(encoded(t, c)))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Targets) != 171 {
		t.Fatalf("catalogue was trimmed: %d", len(decoded.Targets))
	}
	for _, state := range []string{"unverified", "unsupported", "disabled"} {
		pending := fixtureV2(2)
		pending.Targets[0].Admission["ipv4"] = model.Admission{State: state, Reason: "Explicit pending admission."}
		if err := Validate(pending); err != nil {
			t.Fatalf("valid %s state rejected: %v", state, err)
		}
	}
}

func TestSchema2RejectsInvalidDefinitionsAndEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*model.Target)
	}{
		{"missing endpoint", func(t *model.Target) { t.Host = "" }},
		{"legacy evidence", func(t *model.Target) { t.VerificationSource = VerificationSource }},
		{"private target", func(t *model.Target) { t.Category = "private_baseline" }},
		{"bad interval", func(t *model.Target) { t.Measurement.IntervalSeconds = 1 }},
		{"oversized batch", func(t *model.Target) { t.Measurement.PacketsPerCycle = 100 }},
		{"unknown method", func(t *model.Target) { t.Measurement.Method = "execute" }},
		{"duplicate family", func(t *model.Target) { t.Measurement.Families = []string{"ipv4", "ipv4"} }},
		{"missing family state", func(t *model.Target) { delete(t.Admission, "ipv6") }},
		{"extra family state", func(t *model.Target) { t.Admission["other"] = model.Admission{State: "disabled", Reason: "No"} }},
		{"unknown admission", func(t *model.Target) { t.Admission["ipv6"] = model.Admission{State: "assumed"} }},
		{"verified without proof", func(t *model.Target) { t.Admission["ipv4"] = model.Admission{State: "verified"} }},
		{"pending with proof", func(t *model.Target) { a := t.Admission["ipv4"]; a.State = "unverified"; t.Admission["ipv4"] = a }},
		{"proof wrong method", func(t *model.Target) { t.Admission["ipv4"].Evidence.Method = "https" }},
		{"proof wrong family", func(t *model.Target) { t.Admission["ipv4"].Evidence.Family = "ipv6" }},
		{"proof missing actual time", func(t *model.Target) { t.Admission["ipv4"].Evidence.ObservedAt = time.Time{} }},
		{"proof future time", func(t *model.Target) { t.Admission["ipv4"].Evidence.ObservedAt = time.Now().Add(time.Hour) }},
		{"proof private address", func(t *model.Target) { t.Admission["ipv4"].Evidence.Destination = "10.0.0.1" }},
		{"proof missing numeric destination", func(t *model.Target) { t.Admission["ipv4"].Evidence.Destination = "example.com" }},
		{"proof literal mismatch", func(t *model.Target) { t.Host = "8.8.8.8" }},
		{"proof executable URL", func(t *model.Target) { t.Admission["ipv4"].Evidence.SourceURL = "javascript:run()" }},
		{"private source URL", func(t *model.Target) { t.SourceURL = "https://127.0.0.1/config" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixtureV2(2)
			test.change(&c.Targets[0])
			if err := Validate(c); err == nil {
				t.Fatal("invalid schema-2 record accepted")
			}
		})
	}
}

func TestSchema2PreservesExactMethodPortAndFamily(t *testing.T) {
	for _, method := range []string{model.MethodUDP, model.MethodTCP, model.MethodHTTPS, model.MethodDNS} {
		t.Run(method, func(t *testing.T) {
			c := fixtureV2(2)
			tgt := &c.Targets[0]
			tgt.Measurement.Method = method
			if method != model.MethodUDP {
				tgt.Measurement.PacketsPerCycle = 1
				tgt.Measurement.PacketSpacingMS = 0
			}
			tgt.Port = 443
			if method == model.MethodUDP {
				tgt.Port = 7770
			}
			if method == model.MethodHTTPS {
				tgt.Scheme = "https"
				tgt.Path = "/"
				tgt.Measurement.HTTPMethod = "HEAD"
				tgt.Measurement.ExpectedStatusCodes = []int{200}
			}
			if method == model.MethodDNS {
				tgt.Port = 53
				tgt.Measurement.DNSQueryName = "example.com"
				tgt.Measurement.DNSQueryType = "A"
				tgt.Measurement.DNSExpectedAnswers = []string{"1.1.1.1"}
			}
			e := tgt.Admission["ipv4"].Evidence
			e.Method = method
			e.Destination = "1.1.1.1:" + map[string]string{model.MethodUDP: "7770", model.MethodTCP: "443", model.MethodHTTPS: "443", model.MethodDNS: "53"}[method]
			if err := Validate(c); err != nil {
				t.Fatal(err)
			}
			e.Destination = "1.1.1.1:1"
			if err := Validate(c); err == nil {
				t.Fatal("changed configured port admitted")
			}
		})
	}
	c := fixtureV2(2)
	tgt := &c.Targets[0]
	tgt.Host = "2606:4700:4700::1111"
	tgt.Measurement.Families = []string{"ipv6"}
	tgt.Admission = map[string]model.Admission{"ipv6": {State: "verified", Evidence: &model.Verification{Method: model.MethodICMP, Family: "ipv6", Destination: tgt.Host, ObservedAt: c.PublishedAt.Add(-time.Hour), SourceURL: "https://example.com/current"}}}
	if err := Validate(c); err != nil {
		t.Fatalf("supported public IPv6 literal rejected: %v", err)
	}
}

func TestSchema2RevisionTracksProbeDefinitionOnly(t *testing.T) {
	c := fixtureV2(2)
	ledger := map[string]model.Target{c.Targets[0].ID: cloneTarget(c.Targets[0])}
	next := cloneCatalogue(c)
	next.Version = 3
	next.Targets[0].Label = "New public display name"
	next.Targets[0].Provider = "Updated provider"
	next.Targets[0].Admission["ipv4"].Evidence.ObservedAt = next.Targets[0].Admission["ipv4"].Evidence.ObservedAt.Add(time.Minute)
	if err := validateTransition(c, ledger, next); err != nil {
		t.Fatalf("proof/display refresh partitioned target history: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*model.Target)
	}{{"host", func(t *model.Target) { t.Host = "other.example.com" }}, {"port", func(t *model.Target) { t.Port = 53 }}, {"path", func(t *model.Target) { t.Path = "/changed" }}, {"method", func(t *model.Target) { t.Measurement.Method = model.MethodUDP }}, {"families", func(t *model.Target) { t.Measurement.Families = []string{"ipv4"} }}, {"packet count", func(t *model.Target) { t.Measurement.PacketsPerCycle++ }}, {"timeout", func(t *model.Target) { t.Measurement.TimeoutMS++ }}, {"payload", func(t *model.Target) { t.Measurement.PayloadBytes++ }}} {
		t.Run(test.name, func(t *testing.T) {
			changed := cloneCatalogue(next)
			test.change(&changed.Targets[0])
			if err := validateTransition(c, ledger, changed); err == nil {
				t.Fatal("intrinsic definition change accepted at same revision")
			}
			changed.Targets[0].Revision++
			if err := validateTransition(c, ledger, changed); err != nil {
				t.Fatal(err)
			}
		})
	}
	next.Version = c.Version
	if err := validateTransition(c, ledger, next); err == nil {
		t.Fatal("same catalogue version content changed")
	}
}

func TestSchema2SnapshotDoesNotShareMapsSlicesOrEvidence(t *testing.T) {
	c := fixtureV2(2)
	support := true
	c.Targets[0].Measurement.ProviderIPv6Support = &support
	c.Targets[0].Measurement.ExpectedStatusCodes = []int{200}
	c.Targets[0].Measurement.DNSExpectedAnswers = []string{"1.1.1.1"}
	m := &Manager{current: c, source: "bundled"}
	copy, _ := m.Snapshot()
	tgt := &copy.Targets[0]
	tgt.Tags[0] = "changed"
	tgt.Measurement.Families[0] = "changed"
	tgt.Measurement.ExpectedStatusCodes[0] = 404
	tgt.Measurement.DNSExpectedAnswers[0] = "8.8.8.8"
	*tgt.Measurement.ProviderIPv6Support = false
	tgt.Admission["ipv4"].Evidence.Destination = "8.8.8.8"
	tgt.Admission["ipv6"] = model.Admission{State: "disabled", Reason: "changed"}
	original, _ := m.Snapshot()
	tgt = &original.Targets[0]
	if tgt.Tags[0] != "test" || tgt.Measurement.Families[0] != "ipv4" || tgt.Measurement.ExpectedStatusCodes[0] != 200 || tgt.Measurement.DNSExpectedAnswers[0] != "1.1.1.1" || !*tgt.Measurement.ProviderIPv6Support || tgt.Admission["ipv4"].Evidence.Destination != "1.1.1.1" || tgt.Admission["ipv6"].State != "unverified" {
		t.Fatal("snapshot shares schema-2 mutable data")
	}
}
