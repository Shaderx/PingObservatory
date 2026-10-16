package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

var fixtureNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := fixtureNow
	s, e := openStore(filepath.Join(t.TempDir(), "observations.sqlite"), 30, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, &now
}
func batch(id string, at time.Time, values []float64, sent int) model.Measurement {
	m := model.Measurement{TargetID: id, TargetRevision: 1, Method: model.MethodICMP, Family: model.FamilyIPv4, At: at, BatchStartedAt: at.Add(-4 * time.Second), BatchEndedAt: at, Attempted: sent, Sent: sent, Received: len(values), HasObservation: true, Status: model.StatusOK}
	if len(values) == 0 {
		m.Status = model.StatusTimeout
	} else if len(values) < sent {
		m.Status = model.StatusDegraded
	}
	for i, v := range values {
		m.Samples = append(m.Samples, model.RTTSample{At: at.Add(-time.Duration(len(values)-i-1) * 100 * time.Millisecond), LatencyMS: v, Sequence: i + 1})
	}
	if len(values) > 0 {
		v := quantile(values, .5)
		m.LatencyMS = &v
	}
	return m
}
func target(id string) model.Target {
	return model.Target{ID: id, Revision: 1, Measurement: model.ProbeSettings{Method: model.MethodICMP, Families: []string{model.FamilyIPv4}}}
}
func add(t *testing.T, s *Store, m model.Measurement) {
	t.Helper()
	if e := s.Add(m); e != nil {
		t.Fatal(e)
	}
}
func near(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("got %v want %.12g", got, want)
	}
}
func rowCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if e := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestPersistenceMissingVersusFailuresAndSeries(t *testing.T) {
	s, now := fixture(t)
	at := now.Add(-time.Hour)
	add(t, s, batch("target", at.Add(10*time.Second), []float64{12.345678901}, 1))
	add(t, s, batch("target", at.Add(time.Minute+10*time.Second), nil, 1))
	local := batch("target", at.Add(2*time.Minute+10*time.Second), nil, 0)
	local.Status = model.StatusDNS
	local.HasObservation = false
	local.UnableToMeasure = 1
	add(t, s, local)
	v6 := batch("target", at.Add(10*time.Second), []float64{99}, 1)
	v6.Family = model.FamilyIPv6
	add(t, s, v6)
	revised := batch("target", at.Add(10*time.Second), []float64{123}, 1)
	revised.TargetRevision = 2
	add(t, s, revised)
	path := s.path
	s.Close()
	reopened, e := openStore(path, 30, func() time.Time { return *now })
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	points, e := reopened.Buckets("target", 1, at.Add(-time.Minute), at.Add(4*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"na", model.StatusOK, model.StatusTimeout, model.StatusDNS, "na"}
	for i, status := range want {
		if points[i].Status != status {
			t.Fatalf("point%d: %+v", i, points[i])
		}
		if (points[i].Measurement == nil) != (status == "na") {
			t.Fatal("absence represented as a probe")
		}
	}
	near(t, points[1].MedianMS, 12.345678901)
	near(t, points[2].LossPct, 100)
	if points[3].LossPct != nil || points[3].MedianMS != nil || points[3].UnableToMeasure != 1 {
		t.Fatal("DNS/local error entered loss statistics")
	}
	other, e := reopened.History("target", 1, model.MethodICMP, model.FamilyIPv6, "24h", *now)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, b := range other.Buckets {
		if b.MedianMS != nil {
			near(t, b.MedianMS, 99)
			found = true
		}
	}
	if !found {
		t.Fatal("IPv6 series lost")
	}
	latest, e := reopened.Latest()
	if e != nil || len(latest) != 3 {
		t.Fatalf("latest series/revisions %v %+v", e, latest)
	}
}

func TestMissingAndExpiredStatisticsAreGaps(t *testing.T) {
	s, now := fixture(t)
	add(t, s, batch("expired", now.Add(-time.Minute), []float64{10}, 1))
	*now = now.Add(31 * 24 * time.Hour)
	stats, err := s.Statistics([]model.Target{target("never-observed"), target("expired")}, *now)
	if err != nil {
		t.Fatal(err)
	}
	for _, series := range stats {
		o := series.Observation
		if o.Status != "na" || o.ObservedAt != nil || o.Sent != 0 || o.Received != 0 || o.UnableToMeasure != 0 || o.MedianMS != nil || o.LossPct != nil {
			t.Fatalf("missing local observation became a failure: %+v", o)
		}
	}
}

func TestExactStatisticsEligibilityAndBaseline(t *testing.T) {
	s, now := fixture(t)
	add(t, s, batch("target", now.Add(-30*time.Minute+10*time.Second), []float64{1, 2, 3}, 3))
	add(t, s, batch("target", now.Add(-20*time.Minute+10*time.Second), []float64{4, 100}, 3))
	partial := batch("target", now.Add(-10*time.Minute+10*time.Second), []float64{5}, 2)
	partial.Attempted = 3
	partial.UnobservedSent = 1
	partial.UnableToMeasure = 1
	add(t, s, partial)
	unknown := batch("target", now.Add(-5*time.Minute+10*time.Second), nil, 1)
	unknown.HasObservation = false
	unknown.UnobservedSent = 1
	unknown.UnableToMeasure = 1
	unknown.Status = model.StatusError
	add(t, s, unknown)
	discarded := batch("target", now.Add(-4*time.Minute+10*time.Second), []float64{9999}, 1)
	discarded.Discarded = true
	discarded.Status = model.StatusSuspended
	add(t, s, discarded)
	local := batch("target", now.Add(-time.Minute+10*time.Second), nil, 0)
	local.HasObservation = false
	local.UnableToMeasure = 1
	local.Status = model.StatusPermission
	add(t, s, local)
	stats, e := s.Statistics([]model.Target{target("target")}, *now)
	if e != nil {
		t.Fatal(e)
	}
	o := stats[0].Observation
	if o.MedianMS != nil || o.Status != model.StatusPermission {
		t.Fatalf("latest failure invented current RTT: %+v", o)
	}
	if o.Rolling1hSent != 9 || o.Rolling1hUnobservedSent != 2 || o.Rolling1hReceived != 6 {
		t.Fatalf("partial/local/discarded denominator: %+v", o)
	}
	near(t, o.LossPct, 100./7)
	near(t, o.P95MS, 76.25)
	near(t, o.BaselineMS, 3.5)
	near(t, o.StddevMS, stddev([]float64{1, 2, 3, 4, 5, 100}))
	if o.FailurePct != nil || o.BaselineSampleCount != 6 || o.LastSuccessAt == nil || o.RollingAsOf == nil {
		t.Fatalf("incorrect statistics metadata: %+v", o)
	}
	zero := batch("zero", now.Add(-10*time.Minute), []float64{0, 0, 1, 2, 3, 4}, 6)
	add(t, s, zero)
	stats, e = s.Statistics([]model.Target{target("zero")}, *now)
	if e != nil {
		t.Fatal(e)
	}
	if stats[0].Observation.BaselineSampleCount != 6 {
		t.Fatal("valid zero-duration successes were excluded")
	}
	near(t, stats[0].Observation.BaselineMS, 1.5)
	add(t, s, batch("zero", now.Add(-time.Minute), []float64{5}, 1))
	stats, _ = s.Statistics([]model.Target{target("zero")}, *now)
	near(t, stats[0].Observation.BaselineMS, 2)
	add(t, s, batch("mixed-zero", now.Add(-time.Minute), []float64{0, 0, 0, 0, 0, 10, 10, 20, 30, 40}, 10))
	stats, _ = s.Statistics([]model.Target{target("mixed-zero")}, *now)
	near(t, stats[0].Observation.BaselineMS, 5)
	add(t, s, batch("all-zero", now.Add(-time.Minute), []float64{0, 0, 0, 0, 0}, 5))
	stats, _ = s.Statistics([]model.Target{target("all-zero")}, *now)
	if stats[0].Observation.BaselineMS != nil || stats[0].Observation.BaselineSampleCount != 5 {
		t.Fatal("a zero baseline must remain unusable without excluding valid successes")
	}
}

func TestDiscardedClockReversalIsAnOperationalRecordOnly(t *testing.T) {
	s, now := fixture(t)
	m := model.Measurement{TargetID: "clock-jump", TargetRevision: 1, At: now.Add(-time.Minute), BatchStartedAt: now.Add(time.Hour), BatchEndedAt: now.Add(-time.Minute), Method: model.MethodICMP, Family: model.FamilyIPv4, Status: model.StatusSuspended, Discarded: true, UnableToMeasure: 1}
	add(t, s, m)
	h, err := s.History("clock-jump", 1, model.MethodICMP, model.FamilyIPv4, "1h", *now)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range h.Buckets {
		if b.Status != "na" || b.Sent != 0 || b.MedianMS != nil || b.LossPct != nil {
			t.Fatal("discarded clock reversal contributed measurement statistics")
		}
	}
}

func TestNonPacketFailureRateAndUnsentNull(t *testing.T) {
	s, now := fixture(t)
	for i := 0; i < 2; i++ {
		m := batch("tcp", now.Add(time.Duration(-2+i)*time.Minute), nil, 1)
		m.Method = model.MethodTCP
		if i == 0 {
			m = batch("tcp", m.At, []float64{20}, 1)
			m.Method = model.MethodTCP
		}
		add(t, s, m)
	}
	tg := target("tcp")
	tg.Measurement.Method = model.MethodTCP
	stats, e := s.Statistics([]model.Target{tg}, *now)
	if e != nil {
		t.Fatal(e)
	}
	near(t, stats[0].Observation.FailurePct, 50)
	if stats[0].Observation.LossPct != nil {
		t.Fatal("TCP attempts labelled packet loss")
	}
	m := batch("unsent", now.Add(-time.Minute), nil, 1)
	m.UnobservedSent = 1
	m.HasObservation = false
	m.UnableToMeasure = 1
	add(t, s, m)
	stats, e = s.Statistics([]model.Target{target("unsent")}, *now)
	if e != nil {
		t.Fatal(e)
	}
	o := stats[0].Observation
	if o.Rolling1hSent != 1 || o.Rolling1hUnobservedSent != 1 || o.LossPct != nil || o.MedianMS != nil {
		t.Fatalf("unobserved send became loss: %+v", o)
	}
}

func TestLosslessArchivedUnevenQuantiles(t *testing.T) {
	s, now := fixture(t)
	base := now.Add(-50 * time.Hour)
	first := batch("target", base.Add(time.Minute), []float64{1000}, 1)
	second := batch("target", base.Add(40*time.Minute), []float64{0, 1, 2, 3, 4, 5, 6, 7, 8}, 9)
	add(t, s, first)
	add(t, s, second)
	if rowCount(t, s, "raw") != 0 || rowCount(t, s, "chunks") != 2 {
		t.Fatal("older batches were not compacted")
	}
	h, e := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "30d", *now)
	if e != nil {
		t.Fatal(e)
	}
	index := int(base.Sub(h.Start) / (2 * time.Hour))
	b := h.Buckets[index]
	near(t, b.MedianMS, 4.5)
	near(t, b.P95MS, 553.6)
	near(t, b.StddevMS, stddev([]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 1000}))
	near(t, b.LossPct, 0)
	if math.Abs(*b.MedianMS-(1000+4)/2.) < 1 {
		t.Fatal("combined median was averaged from bucket medians")
	}
	if math.Abs(*b.P95MS-(1000+7.6)/2.) < 1 {
		t.Fatal("combined percentile was averaged from bucket percentiles")
	}
	data, e := encodeMeasurement(first)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := decodeMeasurement(data)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(first.Samples, decoded.Samples) {
		t.Fatal("codec altered timing values/timestamps/sequences")
	}
}

