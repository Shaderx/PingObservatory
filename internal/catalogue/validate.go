package catalogue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Shaderx/PingObservatory/internal/model"
)

const (
	MaxTargets        = 64
	MaxCatalogueBytes = 256 * 1024
	// The embedded web UI uses JavaScript numbers for IDs and catalogue counters.
	MaxSafeInteger uint64 = 9007199254740991
	// VerificationSource identifies actual probes from the owner's homelab. It
	// does not promise that an endpoint will reply from every user's network.
	VerificationSource = "owner-homelab"
	DefaultURL         = "https://raw.githubusercontent.com/Shaderx/PingObservatory/main/catalogue/v1.json"
)

var (
	identifier        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	catalogueFilename = regexp.MustCompile(`^v[1-9][0-9]*\.json$`)
)

// ValidateURL allows only the project's versioned JSON catalogues on GitHub.
// An empty URL disables remote updates without disabling the current catalogue.
func ValidateURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return errors.New("catalogue URL must be an HTTPS raw.githubusercontent.com URL without credentials, port, query, or fragment")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 5 || !strings.EqualFold(parts[0], "Shaderx") || parts[1] != "PingObservatory" || !validRef(parts[2]) || parts[3] != "catalogue" || !catalogueFilename.MatchString(parts[4]) {
		return errors.New("catalogue URL must name Shaderx/PingObservatory/<ref>/catalogue/v<number>.json")
	}
	return nil
}

func validRef(ref string) bool {
	if ref == "" || len(ref) > 128 || ref == "." || ref == ".." {
		return false
	}
	for _, c := range ref {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}

// Decode accepts one strict, bounded schema-1 JSON document. Unknown fields,
// duplicate keys, invalid UTF-8, and trailing documents are errors.
func Decode(data []byte) (model.Catalogue, error) {
	var c model.Catalogue
	if len(data) > MaxCatalogueBytes {
		return c, errors.New("catalogue exceeds size limit")
	}
	if err := decodeStrict(data, &c); err != nil {
		return c, fmt.Errorf("invalid catalogue JSON: %w", err)
	}
	if err := requireCatalogueFields(data); err != nil {
		return c, err
	}
	normalize(&c)
	if err := Validate(c); err != nil {
		return c, err
	}
	return c, nil
}

// Validate checks the data contract. A target revision covers its entire record,
// including verification metadata; any record change requires a new revision.
func Validate(c model.Catalogue) error {
	if c.SchemaVersion != 1 {
		return errors.New("unsupported catalogue schema_version")
	}
	if c.Version == 0 || c.Version > MaxSafeInteger {
		return errors.New("catalogue version must be a positive JavaScript-safe integer")
	}
	if c.PublishedAt.IsZero() || c.PublishedAt.Before(time.Unix(0, 0)) || c.PublishedAt.After(time.Now().Add(5*time.Minute)) {
		return errors.New("invalid catalogue published_at")
	}
	if c.Targets == nil || len(c.Targets) > MaxTargets {
		return fmt.Errorf("catalogue targets must be an array with at most %d records", MaxTargets)
	}
	seen := make(map[string]bool, len(c.Targets))
	for _, target := range c.Targets {
		if err := validateTarget(target, c.PublishedAt); err != nil {
			return fmt.Errorf("target %q: %w", target.ID, err)
		}
		if seen[target.ID] {
			return fmt.Errorf("duplicate target ID %q", target.ID)
		}
		seen[target.ID] = true
	}
	return nil
}

func validateTarget(t model.Target, published time.Time) error {
	if !identifier.MatchString(t.ID) || strings.HasPrefix(t.ID, "manual-") {
		return errors.New("invalid stable ID")
	}
	if t.Revision == 0 || t.Revision > MaxSafeInteger {
		return errors.New("revision must be a positive JavaScript-safe integer")
	}
	if !validText(t.Label, 128) {
		return errors.New("label must be nonempty plain text of at most 128 bytes")
	}
	if err := validateHost(t.Host); err != nil {
		return err
	}
	if t.Location != nil && (math.IsNaN(t.Location.Latitude) || math.IsNaN(t.Location.Longitude) || math.IsInf(t.Location.Latitude, 0) || math.IsInf(t.Location.Longitude, 0) || t.Location.Latitude < -90 || t.Location.Latitude > 90 || t.Location.Longitude < -180 || t.Location.Longitude > 180) {
		return errors.New("location is outside latitude/longitude bounds")
	}
	if t.VerifiedAt == nil || t.VerifiedAt.IsZero() || t.VerifiedAt.Before(time.Unix(0, 0)) || t.VerifiedAt.After(published) {
		return errors.New("verified_at must record a real verification time no later than published_at")
	}
	if t.VerificationSource != VerificationSource {
		return fmt.Errorf("verification_source must be %q", VerificationSource)
	}
	if err := validateVerification(t); err != nil {
		return err
	}
	return nil
}

func validateVerification(t model.Target) error {
	var destination netip.Addr
	switch t.VerificationMethod {
	case "icmp":
		ip, err := netip.ParseAddr(t.VerificationDestination)
		if err != nil {
			return errors.New("ICMP verification_destination must be the exact observed public IP address")
		}
		destination = ip
	case "udp_echo", "tcp_connect", "https", "dns":
		peer, err := netip.ParseAddrPort(t.VerificationDestination)
		if err != nil || peer.Port() == 0 {
			return errors.New("verification_destination must be the exact observed public IP:port for this method")
		}
		destination = peer.Addr()
	default:
		return errors.New("verification_method must identify an observed icmp, udp_echo, tcp_connect, https, or dns success")
	}
	if destination.Zone() != "" || destination.Is4In6() || !destination.IsGlobalUnicast() || destination.IsPrivate() || destination.IsLoopback() || destination.IsLinkLocalUnicast() {
		return errors.New("verification_destination must be public unicast")
	}
	if (t.VerificationFamily != "ipv4" && t.VerificationFamily != "ipv6") || (t.VerificationFamily == "ipv4") != destination.Is4() {
		return errors.New("verification_family must be ipv4 or ipv6 and match the observed destination")
	}
	if host, err := netip.ParseAddr(t.Host); err == nil && host != destination {
		return errors.New("verification_destination does not match the literal target host")
	}
	return nil
}

func validText(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) || s != strings.TrimSpace(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func validateHost(host string) error {
	if host == "" || len(host) > 253 || host != strings.TrimSpace(host) {
		return errors.New("invalid host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !ip.Is4() {
			return errors.New("catalogue literal address must be IPv4 for the MVP probe implementation")
		}
		if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return errors.New("catalogue IP must be a public unicast address")
		}
		return nil
	}
	if host != strings.ToLower(host) || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return errors.New("catalogue host must be a lowercase fully qualified public hostname or public IP")
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return errors.New("invalid hostname label")
		}
		for _, c := range part {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return errors.New("invalid hostname character")
			}
		}
	}
	return nil
}

