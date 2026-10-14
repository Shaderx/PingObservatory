package storage

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
	bolt "go.etcd.io/bbolt"
)

var testNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func testLimits() limits {
	return limits{rows: MaxRows, diskBytes: MaxDatabaseBytes, headroom: 4 << 20, compactMinBytes: 4 << 20}
}

func newTestStore(t *testing.T, days int) (*Store, *time.Time) {
	t.Helper()
	now := testNow
	s, err := openStore(filepath.Join(t.TempDir(), "history.db"), days, func() time.Time { return now }, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, &now
}

func measurement(id string, revision uint64, at time.Time, status string) model.Measurement {
	m := model.Measurement{TargetID: id, TargetRevision: revision, At: at, Status: status}
	if status == model.StatusOK {
		latency := 12.5
		m.LatencyMS = &latency
	}
	return m
}

func mustAdd(t *testing.T, s *Store, m model.Measurement) {
	t.Helper()
	if err := s.Add(m); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, s *Store) uint64 {
	t.Helper()
	var count uint64
	if err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		count, err = rowCount(tx)
		if err == nil && int(count) != tx.Bucket(rowsBucket).Stats().KeyN {
			return errors.New("row counter differs from stored rows")
		}
		if err == nil && int(count) != tx.Bucket(timeBucket).Stats().KeyN {
			return errors.New("time index differs from stored rows")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPersistenceGapsFailuresAndRevisionHistory(t *testing.T) {
	s, now := newTestStore(t, 7)
	base := now.Add(-time.Hour)
	m := measurement("test-target", 1, base.Add(18*time.Second).In(time.FixedZone("test", 3600)), model.StatusOK)
	m.Detail = "reply received"
	mustAdd(t, s, m)
	mustAdd(t, s, measurement("test-target", 1, base.Add(time.Minute), model.StatusTimeout))
	mustAdd(t, s, measurement("test-target", 2, base, model.StatusDNS))
	path := s.path
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(path, 7, func() time.Time { return *now }, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	buckets, err := reopened.Buckets("test-target", 1, base.Add(-time.Minute), base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"na", model.StatusOK, model.StatusTimeout, "na"}
	for i, status := range want {
		if buckets[i].Status != status {
			t.Fatalf("bucket %d: got %q, want %q", i, buckets[i].Status, status)
		}
		if (buckets[i].Measurement == nil) != (status == "na") {
			t.Fatalf("bucket %d: incorrect absence marker", i)
		}
		if buckets[i].Minute.Location() != time.UTC {
			t.Fatalf("bucket %d is not UTC", i)
		}
	}
	if got := buckets[1].Measurement; got.Detail != m.Detail || *got.LatencyMS != *m.LatencyMS || !got.At.Equal(m.At) || got.At.Location() != time.UTC {
		t.Fatalf("measurement did not survive reopen: %+v", got)
	}
	revision2, err := reopened.Buckets("test-target", 2, base, base.Add(time.Minute))
	if err != nil || revision2[0].Status != model.StatusDNS {
		t.Fatalf("revision history was merged: %v %+v", err, revision2)
	}
	latest, err := reopened.Latest()
	if err != nil || len(latest) != 2 {
		t.Fatalf("latest: %v %+v", err, latest)
	}
	if latest[0].TargetRevision != 1 || latest[0].Status != model.StatusTimeout || latest[1].TargetRevision != 2 || latest[1].Status != model.StatusDNS {
		t.Fatalf("latest does not preserve revisions: %+v", latest)
	}
}

func TestNewestAttemptPerMinute(t *testing.T) {
	s, now := newTestStore(t, 7)
	base := now.Add(-time.Hour)
	mustAdd(t, s, measurement("target", 1, base.Add(40*time.Second), model.StatusTimeout))
	mustAdd(t, s, measurement("target", 1, base.Add(10*time.Second), model.StatusOK))
	latest, _ := s.Latest()
	if len(latest) != 1 || latest[0].Status != model.StatusTimeout {
		t.Fatalf("older attempt replaced latest: %+v", latest)
	}
	mustAdd(t, s, measurement("target", 1, base.Add(50*time.Second), model.StatusOK))
	latest, _ = s.Latest()
	if len(latest) != 1 || latest[0].Status != model.StatusOK || countRows(t, s) != 1 {
		t.Fatalf("minute replacement duplicated rows: %+v", latest)
	}
}

func TestRetentionChangesAndExpiredRows(t *testing.T) {
	s, now := newTestStore(t, 7)
	old := now.Add(-5 * 24 * time.Hour)
	recent := now.Add(-12 * time.Hour)
	mustAdd(t, s, measurement("old", 1, old, model.StatusTimeout))
	mustAdd(t, s, measurement("recent", 1, recent, model.StatusOK))
	if err := s.SetRetention(1); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s) != 1 {
		t.Fatal("retention change did not prune expired rows")
	}
	buckets, err := s.Buckets("old", 1, old, old.Add(time.Minute))
	if err != nil || buckets[0].Status != "na" || buckets[0].Measurement != nil {
		t.Fatalf("expired row is not absent: %v %+v", err, buckets)
	}
	mustAdd(t, s, measurement("old", 1, old, model.StatusDNS))
	if countRows(t, s) != 1 {
		t.Fatal("expired import was retained")
	}
	*now = now.Add(2 * 24 * time.Hour)
	latest, err := s.Latest()
	if err != nil || len(latest) != 0 {
		t.Fatalf("read returned expired history: %v %+v", err, latest)
	}
	mustAdd(t, s, measurement("new", 1, *now, model.StatusOK))
	if countRows(t, s) != 1 {
		t.Fatal("write did not prune expired rows")
	}
}

func TestRowLimitPrunesOldestAndLatestIndex(t *testing.T) {
	s, now := newTestStore(t, 7)
	s.limits.rows = 3
	base := now.Add(-time.Hour)
	for i := 0; i < 4; i++ {
		mustAdd(t, s, measurement(fmt.Sprintf("target-%d", i), 1, base.Add(time.Duration(i)*time.Minute), model.StatusOK))
	}
	if countRows(t, s) != 3 {
		t.Fatal("row cap was not enforced")
	}
	latest, err := s.Latest()
	if err != nil || len(latest) != 3 {
		t.Fatalf("pruned latest entry survived: %v %+v", err, latest)
	}
	buckets, err := s.Buckets("target-0", 1, base, base.Add(time.Minute))
	if err != nil || buckets[0].Status != "na" {
		t.Fatalf("oldest row was not pruned: %v %+v", err, buckets)
	}
}

func TestInputAndWindowBounds(t *testing.T) {
	s, now := newTestStore(t, 0)
	if s.retention != 7*24*time.Hour {
		t.Fatal("incorrect default retention")
	}
	valid := measurement("target", 1, *now, model.StatusOK)
	invalid := []model.Measurement{valid, valid, valid, valid, valid, valid, valid, valid, valid, valid}
	invalid[0].TargetID = ""
	invalid[1].TargetID = strings.Repeat("x", 129)
	invalid[2].TargetRevision = 0
	invalid[3].At = time.Time{}
	invalid[4].Detail = strings.Repeat("x", 513)
	invalid[5].Status = "na"
	invalid[6].Status = model.StatusTimeout
	invalid[7].LatencyMS = nil
	nan := math.NaN()
	invalid[8].LatencyMS = &nan
	invalid[9].At = now.Add(6 * time.Minute)
	for i, m := range invalid {
		if err := s.Add(m); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid measurement %d: %v", i, err)
		}
	}
	for _, days := range []int{-1, 31} {
		if _, err := Open(filepath.Join(t.TempDir(), "bad.db"), days); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid open retention %d: %v", days, err)
		}
	}
	for _, days := range []int{0, -1, 31} {
		if err := s.SetRetention(days); !errors.Is(err, ErrInvalid) {
			t.Errorf("invalid live retention %d: %v", days, err)
		}
	}
	if _, err := Open("", 7); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty path accepted: %v", err)
	}
	if _, err := s.Buckets("target", 1, *now, now.Add(1441*time.Minute)); !errors.Is(err, ErrQueryTooLarge) {
		t.Fatalf("large query accepted: %v", err)
	}
	if _, err := s.Buckets("target", 1, *now, now.Add(-time.Minute)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reversed query accepted: %v", err)
	}
	if _, err := s.Buckets("target", 1, time.Time{}, *now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero query accepted: %v", err)
	}
	buckets, err := s.Buckets("target", 1, *now, now.Add(1440*time.Minute))
	if err != nil || len(buckets) != 1440 {
		t.Fatalf("maximum bounded query rejected: %v, len %d", err, len(buckets))
	}
}

