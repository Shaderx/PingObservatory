package probe

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func fixtureAddress(family string) netip.Addr {
	if family == model.FamilyIPv6 {
		return netip.MustParseAddr("::1")
	}
	return netip.MustParseAddr("127.0.0.1")
}

func fixtureSettings(method, family string) model.ProbeSettings {
	return model.ProbeSettings{Method: method, Families: []string{family}, IntervalSeconds: 60, PacketsPerCycle: 1, PacketSpacingMS: 1000, TimeoutMS: 300, PayloadBytes: 32}
}

func udpFixture(t *testing.T, addr netip.Addr, respond func([]byte) [][]byte) (int, func()) {
	t.Helper()
	conn, err := net.ListenUDP(networkFor("udp", addr), net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, 0)))
	if err != nil {
		t.Fatalf("UDP loopback fixture: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 1024)
		for {
			n, peer, err := conn.ReadFromUDPAddrPort(buffer)
			if err != nil {
				return
			}
			for _, reply := range respond(append([]byte(nil), buffer[:n]...)) {
				_, _ = conn.WriteToUDPAddrPort(reply, peer)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, func() { _ = conn.Close(); <-done }
}

func TestTCPUDPAndDNSLoopbackFamilies(t *testing.T) {
	for _, family := range []string{model.FamilyIPv4, model.FamilyIPv6} {
		t.Run(family, func(t *testing.T) {
			addr := fixtureAddress(family)
			listener, err := net.Listen(networkFor("tcp", addr), endpoint(addr, 0))
			if err != nil {
				t.Fatalf("TCP loopback fixture: %v", err)
			}
			defer listener.Close()
			go func() {
				for {
					c, err := listener.Accept()
					if err != nil {
						return
					}
					c.Close()
				}
			}()
			target := model.Target{Host: addr.String(), Port: listener.Addr().(*net.TCPAddr).Port, Measurement: fixtureSettings(model.MethodTCP, family)}
			m := ProbeTarget(context.Background(), target, family)
			if m.Status != model.StatusOK || m.Sent != 1 || m.Received != 1 || m.Attempted != 1 || m.UnableToMeasure != 0 || len(m.Samples) != 1 || m.At != m.BatchEndedAt || m.BatchStartedAt.After(m.At) {
				t.Fatalf("TCP measurement = %+v", m)
			}
			udpPort, closeUDP := udpFixture(t, addr, func(packet []byte) [][]byte { return [][]byte{packet} })
			defer closeUDP()
			p := fixtureSettings(model.MethodUDP, family)
			m = probeUDP(context.Background(), addr, udpPort, p)
			if m.Status != model.StatusOK || m.Sent != 1 || m.Received != 1 || m.UnobservedSent != 0 || len(m.Samples) != 1 {
				t.Fatalf("UDP measurement = %+v", m)
			}
			for _, qtype := range []string{"A", "AAAA"} {
				answer := fixtureAddress(model.FamilyIPv4)
				if qtype == "AAAA" {
					answer = fixtureAddress(model.FamilyIPv6)
				}
				dnsPort, closeDNS := udpFixture(t, addr, func(query []byte) [][]byte { return [][]byte{dnsFixtureAnswer(query, answer)} })
				p = fixtureSettings(model.MethodDNS, family)
				p.DNSQueryName, p.DNSQueryType, p.DNSExpectedAnswers = "fixture.example", qtype, []string{answer.String()}
				m = probeDNS(context.Background(), addr, dnsPort, p)
				closeDNS()
				if m.Status != model.StatusOK || m.Sent != 1 || m.Received != 1 || m.UnableToMeasure != 0 || len(m.Samples) != 1 {
					t.Fatalf("DNS %s/%s = %+v", family, qtype, m)
				}
			}
		})
	}
}

func dnsFixtureAnswer(query []byte, answer netip.Addr) []byte {
	packet := append([]byte(nil), query...)
	packet[2], packet[3], packet[7] = 0x81, 0x80, 1
	typeCode := byte(1)
	if answer.Is6() {
		typeCode = 28
	}
	data := answer.AsSlice()
	packet = append(packet, 0xc0, 0x0c, 0, typeCode, 0, 1, 0, 0, 0, 60, 0, byte(len(data)))
	return append(packet, data...)
}

func TestUDPBurstValidatesFramingAndCountsRealTimeout(t *testing.T) {
	addr := fixtureAddress(model.FamilyIPv4)
	var received []time.Time
	port, closeFixture := udpFixture(t, addr, func(packet []byte) [][]byte {
		received = append(received, time.Now())
		seq := binary.BigEndian.Uint64(packet[20:28])
		if seq == 2 {
			return nil
		}
		corrupt := append([]byte(nil), packet...)
		corrupt[len(corrupt)-1] ^= 1
		return [][]byte{corrupt, packet, packet}
	})
	p := fixtureSettings(model.MethodUDP, model.FamilyIPv4)
	p.PacketsPerCycle, p.TimeoutMS = 3, 100
	m := probeUDP(context.Background(), addr, port, p)
	closeFixture()
	if m.Sent != 3 || m.Received != 2 || m.Attempted != 3 || m.UnobservedSent != 0 || m.UnableToMeasure != 0 || m.Status != model.StatusDegraded || len(m.Samples) != 2 || m.Samples[0].Sequence != 1 || m.Samples[1].Sequence != 3 {
		t.Fatalf("UDP burst = %+v", m)
	}
	if len(received) != 3 || received[1].Sub(received[0]) < 950*time.Millisecond || received[2].Sub(received[1]) < 950*time.Millisecond {
		t.Fatalf("UDP burst ignored spacing: %v", received)
	}
}

func TestDNSValidationRejectsUnexpectedAndMalformedAnswers(t *testing.T) {
	query, err := dnsQuestion("fixture.example", "A", 123)
	if err != nil {
		t.Fatal(err)
	}
	answer := dnsFixtureAnswer(query, netip.MustParseAddr("127.0.0.1"))
	if ok, err := dnsAnswerMatches(answer, "fixture.example", "A", []string{"127.0.0.2"}); err != nil || ok {
		t.Fatalf("unexpected answer accepted: %v %v", ok, err)
	}
	truncated := append([]byte(nil), answer...)
	truncated[2] |= 2
	if ok, err := dnsAnswerMatches(truncated, "fixture.example", "A", []string{"127.0.0.1"}); err != nil || ok {
		t.Fatalf("truncated response accepted")
	}
	for i := 0; i < len(answer); i++ {
		if ok, _ := dnsAnswerMatches(answer[:i], "fixture.example", "A", []string{"127.0.0.1"}); ok {
			t.Fatalf("truncated packet length %d accepted", i)
		}
	}
	if _, _, err := dnsName([]byte{0xc0, 0}, 0); err == nil {
		t.Fatal("DNS compression loop accepted")
	}
}

func tlsFixture(t *testing.T, family string, handler http.Handler) (*httptest.Server, model.Target, *x509.CertPool) {
	t.Helper()
	addr := fixtureAddress(family)
	listener, err := net.Listen(networkFor("tcp", addr), endpoint(addr, 0))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener.Close()
	server.Listener = listener
	server.StartTLS()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	p := fixtureSettings(model.MethodHTTPS, family)
	p.HTTPMethod, p.ExpectedStatusCodes, p.TimeoutMS = "GET", []int{200}, 500
	target := model.Target{Host: addr.String(), Port: listener.Addr().(*net.TCPAddr).Port, Scheme: "https", Path: "/", Measurement: p}
	return server, target, roots
}

func TestHTTPSFamiliesPreserveHostTrustAndCompletionTiming(t *testing.T) {
	for _, family := range []string{model.FamilyIPv4, model.FamilyIPv6} {
		t.Run(family, func(t *testing.T) {
			host := fixtureAddress(family).String()
			server, target, roots := tlsFixture(t, family, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != host {
					t.Errorf("Host=%s want=%s", r.Host, host)
				}
				w.Header().Set("Cf-Ray", "discard-this-request-id-LHR")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				time.Sleep(80 * time.Millisecond)
				_, _ = io.WriteString(w, "fixture")
			}))
			defer server.Close()
			m := probeHTTPS(context.Background(), target, fixtureAddress(family), target.Measurement, roots)
			if m.Status != model.StatusOK || m.Received != 1 || m.Sent != 1 || m.POPHint != "LHR" || len(m.Samples) != 1 || m.Samples[0].LatencyMS < 75 {
				t.Fatalf("HTTPS measurement = %+v", m)
			}
			// With no injected fixture root, the same server must fail trust.
			untrusted := probeHTTPS(context.Background(), target, fixtureAddress(family), target.Measurement, nil)
			if untrusted.Status == model.StatusOK || untrusted.Received != 0 || untrusted.FailureStage != "tls" {
				t.Fatalf("untrusted TLS accepted: %+v", untrusted)
			}
		})
	}
}

func TestHTTPSRedirectBlockedAndBodyFailureAreMeasured(t *testing.T) {
	for _, status := range []int{302, 403, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			requests := 0
			server, target, roots := tlsFixture(t, model.FamilyIPv4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Location", "https://127.0.0.1/redirect")
				w.WriteHeader(status)
			}))
			defer server.Close()
			m := probeHTTPS(context.Background(), target, fixtureAddress(model.FamilyIPv4), target.Measurement, roots)
			if m.Status != model.StatusDegraded || m.Sent != 1 || m.Received != 0 || !m.HasObservation || m.HTTPStatus != status || requests != 1 {
				t.Fatalf("status response = %+v requests=%d", m, requests)
			}
		})
	}
	server, target, roots := tlsFixture(t, model.FamilyIPv4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "short")
	}))
	defer server.Close()
	m := probeHTTPS(context.Background(), target, fixtureAddress(model.FamilyIPv4), target.Measurement, roots)
	if m.Status != model.StatusDegraded || !m.HasObservation || m.Sent != 1 || m.Received != 0 || m.UnableToMeasure != 0 {
		t.Fatalf("truncated body = %+v", m)
	}
}

