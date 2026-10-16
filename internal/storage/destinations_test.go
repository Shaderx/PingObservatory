package storage

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func peerBatch(at time.Time, ip string) model.Measurement {
	m := batch("peer-fixture", at, []float64{10}, 1)
	m.DestinationIP = ip
	return m
}

func peerState(t *testing.T, s *Store, now time.Time) *model.DestinationState {
	t.Helper()
	stats, err := s.Statistics([]model.Target{target("peer-fixture")}, now)
	if err != nil {
		t.Fatal(err)
	}
	return stats[0].Observation.Destination
}

func TestDestinationSeedsChangesAndReopensWithoutChangingHealth(t *testing.T) {
	s, now := fixture(t)
	legacy := peerBatch(now.Add(-20*time.Minute), "")
	add(t, s, legacy)
	if peerState(t, s, *now) != nil {
		t.Fatal("legacy record invented a destination baseline")
	}
	first := peerBatch(now.Add(-10*time.Minute), "1.1.1.1")
	add(t, s, first)
	if state := peerState(t, s, *now); state.BaselineIP != "1.1.1.1" || state.ChangeCount != 0 || len(state.History) != 0 {
		t.Fatal(state)
	}
	add(t, s, peerBatch(now.Add(-9*time.Minute), "1.1.1.1"))
	changed := peerBatch(now.Add(-8*time.Minute), "8.8.8.8")
	add(t, s, changed)
	add(t, s, changed)
	failed := peerBatch(now.Add(-7*time.Minute), "")
	failed.Status, failed.Attempted, failed.Sent, failed.Received, failed.UnableToMeasure, failed.HasObservation, failed.Samples, failed.LatencyMS = model.StatusDNS, 0, 0, 0, 1, false, nil, nil
	add(t, s, failed)
	add(t, s, peerBatch(now.Add(-6*time.Minute), "1.1.1.1"))
	want := peerState(t, s, *now)
	if want.ChangeCount != 2 || len(want.History) != 2 || want.BaselineIP != "1.1.1.1" || want.LastIP != "1.1.1.1" || want.History[0].OldIP != "1.1.1.1" || want.History[0].NewIP != "8.8.8.8" || want.History[1].NewIP != "1.1.1.1" {
		t.Fatal(want)
	}
	late := peerBatch(now.Add(-9*time.Minute+30*time.Second), "9.9.9.9")
	add(t, s, late)
	if !reflect.DeepEqual(want, peerState(t, s, *now)) {
		t.Fatal("late batch rewound or invented transitions")
	}
	conflict := first
	conflict.DestinationIP = "8.8.4.4"
	if err := s.Add(conflict); !errors.Is(err, ErrInvalid) {
		t.Fatal("equal-time conflict accepted", err)
	}
	newer := peerBatch(now.Add(-time.Minute), "1.1.1.1")
	add(t, s, newer)
	stats, err := s.Statistics([]model.Target{target("peer-fixture")}, *now)
	if err != nil || stats[0].Observation.Status != model.StatusOK || stats[0].Observation.LossPct == nil {
		t.Fatal("informational destination history changed measurement health", err)
	}
	path := s.path
	s.Close()
	reopened, err := openStore(path, 30, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state := peerState(t, reopened, *now)
	if state.ChangeCount != 2 || state.LastIP != "1.1.1.1" || !state.BaselineObservedAt.Equal(first.At) {
		t.Fatal("reopen lost local baseline", state)
	}
}

func TestDestinationBoundsRetentionIsolationAndArchive(t *testing.T) {
	s, now := fixture(t)
	first := peerBatch(now.Add(-50*time.Hour), "")
	add(t, s, first)
	second := peerBatch(now.Add(-50*time.Hour+time.Minute), "1.1.1.1")
	add(t, s, second)
	records, err := s.measurements(first.TargetID, 1, model.MethodICMP, model.FamilyIPv4, first.At.Add(-time.Minute), second.At.Add(time.Minute))
	if err != nil || len(records) != 2 || records[0].DestinationIP != "" || records[1].DestinationIP != "1.1.1.1" {
		t.Fatal("mixed legacy/new compact history lost peer evidence", records, err)
	}
	conflict := second
	conflict.DestinationIP = "8.8.8.8"
	if err := s.Add(conflict); !errors.Is(err, ErrInvalid) {
		t.Fatal("archived equal-time conflict accepted", err)
	}
	for i := 0; i < 71; i++ {
		ip := "8.8.8.8"
		if i%2 == 1 {
			ip = "1.1.1.1"
		}
		add(t, s, peerBatch(now.Add(time.Duration(i-80)*time.Minute), ip))
	}
	state := peerState(t, s, *now)
	if state.ChangeCount != 71 || len(state.History) != 64 || state.HistoryLimit != 64 || !state.BaselineObservedAt.Equal(second.At) {
		t.Fatal("unbounded/truncated count or rewritten first capture", state)
	}
	v6 := peerBatch(now.Add(-5*time.Minute), "2606:4700:4700::1111")
	v6.Family = model.FamilyIPv6
	add(t, s, v6)
	revised := peerBatch(now.Add(-4*time.Minute), "9.9.9.9")
	revised.TargetRevision = 2
	add(t, s, revised)
	otherMethod := peerBatch(now.Add(-3*time.Minute), "8.8.4.4")
	otherMethod.Method = model.MethodTCP
	add(t, s, otherMethod)
	if !reflect.DeepEqual(state, peerState(t, s, *now)) {
		t.Fatal("family/revision/method crossed destination baselines")
	}
	if err := s.SetRetention(1); err != nil {
		t.Fatal(err)
	}
	if state := peerState(t, s, *now); state == nil || state.ChangeCount != 71 {
		t.Fatal("retention change fabricated/reset an active baseline")
	}
	*now = now.Add(31 * 24 * time.Hour)
	for steps := 0; ; steps++ {
		if steps > 20 {
			t.Fatal("bounded expiry did not finish")
		}
		pending, err := s.MaintenanceStep()
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			break
		}
	}
	if rowCount(t, s, "destinations") != 0 || rowCount(t, s, "series") != 0 {
		t.Fatal("inactive destination state did not expire with its series")
	}
}

