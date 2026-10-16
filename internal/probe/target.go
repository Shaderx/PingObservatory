package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

// ProbeTarget runs one bounded method/family cycle. Every transport uses the
// same pinned address and never falls back to a different family. Packet loss
// accounting is meaningful only for ICMP and UDP; other methods count network
// operations. UnableToMeasure is separate from a sent operation that failed.
func ProbeTarget(ctx context.Context, target model.Target, family string) model.Measurement {
	return probeTargetWith(ctx, target, family, (&net.Resolver{PreferGo: true}).LookupNetIP, time.Now)
}

func probeTargetWith(ctx context.Context, t model.Target, family string, lookup func(context.Context, string, string) ([]netip.Addr, error), now func() time.Time) model.Measurement {
	p := model.Settings(t)
	started := now()
	m := model.Measurement{Method: p.Method, Family: family, At: started.UTC(), BatchStartedAt: started.UTC(), Status: model.StatusError}
	budget := time.Duration(p.TimeoutMS)*time.Millisecond*2 + time.Duration(p.PacketsPerCycle-1)*time.Duration(p.PacketSpacingMS)*time.Millisecond + time.Second
	finish := func(result model.Measurement) model.Measurement {
		ended := now()
		result.Method, result.Family = p.Method, family
		result.At, result.BatchStartedAt, result.BatchEndedAt = ended.UTC(), started.UTC(), ended.UTC()
		if ctx.Err() == context.Canceled || !reliableElapsed(ended.UTC().Sub(started.UTC()), ended.Sub(started), budget) {
			return discardMeasurement(result, "probe interrupted by cancellation, sleep, or a clock discontinuity")
		}
		return result
	}
	unable := func(status, stage, detail string) model.Measurement {
		return finish(model.Measurement{Status: status, FailureStage: stage, Detail: detail, UnableToMeasure: 1})
	}
	if ctx.Err() != nil {
		m.At, m.BatchStartedAt = time.Time{}, time.Time{}
		return discardMeasurement(m, "probe cancelled before start")
	}
	if err := model.ValidateProbeTarget(t); err != nil {
		return unable(model.StatusUnsupported, "configure", err.Error())
	}
	if budget > maxProbeTimeout {
		return unable(model.StatusUnsupported, "configure", "configured burst and resolution budget exceed the 30-second cycle limit")
	}
	declared := false
	for _, f := range p.Families {
		declared = declared || f == family
	}
	if !declared || family != model.FamilyIPv4 && family != model.FamilyIPv6 {
		return unable(model.StatusUnsupported, "configure", "requested address family is not declared")
	}
	if admission, exists := t.Admission[family]; exists && admission.State != "verified" {
		return unable(model.StatusUnsupported, "configure", "target family is not admitted for collection: "+admission.State)
	}
	if p.Method == model.MethodUDP && t.Port != awsBeaconPort {
		return unable(model.StatusUnsupported, "configure", "GameLift UDP echo requires beacon port 7770")
	}
	if p.Method == model.MethodDNS && (t.Port != 53 || len(p.DNSExpectedAnswers) == 0) {
		return unable(model.StatusUnsupported, "configure", "DNS requires resolver port 53 and explicit expected answers")
	}
	if p.Method == model.MethodICMP && p.PacketsPerCycle > 1 && p.PacketSpacingMS < 100 {
		return unable(model.StatusUnsupported, "configure", "ICMP bursts require spacing >=100ms")
	}
	bounded, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	resolveStarted := now()
	resolveCtx, resolveCancel := context.WithTimeout(bounded, time.Duration(p.TimeoutMS)*time.Millisecond)
	addr, err := pinTargetAddress(resolveCtx, t.Host, family, lookup, len(t.Admission) > 0)
	resolveCancel()
	resolvedMS := float64(now().Sub(resolveStarted)) / float64(time.Millisecond)
	if err != nil {
		return unable(model.StatusDNS, "resolve", err.Error())
	}
	if !addr.IsValid() {
		return unable(model.StatusUnsupported, "resolve", "host has no address for the requested family")
	}
	switch p.Method {
	case model.MethodICMP:
		m = probeICMP(bounded, addr, p)
	case model.MethodUDP:
		m = probeUDP(bounded, addr, t.Port, p)
	case model.MethodTCP:
		m = probeTCP(bounded, addr, t.Port, p)
	case model.MethodHTTPS:
		m = probeHTTPS(bounded, t, addr, p, nil)
	case model.MethodDNS:
		m = probeDNS(bounded, addr, t.Port, p)
	}
	if m.Timings == nil {
		m.Timings = map[string]float64{}
	}
	// Resolver timing is distinct from the selected protocol's duration.
	m.Timings["resolve"] = resolvedMS
	return finish(m)
}

func pinAddress(ctx context.Context, host, family string, lookup func(context.Context, string, string) ([]netip.Addr, error)) (netip.Addr, error) {
	return pinTargetAddress(ctx, host, family, lookup, false)
}