func TestTargetResolutionAdmissionAndSuspendDoNotCreateLoss(t *testing.T) {
	target := model.Target{Host: "fixture.example", Port: 443, Measurement: fixtureSettings(model.MethodTCP, model.FamilyIPv4)}
	m := probeTargetWith(context.Background(), target, model.FamilyIPv4, func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("fixture DNS failure")
	}, time.Now)
	if m.Status != model.StatusDNS || m.Sent != 0 || m.HasObservation || m.UnableToMeasure != 1 {
		t.Fatalf("DNS inability = %+v", m)
	}
	target.Admission = map[string]model.Admission{model.FamilyIPv4: {State: "verified"}}
	m = probeTargetWith(context.Background(), target, model.FamilyIPv4, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}, time.Now)
	if m.Sent != 0 || m.UnableToMeasure != 1 || !strings.Contains(m.Detail, "non-public") {
		t.Fatalf("catalogue mixed answer set = %+v", m)
	}
	target.Admission[model.FamilyIPv4] = model.Admission{State: "unsupported"}
	m = ProbeTarget(context.Background(), target, model.FamilyIPv4)
	if m.Status != model.StatusUnsupported || m.Sent != 0 || m.UnableToMeasure != 1 {
		t.Fatalf("unsupported admission = %+v", m)
	}
	for _, times := range [][2]time.Duration{{10 * time.Second, time.Second}, {time.Second, 10 * time.Second}, {10 * time.Second, 10 * time.Second}, {-time.Second, time.Second}} {
		if reliableElapsed(times[0], times[1], time.Second) {
			t.Fatalf("interrupted elapsed accepted: %v", times)
		}
	}
	if !reliableElapsed(time.Second, time.Second, time.Second) {
		t.Fatal("normal bounded elapsed rejected")
	}
	partial := summarize(model.Measurement{Status: model.StatusError, Sent: 3, Received: 1, UnobservedSent: 1, UnableToMeasure: 1, Samples: []model.RTTSample{{At: time.Now().UTC(), LatencyMS: 1, Sequence: 1}}})
	if !partial.HasObservation || partial.Sent-partial.UnobservedSent != 2 || len(partial.Samples) != 1 || partial.LatencyMS == nil {
		t.Fatalf("partial result dropped: %+v", partial)
	}
	discarded := discardMeasurement(partial, "fixture suspend")
	if !discarded.Discarded || discarded.HasObservation || discarded.Sent != 0 || len(discarded.Samples) != 0 {
		t.Fatalf("suspend retained packet loss: %+v", discarded)
	}
	permission := operationFailure(syscall.EACCES, 0, "connect")
	if permission.Sent != 0 || permission.HasObservation || permission.UnableToMeasure != 1 {
		t.Fatalf("permission became loss: %+v", permission)
	}
}

