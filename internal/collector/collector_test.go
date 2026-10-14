package collector

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestNextStaggersAndSkipsMissingCycles(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	targets := []model.Target{{ID: "b"}, {ID: "a"}, {ID: "c"}}
	target, due, ok := Next(base.Add(time.Second), targets, time.Minute)
	if !ok || target.ID != "b" || !due.Equal(base.Add(20*time.Second)) {
		t.Fatalf("unexpected slot %s %s", target.ID, due)
	}
	now := base.Add(17*time.Minute + 45*time.Second)
	target, due, ok = Next(now, targets, time.Minute)
	if !ok || target.ID != "a" || !due.Equal(base.Add(18*time.Minute)) {
		t.Fatalf("missed cycles replayed: %s %s", target.ID, due)
	}
	if targets[0].ID != "b" {
		t.Fatal("input targets mutated")
	}
}

func TestRunCollectsWithoutBrowserAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var records atomic.Int32
	c := &Collector{Interval: 30 * time.Millisecond, Wake: make(chan struct{}, 1), Targets: func() []model.Target { return []model.Target{{ID: "manual-local", Revision: 2, Host: "127.0.0.1"}} }, Probe: func(ctx context.Context, host string, timeout time.Duration) model.Measurement {
		return model.Measurement{At: time.Now(), Status: model.StatusTimeout}
	}, Record: func(m model.Measurement) error {
		if m.TargetID != "manual-local" || m.TargetRevision != 2 {
			t.Error("measurement identity missing")
		}
		records.Add(1)
		return nil
	}}
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.After(2 * time.Second)
	for records.Load() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("collector failed to run independently")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
	if c.Snapshot().ActiveProbes != 0 {
		t.Fatal("active workers remain after stop")
	}
}

func TestCancelledWorkDoesNotCreateMeasurements(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var records atomic.Int32
	c := &Collector{Interval: 20 * time.Millisecond, Targets: func() []model.Target { return []model.Target{{ID: "x"}} }, Probe: func(context.Context, string, time.Duration) model.Measurement {
		cancel()
		return model.Measurement{At: time.Now(), Status: model.StatusTimeout}
	}, Record: func(model.Measurement) error { records.Add(1); return nil }}
	c.Run(ctx)
	if records.Load() != 0 {
		t.Fatal("cancelled work stored as an attempt")
	}
}