func pinTargetAddress(ctx context.Context, host, family string, lookup func(context.Context, string, string) ([]netip.Addr, error), public bool) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsMulticast() || ip == netip.MustParseAddr("255.255.255.255") || ip.Is4() != (family == model.FamilyIPv4) {
			return netip.Addr{}, nil
		}
		if public && !publicUnicast(ip) {
			return netip.Addr{}, errors.New("catalogue destination must be public unicast")
		}
		return ip, nil
	}
	network := "ip4"
	if family == model.FamilyIPv6 {
		network = "ip6"
	}
	answers, err := lookup(ctx, network, host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, addr := range answers {
		if public && !publicUnicast(addr) {
			return netip.Addr{}, errors.New("catalogue DNS answer set contains a non-public destination")
		}
		if !addr.IsValid() || addr.Zone() != "" || addr.Is4In6() || addr.IsUnspecified() || addr.IsMulticast() {
			return netip.Addr{}, nil
		}
	}
	for _, addr := range answers {
		if addr.Is4() == (family == model.FamilyIPv4) {
			return addr, nil
		}
	}
	return netip.Addr{}, nil
}

var publicExcludedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
}

func publicUnicast(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" || addr.Is4In6() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, prefix := range publicExcludedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func reliableElapsed(wall, elapsed, budget time.Duration) bool {
	gap := wall - elapsed
	if gap < 0 {
		gap = -gap
	}
	// On systems where monotonic time stops during sleep, the wall/monotonic
	// gap detects it. Where monotonic time advances, a bounded call returning
	// substantially after its budget is equally unsuitable for loss accounting.
	return wall >= 0 && elapsed >= 0 && gap <= 500*time.Millisecond && wall <= budget+time.Second && elapsed <= budget+time.Second
}

func discardMeasurement(m model.Measurement, detail string) model.Measurement {
	m.Status, m.Detail, m.Discarded = model.StatusSuspended, detail, true
	m.Sent, m.Received, m.Attempted = 0, 0, 0
	m.UnobservedSent = 0
	m.UnableToMeasure, m.HasObservation = 1, false
	m.Samples, m.LatencyMS = nil, nil
	return m
}

func probeICMP(ctx context.Context, addr netip.Addr, p model.ProbeSettings) model.Measurement {
	native := nativeProbePayload
	if addr.Is6() {
		native = nativeProbe6Payload
	}
	return probeICMPWith(ctx, addr, p, native)
}

// Native calls overlap when their timeout exceeds the configured send spacing.
// The bounded result channel preserves each completed response independently.
func probeICMPWith(ctx context.Context, addr netip.Addr, p model.ProbeSettings, native func(context.Context, netip.Addr, int) model.Measurement) model.Measurement {
	type outcome struct {
		sequence int
		ended    time.Time
		result   model.Measurement
	}
	outcomes := make(chan outcome, p.PacketsPerCycle)
	started := time.Now()
	launched := 0
launch:
	for sequence := 1; sequence <= p.PacketsPerCycle; sequence++ {
		wait := time.Until(started.Add(time.Duration(sequence-1) * time.Duration(p.PacketSpacingMS) * time.Millisecond))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				break launch
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			break
		}
		launched++
		go func(sequence int) {
			call, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
			defer cancel()
			result := native(call, addr, p.PayloadBytes)
			ended := time.Now()
			if ctx.Err() != nil && result.Sent > result.Received {
				result.UnobservedSent, result.UnableToMeasure = result.Sent-result.Received, 1
			}
			outcomes <- outcome{sequence, ended, result}
		}(sequence)
	}
	m := model.Measurement{Status: model.StatusTimeout}
	for i := 0; i < launched; i++ {
		completed := <-outcomes
		result := completed.result
		m.Attempted++
		m.Sent += result.Sent
		m.UnobservedSent += result.UnobservedSent
		m.Received += result.Received
		m.UnableToMeasure += result.UnableToMeasure
		if result.UnableToMeasure > 0 || m.UnableToMeasure == 0 {
			m.Status, m.Detail, m.FailureStage = result.Status, result.Detail, result.FailureStage
		}
		if result.Status == model.StatusOK && result.LatencyMS != nil {
			m.Samples = append(m.Samples, model.RTTSample{At: completed.ended.UTC(), LatencyMS: *result.LatencyMS, Sequence: completed.sequence})
		}
	}
	sort.Slice(m.Samples, func(i, j int) bool { return m.Samples[i].Sequence < m.Samples[j].Sequence })
	if ctx.Err() == context.Canceled {
		return discardMeasurement(m, "ICMP cycle interrupted")
	}
	if launched < p.PacketsPerCycle {
		return interruptedMeasurement(ctx, m, 0, "ICMP cycle interrupted")
	}
	return summarize(m)
}
func interruptedMeasurement(ctx context.Context, m model.Measurement, pending int, detail string) model.Measurement {
	if ctx.Err() == context.Canceled {
		return discardMeasurement(m, detail)
	}
	m.Status, m.Detail, m.UnableToMeasure = model.StatusUnavailable, detail, m.UnableToMeasure+1
	m.UnobservedSent += pending
	return summarize(m)
}

func summarize(m model.Measurement) model.Measurement {
	eligible := m.Sent - m.UnobservedSent
	m.HasObservation = eligible > 0 && !m.Discarded
	if len(m.Samples) > 0 {
		latencies := make([]float64, len(m.Samples))
		for i, s := range m.Samples {
			latencies[i] = s.LatencyMS
		}
		sort.Float64s(latencies)
		median := latencies[len(latencies)/2]
		if len(latencies)%2 == 0 {
			median = (latencies[len(latencies)/2-1] + median) / 2
		}
		m.LatencyMS = &median
	}
	if m.UnableToMeasure == 0 {
		if eligible > 0 && m.Received == eligible {
			m.Status = model.StatusOK
		} else if m.Received > 0 {
			m.Status = model.StatusDegraded
		}
	} else if eligible > 0 {
		m.Status = model.StatusDegraded
	}
	return m
}
