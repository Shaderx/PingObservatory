package catalogue

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func validateV2(c model.Catalogue) error {
	if c.Version == 0 || c.Version > MaxSafeInteger {
		return errors.New("catalogue version must be a positive JavaScript-safe integer")
	}
	if c.PublishedAt.IsZero() || c.PublishedAt.Before(time.Unix(0, 0)) || c.PublishedAt.After(time.Now().Add(5*time.Minute)) {
		return errors.New("invalid catalogue published_at")
	}
	if c.Targets == nil || len(c.Targets) > MaxTargets {
		return fmt.Errorf("catalogue targets must be an array with at most %d records; updates are never truncated", MaxTargets)
	}
	seen := make(map[string]bool, len(c.Targets))
	slots := 0
	for _, t := range c.Targets {
		if err := validateTargetVersion(t, c.PublishedAt, c.SchemaVersion); err != nil {
			return fmt.Errorf("target %q: %w", t.ID, err)
		}
		if seen[t.ID] {
			return fmt.Errorf("duplicate target ID %q", t.ID)
		}
		seen[t.ID] = true
		slots += len(t.Measurement.Families)
	}
	if slots > MaxFamilySlots {
		return fmt.Errorf("catalogue exceeds %d family slots; updates are never truncated", MaxFamilySlots)
	}
	return nil
}

func validateTargetV2(t model.Target, published time.Time) error {
	return validateTargetVersion(t, published, 2)
}