func TestNativeTargetLoopbackFamilies(t *testing.T) {
	if os.Getenv("PINGOBSERVATORY_NATIVE_TEST") != "1" {
		t.Skip("set PINGOBSERVATORY_NATIVE_TEST=1 for native ICMP4/6 loopback")
	}
	for _, family := range []string{model.FamilyIPv4, model.FamilyIPv6} {
		t.Run(family, func(t *testing.T) {
			p := fixtureSettings(model.MethodICMP, family)
			p.PacketsPerCycle, p.PacketSpacingMS, p.TimeoutMS = 3, 100, 100
			m := ProbeTarget(context.Background(), model.Target{Host: fixtureAddress(family).String(), Measurement: p}, family)
			if m.Status != model.StatusOK || m.Sent != 3 || m.Received != 3 || len(m.Samples) != 3 || m.UnableToMeasure != 0 || !m.HasObservation {
				t.Fatalf("native %s = %+v", family, m)
			}
			t.Logf("native %s burst: sent=%d received=%d median_ms=%.3f", family, m.Sent, m.Received, *m.LatencyMS)
		})
	}
}

func TestActiveProbeClockDiscontinuityDiscardsOutcomes(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{}, 2)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
			accepted <- struct{}{}
		}
	}()
	for _, jump := range []time.Duration{10 * time.Minute, -time.Hour} {
		start := time.Now()
		calls := 0
		now := func() time.Time {
			calls++
			if calls >= 4 {
				return start.Add(jump)
			}
			return start.Add(time.Duration(calls) * time.Millisecond)
		}
		target := model.Target{Host: "fixture.example", Port: listener.Addr().(*net.TCPAddr).Port, Measurement: fixtureSettings(model.MethodTCP, model.FamilyIPv4)}
		m := probeTargetWith(context.Background(), target, model.FamilyIPv4, func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{fixtureAddress(model.FamilyIPv4)}, nil
		}, now)
		select {
		case <-accepted:
		case <-time.After(time.Second):
			t.Fatal("fixture probe did not actually run")
		}
		if !m.Discarded || m.Status != model.StatusSuspended || m.HasObservation || m.Sent != 0 || m.Received != 0 || len(m.Samples) != 0 {
			t.Fatalf("clock jump retained outcomes: %+v", m)
		}
	}
}