func TestCloseAndExclusiveAccess(t *testing.T) {
	s, now := newTestStore(t, 7)
	started := time.Now()
	if other, err := Open(s.path, 7); err == nil {
		other.Close()
		t.Fatal("second process could open locked storage")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("database lock timeout was not bounded")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("close was not idempotent:", err)
	}
	if err := s.Add(measurement("target", 1, *now, model.StatusOK)); !errors.Is(err, ErrClosed) {
		t.Fatalf("add after close: %v", err)
	}
	if err := s.SetRetention(1); !errors.Is(err, ErrClosed) {
		t.Fatalf("retention after close: %v", err)
	}
	if _, err := s.Latest(); !errors.Is(err, ErrClosed) {
		t.Fatalf("latest after close: %v", err)
	}
	if _, err := s.Buckets("target", 1, *now, now.Add(time.Minute)); !errors.Is(err, ErrClosed) {
		t.Fatalf("history after close: %v", err)
	}
	reopened, err := Open(s.path, 7)
	if err != nil {
		t.Fatal("closed lock was not released:", err)
	}
	reopened.Close()
}

func TestDiskBoundAndCompaction(t *testing.T) {
	now := testNow
	bound := limits{rows: MaxRows, diskBytes: 512 << 10, headroom: 64 << 10, compactMinBytes: 64 << 10}
	s, err := openStore(filepath.Join(t.TempDir(), "bounded.db"), 7, func() time.Time { return now }, bound)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := now.Add(-10 * time.Hour)
	for i := 0; i < 600; i++ {
		m := measurement("target", 1, base.Add(time.Duration(i)*time.Minute), model.StatusOK)
		m.Detail = strings.Repeat("x", 512)
		mustAdd(t, s, m)
		info, err := os.Stat(s.path)
		if err != nil || info.Size() > bound.diskBytes {
			t.Fatalf("disk cap exceeded on write %d: %v %+v", i, err, info)
		}
	}
	if countRows(t, s) >= 600 {
		t.Fatal("disk pressure did not prune rows")
	}
	latest, err := s.Latest()
	if err != nil || len(latest) != 1 || !latest[0].At.Equal(base.Add(599*time.Minute)) {
		t.Fatalf("compaction lost newest row: %v %+v", err, latest)
	}
	now = now.Add(2 * 24 * time.Hour)
	if err := s.SetRetention(1); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s) != 0 {
		t.Fatal("expired rows survived compaction")
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Size() >= bound.compactMinBytes {
		t.Fatalf("empty database high-water size was not reclaimed: %v %+v", err, info)
	}
	for _, suffix := range []string{".compact", ".previous"} {
		if _, err := os.Stat(s.path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary compaction file remains: %s %v", suffix, err)
		}
	}
}

