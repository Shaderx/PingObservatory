package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Shaderx/PingObservatory/internal/model"
)

const DefaultCatalogueURL = "https://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v2.json"

var idPattern = regexp.MustCompile(`^manual-[a-z0-9][a-z0-9_-]{0,47}$`)

func Default() model.Config {
	return model.Config{ManualTargets: []model.Target{}, CatalogueURL: DefaultCatalogueURL, RetentionDays: 30}
}

func Directory() (string, error) {
	p, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "PingObservatory"), nil
}

func Load(path string) (model.Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return model.Config{}, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64*1024+1))
	d.DisallowUnknownFields()
	var c model.Config
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return c, errors.New("config contains trailing data")
	}
	return c, Validate(c)
}

func Validate(c model.Config) error {
	if c.RetentionDays < 1 || c.RetentionDays > 30 {
		return errors.New("retention_days must be between 1 and 30")
	}
	if c.Origin != nil && !ValidPoint(*c.Origin) {
		return errors.New("origin must contain finite latitude -90..90 and longitude -180..180")
	}
	if len(c.ManualTargets) > 16 {
		return errors.New("at most 16 user-provided targets are allowed")
	}
	seen := map[string]bool{}
	for _, t := range c.ManualTargets {
		if !idPattern.MatchString(t.ID) || seen[t.ID] {
			return errors.New("manual target IDs must be unique and start with manual-")
		}
		seen[t.ID] = true
		if t.Revision == 0 || t.Revision > 9007199254740991 {
			return errors.New("target revision must be a positive safe integer")
		}
		if strings.TrimSpace(t.Label) == "" || len(t.Label) > 120 || !ValidHost(t.Host) {
			return errors.New("target label or host is invalid")
		}
		if t.Location != nil && !ValidPoint(*t.Location) {
			return errors.New("target location is invalid")
		}
		if err := model.ValidateProbeTarget(t); err != nil {
			return err
		}
		if t.VerifiedAt != nil || t.VerificationSource != "" || t.VerificationMethod != "" || t.VerificationFamily != "" || t.VerificationDestination != "" || len(t.Admission) > 0 {
			return errors.New("user-provided targets cannot claim catalogue verification")
		}
	}
	return nil
}

func ValidPoint(p model.Point) bool {
	return !math.IsNaN(p.Latitude) && !math.IsInf(p.Latitude, 0) && !math.IsNaN(p.Longitude) && !math.IsInf(p.Longitude, 0) && p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180
}

func ValidHost(s string) bool {
	if len(s) == 0 || len(s) > 253 || strings.TrimSpace(s) != s {
		return false
	}
	if ip := net.ParseIP(s); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast()
	}
	if strings.ContainsAny(s, ":/\\@?* \t\r\n") {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func Save(path string, c model.Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
