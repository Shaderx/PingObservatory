package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestSchema3UpgradeRetainsRemovedNumericLedgerIdentities(t *testing.T) {
	old := fixtureV2(2)
	removed := cloneTarget(old.Targets[0])
	removed.ID = "removed-endpoint"
	old.Targets = append(old.Targets, removed)
	next := fixtureV3()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, encoded(t, next)) }))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "catalogue.json")
	m, err := newManager(cache, DefaultURL, []byte(encoded(t, old)))
	if err != nil {
		t.Fatal(err)
	}
	m.client.Transport = loopbackTransport{base: base, transport: server.Client().Transport}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := newManager(cache, "", []byte(encoded(t, old)))
	if err != nil {
		t.Fatal(err)
	}
	_, source := loaded.Snapshot()
	if source != "cache" || loaded.LastError() != "" || loaded.identities[removed.ID].Admission["ipv4"].Evidence.Scope != "numeric_destination" {
		t.Fatal("removed legacy ledger identity broke schema3 cache reopening", loaded.LastError())
	}
	removed.Admission["ipv4"].Evidence.Scope = "numeric_destination"
	removed.Host = "changed.example.com"
	next.Version++
	next.Targets = append(next.Targets, removed)
	if err := validateTransition(loaded.current, loaded.identities, next); err == nil {
		t.Fatal("migration forgot the removed ID's previous probe definition")
	}
}

func TestSchema3RejectsForbiddenFieldPresence(t *testing.T) {
	for _, schema := range []int{2, 3} {
		for _, field := range []string{"scope", "Scope", "hostname", "Hostname", "port", "Port", "destination", "Destination", "DESTINATION"} {
			lower := strings.ToLower(field)
			if schema == 3 && (lower == "scope" || lower == "hostname") {
				continue
			}
			if schema == 2 && lower == "destination" {
				continue
			}
			for _, value := range []any{nil, "", 0} {
				c := fixtureV3()
				if schema == 2 {
					c = fixtureV2(2)
				}
				var raw map[string]any
				if err := json.Unmarshal([]byte(encoded(t, c)), &raw); err != nil {
					t.Fatal(err)
				}
				e := raw["targets"].([]any)[0].(map[string]any)["admission"].(map[string]any)["ipv4"].(map[string]any)["evidence"].(map[string]any)
				e[field] = value
				if _, err := Decode([]byte(encoded(t, raw))); err == nil {
					t.Fatalf("schema%d accepted forbidden %s=%v", schema, field, value)
				}
			}
		}
	}
	c := fixtureV2(3)
	c.SchemaVersion = 3
	c.Targets[0].Admission["ipv4"].Evidence.Scope = "numeric_destination"
	for _, field := range []string{"hostname", "Hostname", "HOSTNAME", "port", "Port", "PORT"} {
		var raw map[string]any
		json.Unmarshal([]byte(encoded(t, c)), &raw)
		e := raw["targets"].([]any)[0].(map[string]any)["admission"].(map[string]any)["ipv4"].(map[string]any)["evidence"].(map[string]any)
		e[field] = nil
		if _, err := Decode([]byte(encoded(t, raw))); err == nil {
			t.Fatal("numeric scope accepted forbidden null", field)
		}
	}
}

func fixtureV3() model.Catalogue {
	c := fixtureV2(3)
	c.SchemaVersion = 3
	e := c.Targets[0].Admission["ipv4"].Evidence
	e.Scope = "hostname"
	e.Hostname = c.Targets[0].Host
	e.Destination = ""
	return c
}

