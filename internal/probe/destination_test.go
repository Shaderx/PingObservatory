package probe

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
	"github.com/Shaderx/PingObservatory/internal/storage"
)

func TestActualDestinationChangesUseOnlyThePinnedAttempt(t *testing.T) {
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port
	second, err := net.Listen("tcp4", endpoint(netip.MustParseAddr("127.0.0.2"), port))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, listener := range []net.Listener{first, second} {
		go func(listener net.Listener) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}(listener)
	}
	s, err := storage.Open(filepath.Join(t.TempDir(), "observations.sqlite"), 30)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	target := model.Target{ID: "manual-destination-fixture", Revision: 1, Host: "stable.fixture.example", Port: port, Measurement: fixtureSettings(model.MethodTCP, model.FamilyIPv4)}
	lookups := 0
	for i, ip := range []string{"127.0.0.1", "127.0.0.2", "127.0.0.1"} {
		m := probeTargetWith(context.Background(), target, model.FamilyIPv4, func(_ context.Context, network, host string) ([]netip.Addr, error) {
			lookups++
			if network != "ip4" || host != target.Host {
				t.Fatal("wrong resolution identity")
			}
			return []netip.Addr{netip.MustParseAddr(ip)}, nil
		}, time.Now)
		if m.DestinationIP != ip || m.Status != model.StatusOK || m.Received != 1 || lookups != i+1 {
			t.Fatal("captured address differs from actual attempt or a second lookup occurred", m, lookups)
		}
		m.TargetID, m.TargetRevision = target.ID, target.Revision
		if err := s.Add(m); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Statistics([]model.Target{target}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	state := stats[0].Observation.Destination
	if state == nil || state.BaselineIP != "127.0.0.1" || state.LastIP != "127.0.0.1" || state.ChangeCount != 2 || len(state.History) != 2 || state.History[0].NewIP != "127.0.0.2" || stats[0].Observation.Status != model.StatusOK {
		t.Fatal("real round-robin attempts lost informational changes", stats)
	}
}

func TestUnsentAndDiscardedOperationsDoNotClaimDestinations(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	for _, result := range []model.Measurement{{Status: model.StatusPermission, UnableToMeasure: 1}, {Status: model.StatusDNS, UnableToMeasure: 1}, {Sent: 1, Discarded: true}} {
		if actual := captureDestination(result, ip); actual.DestinationIP != "" {
			t.Fatal("no actual network attempt", actual)
		}
	}
	attempt := captureDestination(model.Measurement{Sent: 1, Status: model.StatusTimeout}, ip)
	if attempt.DestinationIP != ip.String() {
		t.Fatal("timeout lost known attempted address")
	}
	if discarded := discardMeasurement(attempt, "fixture clock jump"); discarded.DestinationIP != "" {
		t.Fatal("discarded cycle retained destination")
	}
}
