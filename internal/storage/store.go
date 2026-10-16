// Package storage stores local observations and lossless historical chunks.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Shaderx/PingObservatory/internal/model"
	_ "modernc.org/sqlite"
)

const (
	DefaultRetentionDays       = 30
	MaxRetentionDays           = 30
	MaxQueryBuckets            = 1440
	MaxDatabaseBytes     int64 = 4 << 30
	RawRetention               = 26 * time.Hour
	archiveBatch               = 64
	maxSamples                 = 32
)

var (
	ErrClosed        = errors.New("storage is closed")
	ErrInvalid       = errors.New("invalid storage input")
	ErrQueryTooLarge = errors.New("history query exceeds 1440 minute buckets")
	ErrCapacity      = errors.New("local storage capacity exhausted; retained history was not evicted")
)

type Store struct {
	mu              sync.Mutex
	db              *sql.DB
	path            string
	retention       time.Duration
	now             func() time.Time
	maintained      time.Time
	maintenanceWake chan struct{}
}

// Open uses one connection, an exclusive SQLite profile lock, an 8 MiB page
// cache, rollback journals, incremental vacuum and a 4 GiB main-file limit.
// Existing bbolt files are never opened or migrated by this SQLite store.
func Open(path string, retentionDays int) (*Store, error) {
	return openStore(path, retentionDays, time.Now)
}

