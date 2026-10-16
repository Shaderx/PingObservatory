// Package probe performs bounded native IPv4 ICMP probes without invoking a
// shell or changing host privileges or security settings.
package probe

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

const maxProbeTimeout = 30 * time.Second

type ipv4Lookup func(context.Context, string, string) ([]net.IP, error)
type nativeFunc func(context.Context, netip.Addr) model.Measurement

// Probe starts one IPv4 probe operation. DNS resolution and ICMP share the
// timeout budget, which is capped at 30 seconds. At records the start of that
// operation, including explicit DNS, permission, or platform failures. Work
// cancelled before completion has no timestamp and must not become a loss
// sample. An OK result includes ICMP round-trip latency only, excluding DNS.
// TargetID and TargetRevision are supplied by the collector.
func Probe(ctx context.Context, host string, timeout time.Duration) model.Measurement {
	resolver := &net.Resolver{PreferGo: true}
	return probeWith(ctx, host, timeout, resolver.LookupIP, nativeProbe)
}

func probeWith(ctx context.Context, host string, timeout time.Duration, lookup ipv4Lookup, native nativeFunc) model.Measurement {
	if err := ctx.Err(); err != nil {
		return failure(model.StatusError, "probe cancelled before start")
	}
	started := time.Now().UTC()
	finish := func(m model.Measurement) model.Measurement {
		if ctx.Err() == context.Canceled {
			return failure(model.StatusError, "probe cancelled")
		}
		m.At = started
		return m
	}
	if timeout <= 0 {
		return finish(failure(model.StatusError, "probe timeout must be positive"))
	}
	if timeout > maxProbeTimeout {
		timeout = maxProbeTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 {
		return finish(failure(model.StatusDNS, "invalid IPv4 host"))
	}
	addr, err := netip.ParseAddr(host)
	if err == nil {
		addr = addr.Unmap()
		if !addr.Is4() {
			return finish(failure(model.StatusUnsupported, "IPv6 probing is not supported in this MVP"))
		}
	} else {
		ips, err := lookup(bounded, "ip4", host)
		if err != nil {
			return finish(failure(model.StatusDNS, "IPv4 resolution failed: "+err.Error()))
		}
		for _, ip := range ips {
			if candidate, ok := netip.AddrFromSlice(ip); ok && candidate.Unmap().Is4() {
				addr = candidate.Unmap()
				break
			}
		}
		if !addr.IsValid() {
			return finish(failure(model.StatusDNS, "host has no IPv4 address"))
		}
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr == netip.MustParseAddr("255.255.255.255") {
		return finish(failure(model.StatusError, "probe requires a unicast IPv4 destination"))
	}
	if err := bounded.Err(); err != nil {
		return finish(failure(model.StatusError, "probe cancelled before ICMP send"))
	}
	m := native(bounded, addr)
	if m.Sent > 0 && !m.Discarded {
		m.DestinationIP = addr.String()
	}
	return finish(m)
}

func failure(status, detail string) model.Measurement {
	return model.Measurement{Status: status, Detail: detail, UnableToMeasure: 1}
}

func icmpAfterSend(m model.Measurement) model.Measurement {
	m.Sent, m.FailureStage = 1, "response"
	m.UnobservedSent = 1
	if m.Status == model.StatusTimeout || m.Status == model.StatusUnreachable {
		m.UnableToMeasure, m.UnobservedSent, m.HasObservation = 0, 0, true
	}
	return m
}
