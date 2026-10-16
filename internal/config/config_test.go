package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestConfigRoundTripAndHostValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c.Origin = &model.Point{Latitude: 10, Longitude: 20}
	c.ManualTargets = []model.Target{{ID: "manual-local", Revision: 1, Label: "Local", Host: "127.0.0.1"}}
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	c, err = Load(p)
	if err != nil || len(c.ManualTargets) != 1 || c.Origin.Latitude != 10 {
		t.Fatalf("round trip: %+v %v", c, err)
	}
	for _, host := range []string{"127.0.0.1", "::1", "example.com", "one.example.test"} {
		if !ValidHost(host) {
			t.Fatalf("valid host rejected %q", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "https://example.com", "x;run", "-invalid.test", "foo..test", "a/b", "x\n"} {
		if ValidHost(host) {
			t.Fatalf("invalid host accepted %q", host)
		}
	}
	c.Origin.Latitude = math.NaN()
	if Validate(c) == nil {
		t.Fatal("non-finite origin accepted")
	}
}

func TestConfigRejectsUntrustedVerificationAndTrailingData(t *testing.T) {
	c := Default()
	c.ManualTargets = []model.Target{{ID: "manual-local", Revision: 1, Label: "Local", Host: "127.0.0.1", VerificationSource: "owner-homelab"}}
	if Validate(c) == nil {
		t.Fatal("manual verification claim accepted")
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"manual_targets":[],"catalogue_url":"","retention_days":7} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
