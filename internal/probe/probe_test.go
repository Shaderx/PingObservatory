package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func noLookup(t *testing.T) ipv4Lookup {
	return func(context.Context, string, string) ([]net.IP, error) {
		t.Fatal("unexpected DNS lookup")
		return nil, nil
	}
}

func noNative(t *testing.T) nativeFunc {
	return func(context.Context, netip.Addr) model.Measurement {
		t.Fatal("unexpected ICMP operation")
		return model.Measurement{}
	}
}

func TestLiteralUsesBoundedNativeProbe(t *testing.T) {
	before := time.Now()
	latency := 1.25
	m := probeWith(context.Background(), "127.0.0.1", 250*time.Millisecond, noLookup(t), func(ctx context.Context, addr netip.Addr) model.Measurement {
		if addr.String() != "127.0.0.1" {
			t.Fatalf("destination = %s", addr)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 250*time.Millisecond {
			t.Fatalf("missing or invalid timeout budget: %v", deadline)
		}
		return model.Measurement{Status: model.StatusOK, LatencyMS: &latency}
	})
	if m.Status != model.StatusOK || m.LatencyMS == nil || *m.LatencyMS != latency || m.At.Before(before) || m.At.After(time.Now()) {
		t.Fatalf("unexpected measurement: %+v", m)
	}
}

func TestTimeoutIsCapped(t *testing.T) {
	m := probeWith(context.Background(), "127.0.0.1", time.Hour, noLookup(t), func(ctx context.Context, _ netip.Addr) model.Measurement {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > maxProbeTimeout {
			t.Fatalf("unbounded deadline: %v", deadline)
		}
		return failure(model.StatusPermission, "permission test")
	})
	if m.Status != model.StatusPermission || m.At.IsZero() || m.LatencyMS != nil {
		t.Fatalf("permission failure was not retained: %+v", m)
	}
}

func TestDNSAndICMPShareBudget(t *testing.T) {
	var resolutionDeadline time.Time
	lookup := func(ctx context.Context, network, host string) ([]net.IP, error) {
		if network != "ip4" || host != "test.example" {
			t.Fatalf("unexpected lookup: %s %s", network, host)
		}
		resolutionDeadline, _ = ctx.Deadline()
		return []net.IP{net.ParseIP("::1"), net.ParseIP("127.0.0.1")}, nil
	}
	m := probeWith(context.Background(), "test.example", time.Second, lookup, func(ctx context.Context, addr netip.Addr) model.Measurement {
		deadline, _ := ctx.Deadline()
		if !deadline.Equal(resolutionDeadline) || addr.String() != "127.0.0.1" {
			t.Fatalf("DNS and native budgets/destination differ")
		}
		return failure(model.StatusTimeout, "timed out")
	})
	if m.Status != model.StatusTimeout || m.At.IsZero() || m.LatencyMS != nil {
		t.Fatalf("unexpected attempted failure: %+v", m)
	}
}

func TestDNSFailuresAreNotLoss(t *testing.T) {
	tests := []struct {
		name string
		ips  []net.IP
		err  error
	}{
		{"lookup error", nil, errors.New("resolver unavailable")},
		{"IPv6 only", []net.IP{net.ParseIP("::1")}, nil},
		{"empty result", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := probeWith(context.Background(), "test.example", time.Second, func(context.Context, string, string) ([]net.IP, error) {
				return tt.ips, tt.err
			}, noNative(t))
			if m.Status != model.StatusDNS || m.At.IsZero() || m.LatencyMS != nil {
				t.Fatalf("DNS failure = %+v", m)
			}
		})
	}
}

func TestResolutionHonorsDeadline(t *testing.T) {
	start := time.Now()
	m := probeWith(context.Background(), "test.example", 10*time.Millisecond, func(ctx context.Context, _, _ string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, noNative(t))
	if m.Status != model.StatusDNS || m.At.IsZero() || time.Since(start) > time.Second {
		t.Fatalf("unbounded DNS operation: %+v", m)
	}
}

func TestCancelledWorkHasNoTimestamp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := probeWith(ctx, "127.0.0.1", time.Second, noLookup(t), noNative(t))
	if !m.At.IsZero() || m.LatencyMS != nil {
		t.Fatalf("cancelled work was recorded as a measurement: %+v", m)
	}
	ctx, cancel = context.WithCancel(context.Background())
	m = probeWith(ctx, "test.example", time.Second, func(context.Context, string, string) ([]net.IP, error) {
		cancel()
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}, noNative(t))
	if !m.At.IsZero() {
		t.Fatalf("cancelled DNS work was recorded: %+v", m)
	}
}

func TestUnsupportedAndInvalidDestinationsDoNotSend(t *testing.T) {
	for _, host := range []string{"::1", "0.0.0.0", "224.0.0.1", "255.255.255.255", ""} {
		t.Run(host, func(t *testing.T) {
			m := probeWith(context.Background(), host, time.Second, noLookup(t), noNative(t))
			if m.Status == model.StatusOK || m.At.IsZero() || m.Detail == "" {
				t.Fatalf("invalid destination = %+v", m)
			}
		})
	}
	m := probeWith(context.Background(), "127.0.0.1", 0, noLookup(t), noNative(t))
	if m.Status != model.StatusError || m.At.IsZero() {
		t.Fatalf("invalid timeout = %+v", m)
	}
}

// This is the only real network probe in the test suite, and targets loopback.
func TestNativeLoopback(t *testing.T) {
	if os.Getenv("PINGOBSERVATORY_NATIVE_TEST") != "1" {
		t.Skip("set PINGOBSERVATORY_NATIVE_TEST=1 to test native loopback ICMP")
	}
	m := Probe(context.Background(), "127.0.0.1", time.Second)
	if m.Status != model.StatusOK || m.LatencyMS == nil || *m.LatencyMS < 0 || m.At.IsZero() {
		t.Fatalf("native loopback = %+v", m)
	}
	t.Logf("native loopback: status=%s latency_ms=%.3f", m.Status, *m.LatencyMS)
}