func openStore(path string, retentionDays int, now func() time.Time) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: empty database path", ErrInvalid)
	}
	if retentionDays == 0 {
		retentionDays = DefaultRetentionDays
	}
	if retentionDays < 1 || retentionDays > 30 {
		return nil, fmt.Errorf("%w: retention must be 1..30 days", ErrInvalid)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > MaxDatabaseBytes {
		return nil, ErrCapacity
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: path, retention: time.Duration(retentionDays) * 24 * time.Hour, now: now, maintenanceWake: make(chan struct{}, 1)}
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	for _, pragma := range []string{
		"PRAGMA busy_timeout=250", "PRAGMA page_size=4096", "PRAGMA auto_vacuum=INCREMENTAL",
		"PRAGMA journal_mode=DELETE", "PRAGMA locking_mode=EXCLUSIVE", "PRAGMA synchronous=FULL",
		"PRAGMA cache_size=-8192", "PRAGMA mmap_size=0", "PRAGMA temp_store=MEMORY",
		"PRAGMA journal_size_limit=16777216", "PRAGMA max_page_count=1048576", "PRAGMA foreign_keys=ON",
	} {
		if _, err = db.Exec(pragma); err != nil {
			return fail(err)
		}
	}
	var schema int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schema); err != nil {
		return fail(err)
	}
	if schema != 0 && schema != 2 && schema != 3 {
		return fail(errors.New("unsupported SQLite history schema"))
	}
	if schema == 0 {
		var tables int
		if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return fail(err)
		}
		if tables != 0 {
			return fail(errors.New("unrelated SQLite database schema"))
		}
	}
	// The write transaction establishes the exclusive OS lock for this profile;
	// it remains held by the retained connection until Close, including vacuum.
	if _, err = db.Exec(`BEGIN EXCLUSIVE;
CREATE TABLE IF NOT EXISTS series(id INTEGER PRIMARY KEY,target TEXT NOT NULL,revision INTEGER NOT NULL,method TEXT NOT NULL,family TEXT NOT NULL,latest BLOB,latest_ns INTEGER,last_success_ns INTEGER,UNIQUE(target,revision,method,family));
CREATE TABLE IF NOT EXISTS raw(series_id INTEGER NOT NULL REFERENCES series(id),minute INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(series_id,minute)) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS raw_age ON raw(minute);
CREATE TABLE IF NOT EXISTS chunks(series_id INTEGER NOT NULL REFERENCES series(id),minute INTEGER NOT NULL,data BLOB NOT NULL,PRIMARY KEY(series_id,minute)) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS chunks_age ON chunks(minute);
CREATE TABLE IF NOT EXISTS destinations(series_id INTEGER PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,data BLOB NOT NULL);
PRAGMA user_version=3;
COMMIT;`); err != nil {
		return fail(capacityError(err))
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fail(err)
	}
	// Expired data is excluded by every read immediately. Physical cleanup and
	// archival resume in bounded background steps, never on the startup path.
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) SetRetention(days int) error {
	if days < 1 || days > 30 {
		return fmt.Errorf("%w: retention must be 1..30 days", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrClosed
	}
	s.retention = time.Duration(days) * 24 * time.Hour
	s.maintained = time.Time{}
	select {
	case s.maintenanceWake <- struct{}{}:
	default:
	}
	return nil
}

// Add keeps the newest batch for each minute and exact series. It never infers
// sent packets from a timeout or a local error, and never creates gap records.
func (s *Store) Add(m model.Measurement) error {
	if m.DestinationIP != "" && (m.Sent <= 0 || m.Discarded) {
		return fmt.Errorf("%w: destination was not captured by an accepted network attempt", ErrInvalid)
	}
	m = normalize(m)
	if err := validate(m); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrClosed
	}
	if m.At.After(s.now().Add(5 * time.Minute)) {
		return fmt.Errorf("%w: future measurement", ErrInvalid)
	}
	if err := s.maintainLocked(false); err != nil {
		return err
	}
	if m.At.Before(s.now().Add(-s.retention)) {
		return nil
	}
	encoded, err := encodeMeasurement(m)
	if err != nil {
		return err
	}
	latest, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(latest) > 16384 {
		return fmt.Errorf("%w: detailed measurement exceeds16KiB", ErrInvalid)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return capacityError(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO series(target,revision,method,family) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", m.TargetID, m.TargetRevision, m.Method, m.Family); err != nil {
		return capacityError(err)
	}
	var id int64
	var previousLatest sql.NullInt64
	if err = tx.QueryRow("SELECT id,latest_ns FROM series WHERE target=? AND revision=? AND method=? AND family=?", m.TargetID, m.TargetRevision, m.Method, m.Family).Scan(&id, &previousLatest); err != nil {
		return err
	}
	if previousLatest.Valid && previousLatest.Int64 < s.now().UTC().Add(-s.retention).UnixNano() {
		// Logical expiry takes effect even while physical cleanup is pending.
		// A new active epoch must seed its own first actual captured destination.
		if _, err = tx.Exec("DELETE FROM destinations WHERE series_id=?", id); err != nil {
			return err
		}
	}
	minute := m.At.Unix() / 60
	var old []byte
	err = tx.QueryRow("SELECT data FROM raw WHERE series_id=? AND minute=?", id, minute).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) && m.At.Before(s.now().Add(-RawRetention)) {
		var chunk []byte
		chunkErr := tx.QueryRow("SELECT data FROM chunks WHERE series_id=? AND minute=?", id, minute/30*30).Scan(&chunk)
		if chunkErr != nil && !errors.Is(chunkErr, sql.ErrNoRows) {
			return chunkErr
		}
		if chunkErr == nil {
			records, decodeErr := decodeChunk(chunk)
			if decodeErr != nil {
				return decodeErr
			}
			for _, previous := range records {
				if previous.At.Unix()/60 == minute {
					old, err = encodeMeasurement(previous)
					if err != nil {
						return err
					}
					break
				}
			}
		}
	}
	if err == nil {
		previous, e := decodeMeasurement(old)
		if e != nil {
			return e
		}
		if previous.At.After(m.At) {
			return nil
		}
		if previous.At.Equal(m.At) && previous.DestinationIP != "" && m.DestinationIP != "" && previous.DestinationIP != m.DestinationIP {
			return fmt.Errorf("%w: conflicting destination at the same observation time", ErrInvalid)
		}
		if previous.At.Equal(m.At) && previous.DestinationIP != "" && m.DestinationIP == "" {
			return nil
		}
	}
	if m.At.Before(s.now().Add(-RawRetention)) {
		err = putChunk(tx, id, minute, m)
		if err == nil {
			_, err = tx.Exec("DELETE FROM raw WHERE series_id=? AND minute=?", id, minute)
		}
	} else {
		_, err = tx.Exec("INSERT INTO raw(series_id,minute,data) VALUES(?,?,?) ON CONFLICT(series_id,minute) DO UPDATE SET data=excluded.data", id, minute, encoded)
	}
	if err != nil {
		return capacityError(err)
	}
	_, err = tx.Exec("UPDATE series SET latest=?,latest_ns=? WHERE id=? AND (latest_ns IS NULL OR latest_ns<=?)", latest, m.At.UnixNano(), id, m.At.UnixNano())
	if err != nil {
		return capacityError(err)
	}
	if err := updateDestination(tx, id, m, s.now().UTC().Add(-s.retention)); err != nil {
		return capacityError(err)
	}
	for _, sample := range m.Samples {
		if !m.Discarded && m.HasObservation {
			_, err = tx.Exec("UPDATE series SET last_success_ns=? WHERE id=? AND (last_success_ns IS NULL OR last_success_ns<?)", sample.At.UnixNano(), id, sample.At.UnixNano())
			if err != nil {
				return capacityError(err)
			}
		}
	}
	return capacityError(tx.Commit())
}