func normalize(c *model.Catalogue) {
	c.PublishedAt = c.PublishedAt.UTC()
	for i := range c.Targets {
		if c.Targets[i].VerifiedAt != nil {
			at := c.Targets[i].VerifiedAt.UTC()
			c.Targets[i].VerifiedAt = &at
		}
	}
}

func decodeStrict(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	keys.UseNumber()
	if err := checkJSONValue(keys, 0); err != nil {
		return err
	}
	if _, err := keys.Token(); err != io.EOF {
		return errors.New("trailing JSON content")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}

func checkJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || name == "" || name != strings.ToLower(name) {
				return errors.New("JSON object keys must use lowercase field names")
			}
			if seen[name] {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			seen[name] = true
			if err := checkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func requireCatalogueFields(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	for _, key := range []string{"schema_version", "version", "published_at", "targets"} {
		if _, ok := root[key]; !ok {
			return fmt.Errorf("missing catalogue field %q", key)
		}
	}
	return requireTargetFields(root["targets"])
}

func requireTargetFields(data []byte) error {
	var targets []map[string]json.RawMessage
	if err := json.Unmarshal(data, &targets); err != nil {
		return err
	}
	for i, t := range targets {
		for _, key := range []string{"id", "revision", "label", "host", "verified_at", "verification_source", "verification_method", "verification_family", "verification_destination"} {
			if _, ok := t[key]; !ok {
				return fmt.Errorf("target %d missing field %q", i, key)
			}
		}
		if point, ok := t["location"]; ok && !bytes.Equal(bytes.TrimSpace(point), []byte("null")) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(point, &fields); err != nil {
				return err
			}
			for _, key := range []string{"latitude", "longitude"} {
				value, ok := fields[key]
				if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return fmt.Errorf("target %d location missing %q", i, key)
				}
			}
		}
	}
	return nil
}