func TestRolloverRetentionAndCompletedGrids(t *testing.T) {
	s, now := fixture(t)
	at := now.Add(-25*time.Hour - 50*time.Minute + 10*time.Second)
	add(t, s, batch("target", at, []float64{12.123456789}, 1))
	if rowCount(t, s, "raw") != 1 {
		t.Fatal("recent26h not raw")
	}
	*now = now.Add(20 * time.Minute)
	add(t, s, batch("new", now.Add(-10*time.Second), []float64{20}, 1))
	if rowCount(t, s, "chunks") != 1 {
		t.Fatal("raw expiry lost original batch instead of archiving")
	}
	for window, w := range historyWindows {
		h, e := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, window, now.Add(23*time.Second))
		if e != nil {
			t.Fatal(e)
		}
		if len(h.Buckets) != w.count || h.StepSeconds != int(w.step/time.Second) || !h.End.Equal(now.Truncate(w.step)) {
			t.Fatalf("%s wrong grid %+v", window, h)
		}
	}
	h, e := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "7d", *now)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, b := range h.Buckets {
		if b.MedianMS != nil {
			near(t, b.MedianMS, 12.123456789)
			found = true
		}
	}
	if !found {
		t.Fatal("archived timing unavailable")
	}
	if e = s.SetRetention(1); e != nil {
		t.Fatal(e)
	}
	h, e = s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "7d", *now)
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range h.Buckets {
		if b.MedianMS != nil {
			t.Fatal("expired history remains visible")
		}
	}
	if !h.RetainedFrom.Equal(now.Add(-24*time.Hour)) || !h.Buckets[0].RetentionExpired || h.Buckets[len(h.Buckets)-1].RetentionExpired {
		t.Fatal("retention-expired coverage was not distinguished from ordinary gaps")
	}
}