func (s *Store) Latest() ([]model.Measurement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	rows, err := s.db.Query("SELECT latest FROM series WHERE latest_ns>=? ORDER BY target,revision,method,family", s.now().Add(-s.retention).UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.Measurement, 0)
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var m model.Measurement
		if err = json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		if m.DestinationIP != "" && !validDestinationIP(m.DestinationIP, m.Family) {
			return nil, errors.New("corrupt measurement destination/family")
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) maintainLocked(force bool) error {
	now := s.now().UTC()
	if !force && now.Truncate(time.Minute).Equal(s.maintained) {
		return nil
	}
	cutoff := now.Add(-s.retention)
	// Free expired rows first, so a full database can reclaim space before
	// archival. A call commits at most one 64-row transaction and releases the
	// store lock. The remaining rows are the durable restart cursor.
	for _, query := range []struct {
		statement string
		bound     int64
	}{
		{"DELETE FROM raw WHERE (series_id,minute) IN (SELECT series_id,minute FROM raw WHERE minute<? LIMIT 64)", cutoff.Unix() / 60},
		{"DELETE FROM chunks WHERE (series_id,minute) IN (SELECT series_id,minute FROM chunks WHERE minute<? LIMIT 64)", cutoff.Unix()/60 - 30},
		{"DELETE FROM series WHERE id IN (SELECT id FROM series WHERE latest_ns<? AND NOT EXISTS(SELECT 1 FROM raw WHERE series_id=series.id) AND NOT EXISTS(SELECT 1 FROM chunks WHERE series_id=series.id) LIMIT 64)", cutoff.UnixNano()},
	} {
		result, err := s.db.Exec(query.statement, query.bound)
		if err != nil {
			return capacityError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
	// Each 30-minute chunk contains at most 30 original batches and is <=32KiB.
	// Raw-to-chunk replacement is atomic; readers always see one original batch.
	{
		rows, err := s.db.Query("SELECT series_id,minute,data FROM raw WHERE minute<? ORDER BY minute LIMIT 64", now.Add(-RawRetention).Unix()/60)
		if err != nil {
			return err
		}
		type entry struct {
			id, minute int64
			data       []byte
		}
		batch := make([]entry, 0, archiveBatch)
		for rows.Next() {
			var e entry
			if err = rows.Scan(&e.id, &e.minute, &e.data); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			goto complete
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, e := range batch {
			m, decodeErr := decodeMeasurement(e.data)
			if decodeErr != nil {
				tx.Rollback()
				return decodeErr
			}
			if !m.At.Before(cutoff) {
				if err = putChunk(tx, e.id, e.minute, m); err != nil {
					tx.Rollback()
					return capacityError(err)
				}
			}
			if _, err = tx.Exec("DELETE FROM raw WHERE series_id=? AND minute=?", e.id, e.minute); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return capacityError(err)
		}
		return nil
	}
complete:
	if _, err := s.db.Exec("PRAGMA incremental_vacuum(256)"); err != nil {
		return err
	}
	s.maintained = now.Truncate(time.Minute)
	return nil
}

// MaintenanceStep performs one bounded transaction. Pending is true until all
// currently due cleanup and archival is complete. Reads enforce retention even
// while pending; no retained samples are dropped to accelerate cleanup.
func (s *Store) MaintenanceStep() (pending bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return false, ErrClosed
	}
	err = s.maintainLocked(false)
	return !s.now().UTC().Truncate(time.Minute).Equal(s.maintained), err
}

// RunMaintenance yields the store lock between transactions so reopening a
// mature profile does not block the local UI or collection behind a full drain.
// Cancellation stops between atomic steps; the next run resumes from the rows.
func (s *Store) RunMaintenance(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		pending, err := s.MaintenanceStep()
		if err != nil {
			return err
		}
		delay := time.Minute
		if pending {
			delay = 10 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-s.maintenanceWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func putChunk(tx *sql.Tx, id, minute int64, m model.Measurement) error {
	start := minute / 30 * 30
	var raw []byte
	err := tx.QueryRow("SELECT data FROM chunks WHERE series_id=? AND minute=?", id, start).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var records []model.Measurement
	if err == nil {
		records, err = decodeChunk(raw)
		if err != nil {
			return err
		}
	}
	found := false
	for i, old := range records {
		if old.At.Unix()/60 == minute {
			if old.At.After(m.At) {
				return nil
			}
			if old.At.Equal(m.At) && old.DestinationIP != "" {
				if m.DestinationIP == "" {
					return nil
				}
				if old.DestinationIP != m.DestinationIP {
					return fmt.Errorf("%w: conflicting archived destination at the same observation time", ErrInvalid)
				}
			}
			records[i] = m
			found = true
			break
		}
	}
	if !found {
		records = append(records, m)
	}
	encoded, err := encodeChunk(records)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO chunks(series_id,minute,data) VALUES(?,?,?) ON CONFLICT(series_id,minute) DO UPDATE SET data=excluded.data", id, start, encoded)
	return err
}

func (s *Store) measurements(id string, revision uint64, method, family string, start, end time.Time) ([]model.Measurement, error) {
	var series int64
	err := s.db.QueryRow("SELECT id FROM series WHERE target=? AND revision=? AND method=? AND family=?", id, revision, method, family).Scan(&series)
	if errors.Is(err, sql.ErrNoRows) {
		return []model.Measurement{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]model.Measurement, 0)
	for _, q := range []struct {
		table string
		min   int64
	}{{"raw", start.Unix() / 60}, {"chunks", start.Unix() / 1800 * 30}} {
		rows, err := s.db.Query("SELECT data FROM "+q.table+" WHERE series_id=? AND minute>=? AND minute<? ORDER BY minute", series, q.min, (end.Unix()+59)/60)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return nil, err
			}
			var records []model.Measurement
			if q.table == "raw" {
				m, e := decodeMeasurement(data)
				err = e
				records = []model.Measurement{m}
			} else {
				records, err = decodeChunk(data)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			for _, m := range records {
				if !m.At.Before(start) && m.At.Before(end) {
					m.TargetID = id
					m.TargetRevision = revision
					m.Method = method
					m.Family = family
					if m.DestinationIP != "" && !validDestinationIP(m.DestinationIP, family) {
						rows.Close()
						return nil, errors.New("corrupt compact destination/family")
					}
					out = append(out, m)
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func normalize(m model.Measurement) model.Measurement {
	m.At = m.At.UTC()
	if m.Method == "" {
		m.Method = model.MethodICMP
	}
	if m.Family == "" {
		m.Family = model.FamilyIPv4
	}
	if m.BatchStartedAt.IsZero() {
		m.BatchStartedAt = m.At
	}
	if m.BatchEndedAt.IsZero() {
		m.BatchEndedAt = m.At
	}
	// Legacy successful records contain direct response evidence. Legacy
	// failures contain no send evidence and remain outside packet statistics.
	if m.Status == model.StatusOK && m.LatencyMS != nil && m.Attempted == 0 && m.Sent == 0 && m.Received == 0 && len(m.Samples) == 0 {
		m.Attempted = 1
		m.Sent = 1
		m.Received = 1
		m.HasObservation = true
		m.Samples = []model.RTTSample{{At: m.At, LatencyMS: *m.LatencyMS, Sequence: 1}}
	}
	return m
}

func validate(m model.Measurement) error {
	if m.DestinationIP != "" && (!validDestinationIP(m.DestinationIP, m.Family) || m.Sent <= 0 || m.Discarded) {
		return fmt.Errorf("%w: destination must be an actual attempted numeric address in this family", ErrInvalid)
	}
	if len(m.TargetID) == 0 || len(m.TargetID) > 128 || !utf8.ValidString(m.TargetID) || strings.TrimSpace(m.TargetID) != m.TargetID || m.TargetRevision == 0 || m.TargetRevision > 9007199254740991 {
		return fmt.Errorf("%w: target identity", ErrInvalid)
	}
	if m.At.IsZero() || m.At.Year() < 1970 || m.At.Year() > 2261 || !m.Discarded && (m.BatchStartedAt.After(m.At) || m.BatchEndedAt.After(m.At) || m.BatchEndedAt.Before(m.BatchStartedAt) || m.At.Sub(m.BatchStartedAt) > 10*time.Minute) {
		return fmt.Errorf("%w: batch timestamps", ErrInvalid)
	}
	if !validSeries(m.Method, m.Family) || statusCode(m.Status) == 0 || len(m.Detail) > 512 || !utf8.ValidString(m.Detail) {
		return fmt.Errorf("%w: method, family, status or detail", ErrInvalid)
	}
	if m.Attempted < 0 || m.Attempted > maxSamples || m.Sent < 0 || m.Sent > m.Attempted || m.UnobservedSent < 0 || m.UnobservedSent > m.Sent || m.Received < 0 || m.Received > m.Sent-m.UnobservedSent || m.UnableToMeasure < 0 || m.UnableToMeasure > maxSamples || len(m.Samples) != m.Received || len(m.Samples) > maxSamples {
		return fmt.Errorf("%w: observation counts", ErrInvalid)
	}
	if !m.HasObservation && (m.Sent != m.UnobservedSent || m.Received != 0) {
		return fmt.Errorf("%w: absent observation has eligible counts", ErrInvalid)
	}
	seen := map[int]bool{}
	for _, r := range m.Samples {
		if r.At.IsZero() || r.At.Before(m.BatchStartedAt) || r.At.After(m.At) || !finite(r.LatencyMS) || r.LatencyMS < 0 || r.LatencyMS > 120000 || r.Sequence < 0 || r.Sequence > 65535 || seen[r.Sequence] {
			return fmt.Errorf("%w: RTT sample", ErrInvalid)
		}
		seen[r.Sequence] = true
	}
	if m.LatencyMS != nil && (!finite(*m.LatencyMS) || *m.LatencyMS < 0 || *m.LatencyMS > 120000) {
		return fmt.Errorf("%w: latency", ErrInvalid)
	}
	return nil
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func validSeries(method, family string) bool {
	return (family == model.FamilyIPv4 || family == model.FamilyIPv6) && (method == model.MethodICMP || method == model.MethodUDP || method == model.MethodTCP || method == model.MethodHTTPS || method == model.MethodDNS)
}
func capacityError(err error) error {
	if err == nil {
		return nil
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&255 == 13 {
		return errors.Join(ErrCapacity, err)
	}
	return err
}
