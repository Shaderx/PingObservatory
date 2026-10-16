package model

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

func ValidateProbeTarget(t Target) error {
	p := Settings(t)
	if p.Method != MethodICMP && p.Method != MethodUDP && p.Method != MethodTCP && p.Method != MethodHTTPS && p.Method != MethodDNS {
		return errors.New("unsupported measurement method")
	}
	if p.IntervalSeconds != 60 {
		return errors.New("the desktop collector interval must be 60 seconds")
	}
	if len(p.Families) < 1 || len(p.Families) > 2 {
		return errors.New("one or two address families are required")
	}
	seen := map[string]bool{}
	for _, f := range p.Families {
		if f != FamilyIPv4 && f != FamilyIPv6 || seen[f] {
			return errors.New("families must be unique ipv4/ipv6 values")
		}
		seen[f] = true
	}
	if ip, err := netip.ParseAddr(t.Host); err == nil {
		if ip.Zone() != "" || ip.Is4In6() {
			return errors.New("address zones and mapped IPv6 are unsupported")
		}
		f := FamilyIPv6
		if ip.Is4() {
			f = FamilyIPv4
		}
		for _, configured := range p.Families {
			if configured != f && t.Admission[configured].State != "unsupported" {
				return errors.New("literal address mismatch requires an explicit unsupported admission")
			}
		}
	}
	if p.TimeoutMS < 100 || p.TimeoutMS > 3000 || p.PacketSpacingMS < 0 || p.PacketSpacingMS > 3000 {
		return errors.New("timeout or packet spacing is outside supported bounds")
	}
	if p.PacketsPerCycle < 1 || p.PacketsPerCycle > 20 {
		return errors.New("packet count must be 1..20")
	}
	if 2*p.TimeoutMS+(p.PacketsPerCycle-1)*p.PacketSpacingMS+1000 > 30000 {
		return errors.New("the complete probe cycle must fit within 30 seconds")
	}
	if p.Method == MethodICMP && p.PacketsPerCycle > 1 && p.PacketSpacingMS < 100 {
		return errors.New("ICMP bursts require spacing >=100ms")
	}
	if p.Method == MethodUDP && (p.PacketsPerCycle > 3 || p.PacketSpacingMS < 1000 || p.PayloadBytes < 28 || p.PayloadBytes > 300) {
		return errors.New("UDP echo requires 1..3 packets, spacing >=1000ms and 28..300 byte payload")
	}
	if p.Method == MethodUDP && t.Port != 7770 {
		return errors.New("GameLift UDP echo requires beacon port 7770")
	}
	if p.Method != MethodICMP && p.Method != MethodUDP && p.PacketsPerCycle != 1 {
		return errors.New("transport and application probes use one network attempt per cycle")
	}
	if p.Method != MethodICMP && (t.Port < 1 || t.Port > 65535) {
		return errors.New("this probe method requires a port in 1..65535")
	}
	if p.PayloadBytes < 0 || p.PayloadBytes > 300 {
		return errors.New("payload is outside supported bounds")
	}
	if p.Method == MethodICMP && p.PayloadBytes < 16 {
		return errors.New("ICMP payload must contain at least 16 bytes")
	}
	if p.Method == MethodHTTPS {
		if t.Scheme != "https" || !strings.HasPrefix(t.Path, "/") || len(t.Path) > 2048 || strings.ContainsAny(t.Path, "\r\n") {
			return errors.New("HTTPS requires https scheme and a bounded absolute path")
		}
		if p.HTTPMethod != "GET" && p.HTTPMethod != "HEAD" {
			return errors.New("HTTPS method must be GET or HEAD")
		}
		if len(p.ExpectedStatusCodes) < 1 || len(p.ExpectedStatusCodes) > 10 {
			return errors.New("expected HTTP statuses are required")
		}
		for _, status := range p.ExpectedStatusCodes {
			if status < 100 || status > 599 {
				return fmt.Errorf("invalid expected HTTP status %d", status)
			}
		}
	}
	if p.Method == MethodDNS {
		if t.Port != 53 || len(p.DNSExpectedAnswers) == 0 {
			return errors.New("DNS requires resolver port 53 and explicit expected answers")
		}
		if p.DNSQueryName == "" || len(p.DNSQueryName) > 253 || strings.ContainsAny(p.DNSQueryName, " /:@\\\r\n") {
			return errors.New("DNS query name is invalid")
		}
		if p.DNSQueryType != "A" && p.DNSQueryType != "AAAA" {
			return errors.New("DNS query type must be A or AAAA")
		}
		if len(p.DNSExpectedAnswers) > 20 {
			return errors.New("too many expected DNS answers")
		}
		for _, answer := range p.DNSExpectedAnswers {
			ip, err := netip.ParseAddr(answer)
			if err != nil || ip.Zone() != "" || ip.Is4In6() || ip.Is4() != (p.DNSQueryType == "A") {
				return errors.New("expected DNS answers must be IP addresses")
			}
		}
	}
	return nil
}