func TestDestinationSchema2MigrationDoesNotBackfill(t *testing.T) {
	s, now := fixture(t)
	old := peerBatch(now.Add(-5*time.Minute), "")
	add(t, s, old)
	if _, err := s.db.Exec("DROP TABLE destinations; PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	path := s.path
	s.Close()
	loaded, err := openStore(path, 30, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if peerState(t, loaded, *now) != nil || rowCount(t, loaded, "destinations") != 0 {
		t.Fatal("migration fabricated peers for old batches")
	}
	var schema int
	loaded.db.QueryRow("PRAGMA user_version").Scan(&schema)
	if schema != 3 {
		t.Fatal("history schema not migrated")
	}
	add(t, loaded, peerBatch(now.Add(-time.Minute), "1.1.1.1"))
	state := peerState(t, loaded, *now)
	if state.ChangeCount != 0 || !state.BaselineObservedAt.Equal(now.Add(-time.Minute)) {
		t.Fatal("old observations seeded baseline", state)
	}
	var raw []byte
	if err := loaded.db.QueryRow("SELECT data FROM raw WHERE minute=?", old.At.Unix()/60).Scan(&raw); err != nil || raw[0] != 2 {
		t.Fatal("migration rewrote legacy compact data", err)
	}
}

func TestDestinationIPv6ArchiveSurvivesReopen(t *testing.T) {
	s, now := fixture(t)
	m := peerBatch(now.Add(-50*time.Hour), "2606:4700:4700::1111")
	m.Family = model.FamilyIPv6
	add(t, s, m)
	path := s.path
	s.Close()
	reopened, err := openStore(path, 30, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.measurements(m.TargetID, 1, m.Method, m.Family, m.At.Add(-time.Minute), m.At.Add(time.Minute))
	if err != nil || len(records) != 1 || records[0].DestinationIP != m.DestinationIP {
		t.Fatal("archived IPv6 destination lost on reopen", records, err)
	}
	state, err := reopened.destinationForSeries(m.TargetID, 1, m.Method, m.Family, m.At.Add(-time.Minute), *now)
	if err != nil || state == nil || state.BaselineIP != m.DestinationIP || state.ChangeCount != 0 {
		t.Fatal("IPv6 baseline lost on reopen", state, err)
	}
}

func TestDestinationRejectsInvalidOrUnsentCaptures(t *testing.T) {
	s, now := fixture(t)
	for _, ip := range []string{"example.com", "1.1.1.1:443", "::ffff:1.1.1.1", "fe80::1%eth0", "0.0.0.0", "224.0.0.1", "2001:DB8::1", "::1"} {
		m := peerBatch(now.Add(-time.Minute), ip)
		if err := s.Add(m); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid numeric/family destination accepted", ip, err)
		}
	}
	m := peerBatch(now.Add(-time.Minute), "127.0.0.1")
	add(t, s, m)
	for _, discarded := range []bool{false, true} {
		m := peerBatch(now.Add(-time.Minute), "1.1.1.1")
		m.Discarded = discarded
		if !discarded {
			m.Sent, m.Received, m.Attempted, m.Samples, m.LatencyMS, m.HasObservation = 0, 0, 0, nil, nil, false
		}
		if err := s.Add(m); !errors.Is(err, ErrInvalid) {
			t.Fatal("unattempted/discarded capture accepted", err)
		}
	}
	legacy := peerBatch(now.Add(-time.Minute), "1.1.1.1")
	legacy.Sent, legacy.Received, legacy.Attempted, legacy.Samples = 0, 0, 0, nil
	if err := s.Add(legacy); !errors.Is(err, ErrInvalid) {
		t.Fatal("legacy count normalization invented an attempted destination", err)
	}
}

func TestDestinationLogicalExpiryReseedsBeforeCleanup(t *testing.T) {
	s, now := fixture(t)
	if err := s.SetRetention(1); err != nil {
		t.Fatal(err)
	}
	add(t, s, peerBatch(now.Add(-time.Minute), "1.1.1.1"))
	*now = now.Add(25 * time.Hour)
	if peerState(t, s, *now) != nil {
		t.Fatal("logically expired series exposed destination state before cleanup")
	}
	add(t, s, peerBatch(now.Add(-time.Minute), "8.8.8.8"))
	state := peerState(t, s, *now)
	if state == nil || state.ChangeCount != 0 || state.BaselineIP != "8.8.8.8" || state.LastIP != "8.8.8.8" {
		t.Fatal("expired baseline created a false new-epoch change", state)
	}
}