func TestInterruptedCompactionRecovery(t *testing.T) {
	s, now := newTestStore(t, 7)
	mustAdd(t, s, measurement("target", 1, *now, model.StatusTimeout))
	path := s.path
	s.Close()
	if err := os.Rename(path, path+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".compact", []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := openStore(path, 7, func() time.Time { return *now }, testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	latest, err := recovered.Latest()
	if err != nil || len(latest) != 1 || latest[0].Status != model.StatusTimeout {
		t.Fatalf("interrupted compaction lost original data: %v %+v", err, latest)
	}
}

func TestUnknownSchemaAndCorruptRecordErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unrelated.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucket([]byte("unrelated"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if other, err := Open(path, 7); err == nil {
		other.Close()
		t.Fatal("unrelated database schema was accepted")
	}
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("unrelated")) == nil || tx.Bucket(metaBucket) != nil {
			return errors.New("unrelated database was modified")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, now := newTestStore(t, 7)
	mustAdd(t, s, measurement("target", 1, *now, model.StatusOK))
	if err := s.db.Update(func(tx *bolt.Tx) error {
		key := measurementKey(seriesKey("target", 1), *now)
		return tx.Bucket(rowsBucket).Put(key, []byte("invalid-json"))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Latest(); err == nil {
		t.Fatal("latest suppressed a corrupt record")
	}
	if _, err := s.Buckets("target", 1, *now, now.Add(time.Minute)); err == nil {
		t.Fatal("history suppressed a corrupt record")
	}
}

func TestConcurrentWritersAndReaders(t *testing.T) {
	s, now := newTestStore(t, 7)
	var group sync.WaitGroup
	errorsCh := make(chan error, 20)
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if err := s.Add(measurement(fmt.Sprintf("target-%d", index), 1, *now, model.StatusOK)); err != nil {
				errorsCh <- err
			}
			if _, err := s.Latest(); err != nil {
				errorsCh <- err
			}
		}(i)
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	if countRows(t, s) != 10 {
		t.Fatal("concurrent writes lost measurements")
	}
}
