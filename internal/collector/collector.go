package collector

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

type Status struct {
	StartedAt       time.Time `json:"started_at"`
	LastError       string    `json:"last_error,omitempty"`
	ActiveProbes    int       `json:"active_probes"`
	SkippedSlots    int       `json:"skipped_slots"`
	ScheduledSeries int       `json:"scheduled_series"`
	MaxConcurrent   int       `json:"max_concurrent"`
	Enabled         bool      `json:"enabled"`
}

type Job struct {
	Target model.Target
	Family string
}

func (j Job) Key() string {
	return j.Target.ID + "/" + j.Family + "/" + model.Settings(j.Target).Method
}

type Collector struct {
	Targets     func() []model.Target
	Probe       func(context.Context, string, time.Duration) model.Measurement
	Jobs        func() []Job
	ProbeTarget func(context.Context, model.Target, string) model.Measurement
	Record      func(model.Measurement) error
	Interval    time.Duration
	Timeout     time.Duration
	Wake        chan struct{}
	mu          sync.Mutex
	status      Status
}

func NextJob(now time.Time, jobs []Job, interval time.Duration) (Job, time.Time, bool) {
	if len(jobs) == 0 || interval <= 0 {
		return Job{}, time.Time{}, false
	}
	jobs = append([]Job(nil), jobs...)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Key() < jobs[j].Key() })
	n := now.UnixNano()
	base := n - n%int64(interval)
	for i, j := range jobs {
		slot := base + int64(interval)*int64(i)/int64(len(jobs))
		if slot > n {
			return j, time.Unix(0, slot), true
		}
	}
	return jobs[0], time.Unix(0, base+int64(interval)), true
}

func (c *Collector) jobs() []Job {
	if c.Jobs != nil {
		return c.Jobs()
	}
	var jobs []Job
	if c.Targets != nil {
		for _, t := range c.Targets() {
			for _, f := range model.Settings(t).Families {
				jobs = append(jobs, Job{t, f})
			}
		}
	}
	return jobs
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
	c.status.Enabled = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.status.Enabled = false; c.mu.Unlock() }()
	const maxConcurrent = 64
	sem := make(chan struct{}, maxConcurrent)
	var workers sync.WaitGroup
	defer workers.Wait()
	last := map[string]int64{}
	running := map[string]bool{}
	var runningMu sync.Mutex
	for {
		jobs := c.jobs()
		c.mu.Lock()
		c.status.ScheduledSeries = len(jobs)
		c.status.MaxConcurrent = maxConcurrent
		c.mu.Unlock()
		job, due, ok := NextJob(time.Now(), jobs, interval)
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
		key := job.Key()
		cycle := due.UnixNano() / int64(interval)
		if last[key] >= cycle {
			continue
		}
		if len(last) > 2048 {
			active := map[string]bool{}
			for _, j := range jobs {
				active[j.Key()] = true
			}
			for k := range last {
				if !active[k] {
					delete(last, k)
				}
			}
		}
		runningMu.Lock()
		busy := running[key]
		if !busy {
			running[key] = true
		}
		runningMu.Unlock()
		if busy {
			c.mu.Lock()
			c.status.SkippedSlots++
			c.status.LastError = "previous batch is still active; skipped slot is N/A"
			c.mu.Unlock()
			continue
		}
		select {
		case sem <- struct{}{}:
		default:
			runningMu.Lock()
			delete(running, key)
			runningMu.Unlock()
			c.mu.Lock()
			c.status.SkippedSlots++
			c.status.LastError = "probe capacity limit reached; skipped slot is N/A"
			c.mu.Unlock()
			continue
		}
		last[key] = cycle
		workers.Add(1)
		go func(job Job, key string) {
			defer workers.Done()
			defer func() { <-sem }()
			defer func() { runningMu.Lock(); delete(running, key); runningMu.Unlock() }()
			c.mu.Lock()
			c.status.ActiveProbes++
			c.mu.Unlock()
			defer func() { c.mu.Lock(); c.status.ActiveProbes--; c.mu.Unlock() }()
			var m model.Measurement
			if c.ProbeTarget != nil {
				m = c.ProbeTarget(ctx, job.Target, job.Family)
			} else {
				m = c.Probe(ctx, job.Target.Host, timeout)
			}
			if ctx.Err() != nil || m.At.IsZero() {
				return
			}
			m.TargetID = job.Target.ID
			m.TargetRevision = job.Target.Revision
			m.Method = model.Settings(job.Target).Method
			m.Family = job.Family
			if err := c.Record(m); err != nil {
				c.SetError(err.Error())
			}
		}(job, key)
	}
}