func TestMaintenanceReopenIsBoundedResumableAndLossless(t *testing.T) {
	s, now := fixture(t)
	base := now.Add(-25 * time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	const count = archiveBatch * 4
	add(t, s, batch("target", base, []float64{1}, 1))
	var id int64
	if err := s.db.QueryRow("SELECT id FROM series WHERE target='target'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < count; i++ {
		m := batch("target", base.Add(time.Duration(i)*time.Minute), []float64{float64(i + 1)}, 1)
		data, err := encodeMeasurement(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO raw(series_id,minute,data) VALUES(?,?,?)", id, m.At.Unix()/60, data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(8 * time.Hour)
	path := s.path
	s.Close()
	s, err = openStore(path, 30, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if rowCount(t, s, "raw") != count {
		t.Fatal("opening the profile drained pending maintenance")
	}
	pending, err := s.MaintenanceStep()
	if err != nil || !pending || rowCount(t, s, "raw") != count-archiveBatch {
		t.Fatalf("unbounded first step: pending=%v err=%v", pending, err)
	}
	// A newer record in a not-yet-archived minute replaces the raw record
	// atomically, even while the backlog is only partly processed.
	replacement := batch("target", base.Add((count-1)*time.Minute+5*time.Second), []float64{1000}, 1)
	add(t, s, replacement)
	check := func() {
		h, err := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "7d", *now)
		if err != nil {
			t.Fatal(err)
		}
		sent, received := 0, 0
		for _, b := range h.Buckets {
			sent += b.Sent
			received += b.Received
		}
		if sent != count || received != count {
			t.Fatalf("cleanup altered or duplicated history: %d/%d", sent, received)
		}
	}
	check()
	s.Close()
	s, err = openStore(path, 30, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	for steps := 0; ; steps++ {
		if steps > 10 {
			t.Fatal("maintenance did not resume to completion")
		}
		pending, err = s.MaintenanceStep()
		if err != nil {
			t.Fatal(err)
		}
		check()
		if !pending {
			break
		}
	}
	if rowCount(t, s, "raw") != 0 {
		t.Fatal("raw archive backlog remains")
	}
	if err = s.SetRetention(1); err != nil {
		t.Fatal(err)
	}
	if rowCount(t, s, "chunks") == 0 {
		t.Fatal("retention update synchronously drained history")
	}
	h, err := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "7d", *now)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range h.Buckets {
		if b.Received != 0 || b.MedianMS != nil {
			t.Fatal("expired data exposed while cleanup pending")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	if rowCount(t, s, "chunks") == 0 {
		t.Fatal("cancelled worker drained history")
	}
	for steps := 0; ; steps++ {
		if steps > 10 {
			t.Fatal("expired cleanup did not complete")
		}
		pending, err = s.MaintenanceStep()
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			break
		}
	}
	if rowCount(t, s, "chunks") != 0 || rowCount(t, s, "series") != 0 {
		t.Fatal("expired rows or identities retained after cleanup")
	}
}

func TestRTTUsesOriginalTimestampAcrossCompletedBoundary(t *testing.T) {
	s, now := fixture(t)
	*now = now.Add(10 * time.Second)
	m := batch("target", now.Truncate(time.Minute).Add(time.Second), []float64{7}, 1)
	m.Samples[0].At = m.At.Add(-2 * time.Second)
	add(t, s, m)
	h, e := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "1h", *now)
	if e != nil {
		t.Fatal(e)
	}
	near(t, h.Buckets[59].MedianMS, 7)
	if h.Buckets[59].Sent != 0 || h.Buckets[59].LossPct != nil {
		t.Fatal("packet counts fabricated in earlier RTT bucket")
	}
}

func TestExclusiveLockBoundsErrorsAndCapacity(t *testing.T) {
	s, now := fixture(t)
	started := time.Now()
	other, e := Open(s.path, 30)
	if e == nil {
		other.Close()
		t.Fatal("second collector acquired profile")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("exclusive lock timeout unbounded")
	}
	add(t, s, batch("preserved", now.Add(-time.Minute), []float64{1}, 1))
	var pages int
	if e = s.db.QueryRow("PRAGMA page_count").Scan(&pages); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(fmt.Sprintf("PRAGMA max_page_count=%d", pages)); e != nil {
		t.Fatal(e)
	}
	failed := false
	for i := 0; i < 100; i++ {
		m := batch(fmt.Sprintf("long-target-%d", i), now.Add(-time.Minute), []float64{2}, 1)
		m.Detail = strings.Repeat("x", 512)
		e = s.Add(m)
		if errors.Is(e, ErrCapacity) {
			failed = true
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	if !failed {
		t.Fatal("capacity failure did not surface")
	}
	points, e := s.Buckets("preserved", 1, now.Add(-time.Minute), *now)
	if e != nil {
		t.Fatal(e)
	}
	near(t, points[0].MedianMS, 1)
	if _, e = s.Buckets("target", 1, *now, now.Add(1441*time.Minute)); !errors.Is(e, ErrQueryTooLarge) {
		t.Fatal("unbounded query accepted")
	}
	if e = s.SetRetention(0); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid retention accepted")
	}
	bad := batch("target", *now, []float64{1}, 1)
	bad.UnobservedSent = 1
	if e = s.Add(bad); !errors.Is(e, ErrInvalid) {
		t.Fatal("received exceeds eligible sends accepted")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Add(batch("target", *now, []float64{1}, 1)); !errors.Is(e, ErrClosed) {
		t.Fatal("write after close accepted")
	}
}

func TestRuntimePragmasAndCorruption(t *testing.T) {
	s, now := fixture(t)
	for pragma, want := range map[string]int{"cache_size": -8192, "mmap_size": 0, "auto_vacuum": 2, "temp_store": 2, "max_page_count": 1048576} {
		var n int
		if e := s.db.QueryRow("PRAGMA " + pragma).Scan(&n); e != nil || n != want {
			t.Fatalf("%s %d %v", pragma, n, e)
		}
	}
	if s.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("unbounded connection pool")
	}
	add(t, s, batch("target", now.Add(-time.Minute), []float64{1}, 1))
	if _, e := s.db.Exec("UPDATE raw SET data=x'ff'"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.History("target", 1, model.MethodICMP, model.FamilyIPv4, "1h", *now); e == nil {
		t.Fatal("corrupt record silently ignored")
	}
	path := filepath.Join(t.TempDir(), "original.db")
	original := []byte("old database retained")
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	if st, e := Open(path, 30); e == nil {
		st.Close()
		t.Fatal("unrelated file accepted")
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(original, after) {
		t.Fatal("unrelated database file changed")
	}
}

func TestConcurrentOperationsAndMinuteReplacement(t *testing.T) {
	s, now := fixture(t)
	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- s.Add(batch(fmt.Sprintf("target%d", i), now.Add(-time.Minute), []float64{float64(i)}, 1))
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatal(e)
		}
	}
	newer := batch("target0", now.Add(-time.Minute+20*time.Second), []float64{20}, 1)
	add(t, s, newer)
	add(t, s, batch("target0", now.Add(-time.Minute+10*time.Second), []float64{99}, 1))
	if rowCount(t, s, "raw") != 10 {
		t.Fatal("minute replacement duplicated history")
	}
	latest, e := s.Latest()
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range latest {
		if m.TargetID == "target0" {
			near(t, m.LatencyMS, 20)
		}
	}
}
