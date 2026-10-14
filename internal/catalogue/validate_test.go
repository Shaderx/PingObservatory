package catalogue

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestURLAllowlist(t *testing.T) {
	for _, raw := range []string{"", DefaultURL, "https://raw.githubusercontent.com/shaderx/PingObservatory/v0.1.0/catalogue/v1.json", "https://raw.githubusercontent.com/Shaderx/PingObservatory/0123456789abcdef/catalogue/v2.json"} {
		if err := ValidateURL(raw); err != nil {
			t.Fatalf("rejected valid URL %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v1.json", "https://raw.githubusercontent.com:443/Shaderx/PingObservatory/main/catalogue/v1.json", "https://user:pass@raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v1.json", DefaultURL + "?token=x", DefaultURL + "#fragment", "https://raw.githubusercontent.com/other/PingObservatory/main/catalogue/v1.json", "https://raw.githubusercontent.com/Shaderx/Other/main/catalogue/v1.json", "https://example.com/catalogue/v1.json", "https://raw.githubusercontent.com.evil.example/Shaderx/PingObservatory/main/catalogue/v1.json", "https://raw.githubusercontent.com/Shaderx/PingObservatory/../catalogue/v1.json", "https://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/%761.json", "https://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v0.json", "https://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v1.exe"} {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("accepted invalid URL %q", raw)
		}
	}
}

func TestStrictJSON(t *testing.T) {
	valid := encoded(t, fixture(2))
	for _, test := range []struct{ name, data string }{
		{"trailing document", valid + ` {}`},
		{"trailing garbage", valid + ` nope`},
		{"duplicate keys", strings.Replace(valid, `"version":2`, `"version":1,"version":2`, 1)},
		{"uppercase keys", strings.Replace(valid, `"version"`, `"VERSION"`, 1)},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"script":"run"}`},
		{"nested unknown field", strings.Replace(valid, `"host":"example.com"`, `"host":"example.com","args":["x"]`, 1)},
		{"missing coordinate", strings.Replace(valid, `"latitude":20,`, ``, 1)},
		{"null coordinate", strings.Replace(valid, `"latitude":20`, `"latitude":null`, 1)},
		{"invalid UTF-8", strings.Replace(valid, "Test endpoint", string([]byte{0xff}), 1)},
		{"null catalogue", `null`},
		{"null targets", `{"schema_version":1,"version":2,"published_at":"2026-10-14T00:00:00Z","targets":null}`},
		{"missing targets", `{"schema_version":1,"version":2,"published_at":"2026-10-14T00:00:00Z"}`},
		{"invalid timestamp", strings.Replace(valid, "2026-10-14T00:00:00Z", "yesterday", 1)},
		{"too deep", strings.Repeat("[", 20) + strings.Repeat("]", 20)},
		{"oversized", strings.Repeat(" ", MaxCatalogueBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode([]byte(test.data)); err == nil {
				t.Fatal("invalid catalogue accepted")
			}
		})
	}
	if _, err := Decode([]byte(valid + " \n\t")); err != nil {
		t.Fatalf("valid trailing whitespace rejected: %v", err)
	}
}

func TestDataValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*model.Catalogue)
	}{
		{"wrong schema", func(c *model.Catalogue) { c.SchemaVersion = 2 }},
		{"zero version", func(c *model.Catalogue) { c.Version = 0 }},
		{"unsafe version", func(c *model.Catalogue) { c.Version = MaxSafeInteger + 1 }},
		{"zero revision", func(c *model.Catalogue) { c.Targets[0].Revision = 0 }},
		{"unsafe revision", func(c *model.Catalogue) { c.Targets[0].Revision = MaxSafeInteger + 1 }},
		{"bad ID", func(c *model.Catalogue) { c.Targets[0].ID = "../unsafe" }},
		{"reserved manual ID", func(c *model.Catalogue) { c.Targets[0].ID = "manual-router" }},
		{"empty label", func(c *model.Catalogue) { c.Targets[0].Label = "" }},
		{"label controls", func(c *model.Catalogue) { c.Targets[0].Label = "bad\nlabel" }},
		{"host arguments", func(c *model.Catalogue) { c.Targets[0].Host = "example.com -n 10" }},
		{"host URL", func(c *model.Catalogue) { c.Targets[0].Host = "https://example.com" }},
		{"host port", func(c *model.Catalogue) { c.Targets[0].Host = "example.com:443" }},
		{"loopback IP", func(c *model.Catalogue) { c.Targets[0].Host = "127.0.0.1" }},
		{"private IP", func(c *model.Catalogue) { c.Targets[0].Host = "10.2.3.4" }},
		{"private IPv6", func(c *model.Catalogue) { c.Targets[0].Host = "fd00::1" }},
		{"public IPv6 unsupported", func(c *model.Catalogue) { c.Targets[0].Host = "2606:4700:4700::1111" }},
		{"mapped IPv6 unsupported", func(c *model.Catalogue) { c.Targets[0].Host = "::ffff:1.1.1.1" }},
		{"local hostname", func(c *model.Catalogue) { c.Targets[0].Host = "thing.local" }},
		{"latitude bounds", func(c *model.Catalogue) { c.Targets[0].Location.Latitude = 91 }},
		{"longitude bounds", func(c *model.Catalogue) { c.Targets[0].Location.Longitude = -181 }},
		{"missing verification", func(c *model.Catalogue) { c.Targets[0].VerifiedAt = nil }},
		{"wrong verifier", func(c *model.Catalogue) { c.Targets[0].VerificationSource = "made-up" }},
		{"missing observed method", func(c *model.Catalogue) { c.Targets[0].VerificationMethod = "" }},
		{"unknown observed method", func(c *model.Catalogue) { c.Targets[0].VerificationMethod = "script" }},
		{"missing observed family", func(c *model.Catalogue) { c.Targets[0].VerificationFamily = "" }},
		{"wrong observed family", func(c *model.Catalogue) { c.Targets[0].VerificationFamily = "ipv6" }},
		{"missing exact destination", func(c *model.Catalogue) { c.Targets[0].VerificationDestination = "" }},
		{"hostname destination ambiguous", func(c *model.Catalogue) { c.Targets[0].VerificationDestination = "example.com" }},
		{"private observed destination", func(c *model.Catalogue) { c.Targets[0].VerificationDestination = "10.0.0.1" }},
		{"literal host mismatch", func(c *model.Catalogue) { c.Targets[0].Host = "8.8.8.8" }},
		{"missing transport port", func(c *model.Catalogue) { c.Targets[0].VerificationMethod = "tcp_connect" }},
		{"verification after publication", func(c *model.Catalogue) { at := c.PublishedAt.Add(time.Second); c.Targets[0].VerifiedAt = &at }},
		{"future publication", func(c *model.Catalogue) { c.PublishedAt = time.Now().Add(time.Hour) }},
		{"duplicate target", func(c *model.Catalogue) { c.Targets = append(c.Targets, c.Targets[0]) }},
		{"too many targets", func(c *model.Catalogue) {
			target := c.Targets[0]
			for i := 1; i <= MaxTargets; i++ {
				target.ID = fmt.Sprintf("target-%d", i)
				c.Targets = append(c.Targets, target)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixture(2)
			test.change(&c)
			if err := Validate(c); err == nil {
				t.Fatal("invalid catalogue accepted")
			}
		})
	}
	for _, host := range []string{"example.com", "1.1.1.1"} {
		c := fixture(2)
		c.Targets[0].Host = host
		if err := Validate(c); err != nil {
			t.Fatalf("valid host %q rejected: %v", host, err)
		}
	}
}

func TestObservedVerificationMethodsAndFamilies(t *testing.T) {
	for _, test := range []struct{ method, family, destination string }{
		{"icmp", "ipv4", "1.1.1.1"},
		{"icmp", "ipv6", "2606:4700:4700::1111"},
		{"udp_echo", "ipv4", "1.1.1.1:1935"},
		{"tcp_connect", "ipv4", "1.1.1.1:443"},
		{"https", "ipv6", "[2606:4700:4700::1111]:443"},
		{"dns", "ipv4", "1.1.1.1:53"},
	} {
		t.Run(test.method+"/"+test.family, func(t *testing.T) {
			c := fixture(2)
			c.Targets[0].VerificationMethod = test.method
			c.Targets[0].VerificationFamily = test.family
			c.Targets[0].VerificationDestination = test.destination
			if _, err := Decode([]byte(encoded(t, c))); err != nil {
				t.Fatalf("faithfully identified reachability evidence rejected: %v", err)
			}
		})
	}
}

func TestLargestExactIntegerIsAccepted(t *testing.T) {
	c := fixture(MaxSafeInteger)
	c.Targets[0].Revision = MaxSafeInteger
	if err := Validate(c); err != nil {
		t.Fatalf("largest exact integer rejected: %v", err)
	}
	if _, err := Decode([]byte(encoded(t, c))); err != nil {
		t.Fatalf("largest exact integer lost in JSON decoding: %v", err)
	}
}

func TestTimestampsNormalizeWithoutEquivocation(t *testing.T) {
	c := fixture(2)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "2026-10-13T12:00:00Z", "2026-10-13T14:00:00+02:00", 1))
	next, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTransition(c, map[string]model.Target{c.Targets[0].ID: c.Targets[0]}, next); err != nil {
		t.Fatalf("equivalent timestamp rejected: %v", err)
	}
}

func TestPublicationRegression(t *testing.T) {
	c, next := fixture(2), fixture(3)
	next.PublishedAt = c.PublishedAt.Add(-time.Minute)
	if err := validateTransition(c, map[string]model.Target{}, next); err == nil {
		t.Fatal("publication regression accepted")
	}
}