func validateTargetVersion(t model.Target, published time.Time, schema int) error {
	if !identifier.MatchString(t.ID) || strings.HasPrefix(t.ID, "manual-") {
		return errors.New("invalid or reserved stable ID")
	}
	if t.Revision == 0 || t.Revision > MaxSafeInteger {
		return errors.New("revision must be a positive JavaScript-safe integer")
	}
	if !validText(t.Label, 128) {
		return errors.New("invalid label")
	}
	if err := validateHostV2(t.Host); err != nil {
		return err
	}
	if t.VerifiedAt != nil || t.VerificationSource != "" || t.VerificationMethod != "" || t.VerificationFamily != "" || t.VerificationDestination != "" {
		return errors.New("schema 2/3 uses per-family admission evidence; legacy verification fields are not allowed")
	}
	if t.Category == "private_baseline" || strings.EqualFold(t.Provider, "Homelab") {
		return errors.New("private target classification is not allowed in the public catalogue")
	}
	for name, value := range map[string]string{"provider": t.Provider, "category": t.Category, "region": t.Region, "world_area": t.WorldArea, "role": t.Role, "location_evidence": t.LocationEvidence} {
		if value != "" && !validText(value, 128) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if t.CountryCode != "" && (len(t.CountryCode) != 2 || t.CountryCode != strings.ToUpper(t.CountryCode) || t.CountryCode[0] < 'A' || t.CountryCode[0] > 'Z' || t.CountryCode[1] < 'A' || t.CountryCode[1] > 'Z') {
		return errors.New("invalid country_code")
	}
	if len(t.Tags) > 16 {
		return errors.New("too many tags")
	}
	for _, tag := range t.Tags {
		if !validText(tag, 64) {
			return errors.New("invalid tag")
		}
	}
	if !validOptionalProse(t.Interpretation, 4096) || !validOptionalProse(t.LocationNote, 2048) {
		return errors.New("invalid interpretation or location note")
	}
	if t.Location != nil && (t.Location.Latitude < -90 || t.Location.Latitude > 90 || t.Location.Longitude < -180 || t.Location.Longitude > 180) {
		return errors.New("location is outside latitude/longitude bounds")
	}
	if t.SourceURL != "" {
		if err := validatePublicHTTPSURL(t.SourceURL); err != nil {
			return fmt.Errorf("invalid source_url: %w", err)
		}
	}
	if t.SourceReviewedAt != "" {
		if _, err := time.Parse("2006-01-02", t.SourceReviewedAt); err != nil {
			return errors.New("source_reviewed_at must be an actual YYYY-MM-DD review date")
		}
	}
	if err := validateProbeSettings(t); err != nil {
		return err
	}
	if len(t.Admission) != len(t.Measurement.Families) {
		return errors.New("admission must declare one state for every configured family")
	}
	for _, family := range t.Measurement.Families {
		admission, ok := t.Admission[family]
		if !ok {
			return fmt.Errorf("missing %s admission", family)
		}
		if !validOptionalProse(admission.Reason, 1024) {
			return errors.New("invalid admission reason")
		}
		switch admission.State {
		case "verified":
			if admission.Evidence == nil {
				return errors.New("verified admission requires actual successful observation evidence")
			}
			if err := validateEvidence(t, family, *admission.Evidence, published, schema); err != nil {
				return err
			}
		case "unverified", "unsupported", "disabled":
			if admission.Evidence != nil {
				return errors.New("non-verified admission cannot carry success evidence")
			}
			if admission.Reason == "" {
				return errors.New("non-verified admission must explain its state")
			}
		default:
			return errors.New("admission state must be verified, unverified, unsupported, or disabled")
		}
	}
	return nil
}

func validateProbeSettings(t model.Target) error {
	p := t.Measurement
	if p.IntervalSeconds != 60 {
		return errors.New("catalogue interval_seconds must be 60")
	}
	if p.PacketsPerCycle < 1 || p.PacketsPerCycle > 5 || p.PacketSpacingMS < 0 || p.PacketSpacingMS > 5000 || p.TimeoutMS < 100 || p.TimeoutMS > 10000 || p.PayloadBytes < 0 || p.PayloadBytes > 1400 {
		return errors.New("probe settings exceed bounded packet, spacing, timeout, or payload limits")
	}
	if len(p.Families) < 1 || len(p.Families) > 2 {
		return errors.New("measurement must declare one or two address families")
	}
	families := map[string]bool{}
	for _, family := range p.Families {
		if (family != "ipv4" && family != "ipv6") || families[family] {
			return errors.New("invalid or repeated measurement family")
		}
		families[family] = true
	}
	if t.Port < 0 || t.Port > 65535 {
		return errors.New("invalid endpoint port")
	}
	if p.Method != model.MethodHTTPS && (t.Scheme != "" || t.Path != "" || p.HTTPMethod != "" || len(p.ExpectedStatusCodes) > 0) {
		return errors.New("HTTPS options are only allowed for https measurements")
	}
	if p.Method != model.MethodDNS && (p.DNSQueryName != "" || p.DNSQueryType != "" || len(p.DNSExpectedAnswers) > 0) {
		return errors.New("DNS options are only allowed for dns measurements")
	}
	switch p.Method {
	case model.MethodICMP:
		if t.Port != 0 || p.PayloadBytes < 8 {
			return errors.New("ICMP requires no port and at least eight payload bytes")
		}
	case model.MethodUDP:
		if t.Port == 0 || p.PayloadBytes < 8 {
			return errors.New("UDP echo requires a port and at least eight payload bytes")
		}
	case model.MethodTCP:
		if t.Port == 0 {
			return errors.New("TCP connect requires a port")
		}
	case model.MethodHTTPS:
		if t.Scheme != "https" || !strings.HasPrefix(t.Path, "/") || strings.HasPrefix(t.Path, "//") || len(t.Path) > 2048 || strings.ContainsAny(t.Path, "\r\n\\") {
			return errors.New("HTTPS requires scheme https and a bounded absolute path")
		}
		if p.HTTPMethod != "GET" && p.HTTPMethod != "HEAD" {
			return errors.New("HTTPS method must be GET or HEAD")
		}
		if len(p.ExpectedStatusCodes) == 0 || len(p.ExpectedStatusCodes) > 16 {
			return errors.New("HTTPS requires bounded expected_status_codes")
		}
		for _, status := range p.ExpectedStatusCodes {
			if status < 100 || status > 599 {
				return errors.New("invalid expected HTTP status")
			}
		}
	case model.MethodDNS:
		if p.DNSQueryType != "A" && p.DNSQueryType != "AAAA" {
			return errors.New("DNS query type must be A or AAAA")
		}
		if err := validateHostV2(p.DNSQueryName); err != nil {
			return errors.New("invalid public DNS query name")
		}
		if len(p.DNSExpectedAnswers) > 16 {
			return errors.New("too many expected DNS answers")
		}
		for _, answer := range p.DNSExpectedAnswers {
			ip, err := netip.ParseAddr(answer)
			if err != nil || !publicAddress(ip) {
				return errors.New("invalid expected public DNS answer")
			}
		}
	default:
		return errors.New("unsupported measurement method")
	}
	return model.ValidateProbeTarget(t)
}

func validateEvidence(t model.Target, family string, e model.Verification, published time.Time, schema int) error {
	if e.Method != t.Measurement.Method || e.Family != family {
		return errors.New("verification must match the configured method and family")
	}
	if e.ObservedAt.IsZero() || e.ObservedAt.Before(time.Unix(0, 0)) || e.ObservedAt.After(published) {
		return errors.New("verification requires the actual success timestamp no later than publication")
	}
	if err := validatePublicHTTPSURL(e.SourceURL); err != nil {
		return fmt.Errorf("invalid verification source_url: %w", err)
	}
	if schema == 2 && (e.Scope != "" || e.Hostname != "" || e.Port != 0) {
		return errors.New("schema 2 accepts only legacy numeric-destination evidence")
	}
	if schema == 3 {
		switch e.Scope {
		case "hostname":
			if _, err := netip.ParseAddr(t.Host); err == nil || e.Hostname != t.Host || e.Destination != "" {
				return errors.New("hostname evidence must match the configured DNS hostname and cannot claim a numeric destination")
			}
			if e.Port != evidencePort(t) {
				return errors.New("hostname evidence port differs from the configured endpoint")
			}
			return nil
		case "numeric_destination":
			if e.Hostname != "" || e.Port != 0 {
				return errors.New("numeric evidence cannot contain hostname-level fields")
			}
		default:
			return errors.New("schema 3 evidence scope must be numeric_destination or hostname")
		}
	}
	var destination netip.Addr
	if e.Method == model.MethodICMP {
		ip, err := netip.ParseAddr(e.Destination)
		if err != nil {
			return errors.New("ICMP evidence destination must be the exact observed IP")
		}
		destination = ip
	} else {
		peer, err := netip.ParseAddrPort(e.Destination)
		if err != nil || peer.Port() == 0 {
			return errors.New("transport evidence destination must be exact IP:port")
		}
		destination = peer.Addr()
		port := evidencePort(t)
		if int(peer.Port()) != port {
			return errors.New("verification destination port differs from the configured endpoint")
		}
	}
	if !publicAddress(destination) || (family == "ipv4") != destination.Is4() {
		return errors.New("verification destination must be a public IP matching the family")
	}
	if literal, err := netip.ParseAddr(t.Host); err == nil && literal != destination {
		return errors.New("verification destination differs from the literal endpoint")
	}
	return nil
}

func evidencePort(t model.Target) int {
	if t.Port == 0 && t.Measurement.Method == model.MethodHTTPS {
		return 443
	}
	if t.Port == 0 && t.Measurement.Method == model.MethodDNS {
		return 53
	}
	return t.Port
}

func publicAddress(ip netip.Addr) bool {
	return model.PublicUnicast(ip)
}
func validateHostV2(host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(ip) {
			return errors.New("catalogue literal endpoint must be public unicast")
		}
		return nil
	}
	return validateHost(host)
}
func validatePublicHTTPSURL(raw string) error {
	if len(raw) > 2048 || !validOptionalProse(raw, 2048) {
		return errors.New("invalid URL text")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("expected public HTTPS URL without credentials or fragment")
	}
	return validateHostV2(u.Hostname())
}
func validOptionalProse(s string, max int) bool {
	if len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return false
		}
	}
	return true
}