func TestSchema3KeepsEvidenceScopesExplicit(t *testing.T) {
	for _, method := range []string{model.MethodICMP, model.MethodUDP, model.MethodTCP, model.MethodHTTPS, model.MethodDNS} {
		t.Run(method, func(t *testing.T) {
			c := fixtureV3()
			target := &c.Targets[0]
			target.Measurement.Method = method
			if method != model.MethodICMP && method != model.MethodUDP {
				target.Measurement.PacketsPerCycle = 1
				target.Measurement.PacketSpacingMS = 0
			}
			if method != model.MethodICMP {
				target.Port = 7770
			}
			if method == model.MethodHTTPS {
				target.Port, target.Scheme, target.Path = 443, "https", "/"
				target.Measurement.HTTPMethod = "HEAD"
				target.Measurement.ExpectedStatusCodes = []int{200}
			}
			if method == model.MethodDNS {
				target.Port = 53
				target.Measurement.DNSQueryName, target.Measurement.DNSQueryType = "example.com", "A"
				target.Measurement.DNSExpectedAnswers = []string{"1.1.1.1"}
			}
			e := target.Admission["ipv4"].Evidence
			e.Method, e.Port = method, target.Port
			if _, err := Decode([]byte(encoded(t, c))); err != nil {
				t.Fatal(err)
			}
			e.Port++
			if err := Validate(c); err == nil {
				t.Fatal("hostname evidence lost the exact endpoint port")
			}
		})
	}
	for _, test := range []struct {
		name string
		edit func(*model.Target)
	}{
		{"missing scope", func(t *model.Target) { t.Admission["ipv4"].Evidence.Scope = "" }},
		{"unknown scope", func(t *model.Target) { t.Admission["ipv4"].Evidence.Scope = "dns_guess" }},
		{"wrong hostname", func(t *model.Target) { t.Admission["ipv4"].Evidence.Hostname = "other.example.com" }},
		{"numeric target", func(t *model.Target) { t.Host, t.Admission["ipv4"].Evidence.Hostname = "1.1.1.1", "1.1.1.1" }},
		{"fabricated numeric peer", func(t *model.Target) { t.Admission["ipv4"].Evidence.Destination = "1.1.1.1" }},
		{"borrowed family", func(t *model.Target) { t.Admission["ipv4"].Evidence.Family = "ipv6" }},
		{"borrowed method", func(t *model.Target) { t.Admission["ipv4"].Evidence.Method = "https" }},
		{"missing time", func(t *model.Target) { t.Admission["ipv4"].Evidence.ObservedAt = time.Time{} }},
		{"future time", func(t *model.Target) { t.Admission["ipv4"].Evidence.ObservedAt = time.Now().Add(time.Hour) }},
		{"missing source", func(t *model.Target) { t.Admission["ipv4"].Evidence.SourceURL = "" }},
		{"pending with success", func(t *model.Target) {
			a := t.Admission["ipv4"]
			a.State, a.Reason = "unverified", "No retained success"
			t.Admission["ipv4"] = a
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixtureV3()
			test.edit(&c.Targets[0])
			if err := Validate(c); err == nil {
				t.Fatal("invalid evidence admitted")
			}
		})
	}
	c := fixtureV2(3)
	c.SchemaVersion = 3
	c.Targets[0].Admission["ipv4"].Evidence.Scope = "numeric_destination"
	if err := Validate(c); err != nil {
		t.Fatal("schema3 numeric evidence rejected", err)
	}
	c.Targets[0].Admission["ipv4"].Evidence.Hostname = "example.com"
	if err := Validate(c); err == nil {
		t.Fatal("mixed evidence scopes accepted")
	}
	c = fixtureV3()
	c.SchemaVersion = 2
	if err := Validate(c); err == nil {
		t.Fatal("hostname evidence silently changed the schema2 contract")
	}
}

func TestSchema3ProvenanceRefreshKeepsProbeIdentity(t *testing.T) {
	c := fixtureV2(2)
	next := fixtureV3()
	ledger := map[string]model.Target{c.Targets[0].ID: c.Targets[0]}
	if err := validateTransition(c, ledger, next); err != nil {
		t.Fatal("hostname evidence promotion split unchanged measurement identity", err)
	}
	changed := cloneCatalogue(next)
	changed.Version++
	changed.Targets[0].Admission["ipv4"].Evidence.ObservedAt = changed.PublishedAt.Add(-time.Minute)
	if err := validateTransition(next, ledger, changed); err != nil {
		t.Fatal("provenance refresh changed probe identity", err)
	}
	if err := validateTransition(next, ledger, c); err == nil {
		t.Fatal("schema/version rollback accepted")
	}
}