func TestICMPBurstPreservesSpacingWithOverlappingResponses(t *testing.T) {
	p := fixtureSettings(model.MethodICMP, model.FamilyIPv4)
	p.PacketsPerCycle, p.PacketSpacingMS, p.TimeoutMS = 3, 100, 1000
	var active, maximum atomic.Int32
	m := probeICMPWith(context.Background(), fixtureAddress(model.FamilyIPv4), p, func(ctx context.Context, _ netip.Addr, _ int) model.Measurement {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); current > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return failure(model.StatusUnavailable, ctx.Err().Error())
		case <-time.After(250 * time.Millisecond):
		}
		ms := 250.0
		return model.Measurement{Status: model.StatusOK, Sent: 1, Received: 1, HasObservation: true, LatencyMS: &ms}
	})
	if maximum.Load() < 2 || m.Sent != 3 || m.Received != 3 || len(m.Samples) != 3 || m.Status != model.StatusOK {
		t.Fatalf("ICMP spacing serialized or lost responses: max=%d measurement=%+v", maximum.Load(), m)
	}
}

func TestLocalErrorsAfterSendHaveNoInventedLoss(t *testing.T) {
	for _, err := range []error{syscall.EACCES, syscall.EAFNOSUPPORT, syscall.ENETUNREACH, errors.New("local fixture transport error")} {
		m := operationFailure(err, 1, "response")
		if m.Sent != 1 || m.UnobservedSent != 1 || m.HasObservation || m.UnableToMeasure != 1 {
			t.Fatalf("unknown outcome entered denominator: %+v", m)
		}
	}
	for _, err := range []error{syscall.ECONNREFUSED, context.DeadlineExceeded} {
		m := operationFailure(err, 1, "response")
		if m.Sent != 1 || m.UnobservedSent != 0 || !m.HasObservation {
			t.Fatalf("measured network outcome was lost: %+v", m)
		}
	}
}

func TestCatalogueAndNativePublicDestinationBoundaryAgree(t *testing.T) {
	for _, text := range []string{"1.1.1.1", "2606:4700:4700::1111", "100.64.0.1", "192.0.2.1", "2001:db8::1", "127.0.0.1", "fe80::1"} {
		addr := netip.MustParseAddr(text)
		family := model.FamilyIPv6
		if addr.Is4() {
			family = model.FamilyIPv4
		}
		pinned, err := pinTargetAddress(context.Background(), text, family, nil, true)
		if got := err == nil && pinned.IsValid(); got != model.PublicUnicast(addr) {
			t.Fatalf("catalogue/native boundary differs for %s", text)
		}
	}
}
