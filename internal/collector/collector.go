package collector

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

type Status struct {
	StartedAt    time.Time `json:"started_at"`
	LastError    string    `json:"last_error,omitempty"`
	ActiveProbes int       `json:"active_probes"`
}

type Collector struct {
	Targets  func() []model.Target
	Probe    func(context.Context, string, time.Duration) model.Measurement
	Record   func(model.Measurement) error
	Interval time.Duration
	Timeout  time.Duration
	Wake     chan struct{}
	mu       sync.Mutex
	status   Status
}

func (c *Collector) Snapshot() Status  { c.mu.Lock(); defer c.mu.Unlock(); return c.status }
func (c *Collector) SetError(s string) { c.mu.Lock(); defer c.mu.Unlock(); c.status.LastError = s }

// Next selects a future slot from the current wall-clock cycle. Missed slots
// are never enqueued. Sorting makes each target's slot stable within a cycle.
func Next(now time.Time, targets []model.Target, interval time.Duration) (model.Target, time.Time, bool) {
	if len(targets) == 0 || interval <= 0 {
		return model.Target{}, time.Time{}, false
	}
	targets = append([]model.Target(nil), targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	n := now.UnixNano()
	base := n - n%int64(interval)
	for i, t := range targets {
		slot := base + int64(interval)*int64(i)/int64(len(targets))
		if slot > n {
			return t, time.Unix(0, slot), true
		}
	}
	return targets[0], time.Unix(0, base+int64(interval)), true
}

func (c *Collector) Run(ctx context.Context) {
	interval := c.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	c.mu.Lock()
	c.status.StartedAt = time.Now().UTC()
	c.mu.Unlock()
	sem := make(chan struct{}, 4)
	var workers sync.WaitGroup
	defer workers.Wait()
	last := map[string]int64{}
	for {
		t, due, ok := Next(time.Now(), c.Targets(), interval)
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-c.Wake:
				continue
			case <-time.After(time.Second):
				continue
			}
		}
		timer := time.NewTimer(time.Until(due))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.Wake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		now := time.Now()
		// Compare wall time explicitly: monotonic clocks can pause during sleep.
		if now.UnixNano()-due.UnixNano() > int64(2*time.Second) {
			continue
		}
		key := t.ID
		cycle := due.UnixNano() / int64(interval)
		if last[key] >= cycle {
			continue
		}
		if len(last) > 160 {
			last = map[string]int64{}
		}
		select {
		case sem <- struct{}{}:
		default:
			c.SetError("probe concurrency limit reached; skipped slot is N/A")
			continue
		}
		last[key] = cycle
		workers.Add(1)
		go func(t model.Target) {
			defer workers.Done()
			defer func() { <-sem }()
			c.mu.Lock()
			c.status.ActiveProbes++
			c.mu.Unlock()
			defer func() { c.mu.Lock(); c.status.ActiveProbes--; c.mu.Unlock() }()
			m := c.Probe(ctx, t.Host, timeout)
			if ctx.Err() != nil || m.At.IsZero() {
				return
			}
			m.TargetID = t.ID
			m.TargetRevision = t.Revision
			if err := c.Record(m); err != nil {
				c.SetError(err.Error())
			}
		}(t)
	}
}
