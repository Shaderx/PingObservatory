// Package storage keeps bounded, per-minute probe history in a local database.
package storage

import (
	"bytes"
	"encoding/binary"
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
	bolt "go.etcd.io/bbolt"
)

const (
	DefaultRetentionDays       = 7
	MaxRetentionDays           = 30
	MaxQueryBuckets            = 1440
	MaxRows                    = 100000
	MaxDatabaseBytes     int64 = 128 << 20
	maxRecordBytes             = 4096
)

var (
	ErrClosed        = errors.New("storage is closed")
	ErrInvalid       = errors.New("invalid storage input")
	ErrQueryTooLarge = errors.New("history query exceeds 1440 minute buckets")
	rowsBucket       = []byte("measurements")
	timeBucket       = []byte("by_minute")
	latestBucket     = []byte("latest")
	metaBucket       = []byte("meta")
	countKey         = []byte("count")
	schemaKey        = []byte("schema")
)

type limits struct {
	rows            uint64
	diskBytes       int64
	headroom        int64
	compactMinBytes int64
}

// Store serializes writes and database compaction. No network access is used.
type Store struct {
	mu        sync.RWMutex
	db        *bolt.DB
	lock      *bolt.DB
	path      string
	retention time.Duration
	now       func() time.Time
	limits    limits
}

// Open creates the parent directory if needed. Zero retention selects seven
// days; otherwise retention must be between one and thirty days. Only this
// store's schema is accepted when a database already exists.
func Open(path string, retentionDays int) (*Store, error) {
	return openStore(path, retentionDays, time.Now, limits{
		rows: MaxRows, diskBytes: MaxDatabaseBytes, headroom: 4 << 20,
		compactMinBytes: 4 << 20,
	})
}

func openStore(path string, retentionDays int, now func() time.Time, bound limits) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: database path is empty", ErrInvalid)
	}
	if retentionDays == 0 {
		retentionDays = DefaultRetentionDays
	}
	if retentionDays < 1 || retentionDays > MaxRetentionDays {
		return nil, fmt.Errorf("%w: retention must be 1..30 days", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// Keep this OS-managed lock through compaction, when the main DB must be
	// closed for replacement. A crash releases the lock automatically.
	guard, err := openDB(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("lock database: %w", err)
	}
	opened := false
	defer func() {
		if !opened {
			guard.Close()
		}
	}()
	// If compaction stopped after moving the original aside, recover it. The
	// previous file is deleted only after the current database opens correctly.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, previousErr := os.Stat(path + ".previous"); previousErr == nil {
			if err := os.Rename(path+".previous", path); err != nil {
				return nil, fmt.Errorf("recover database: %w", err)
			}
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	fresh := errors.Is(statErr, os.ErrNotExist)
	if info != nil && info.Size() > bound.diskBytes {
		return nil, fmt.Errorf("database exceeds %d byte storage limit", bound.diskBytes)
	}
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{db: db, lock: guard, path: path, retention: time.Duration(retentionDays) * 24 * time.Hour, now: now, limits: bound}
	if err := db.Update(func(tx *bolt.Tx) error {
		if !fresh {
			meta := tx.Bucket(metaBucket)
			if meta == nil || !bytes.Equal(meta.Get(schemaKey), []byte{1}) {
				return errors.New("unsupported database schema")
			}
			for _, name := range [][]byte{rowsBucket, timeBucket, latestBucket} {
				if tx.Bucket(name) == nil {
					return errors.New("incomplete database schema")
				}
			}
			_, err := rowCount(tx)
			return err
		}
		for _, name := range [][]byte{rowsBucket, timeBucket, latestBucket, metaBucket} {
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		if err := tx.Bucket(metaBucket).Put(schemaKey, []byte{1}); err != nil {
			return err
		}
		return setCount(tx, 0)
	}); err != nil {
		db.Close()
		return nil, err
	}
	for _, suffix := range []string{".previous", ".compact"} {
		if err := removeIfExists(path + suffix); err != nil {
			db.Close()
			return nil, fmt.Errorf("remove completed compaction file: %w", err)
		}
	}
	if err := s.maintainLocked(); err != nil {
		s.Close()
		return nil, err
	}
	opened = true
	return s, nil
}

func openDB(path string) (*bolt.DB, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 250 * time.Millisecond})
	if err == nil {
		// Bound allocation increments as well as the live row count.
		db.AllocSize = 1 << 20
	}
	return db, err
}

// Close is safe to call more than once.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.db != nil {
		err = s.db.Close()
		s.db = nil
	}
	if s.lock != nil {
		err = errors.Join(err, s.lock.Close())
		s.lock = nil
	}
	return err
}

// SetRetention changes the live retention period and immediately prunes expired
// rows. Increasing retention cannot restore previously discarded history.
func (s *Store) SetRetention(days int) error {
	if days < 1 || days > MaxRetentionDays {
		return fmt.Errorf("%w: retention must be 1..30 days", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrClosed
	}
	s.retention = time.Duration(days) * 24 * time.Hour
	return s.maintainLocked()
}

// Add retains the newest attempted measurement in a target revision's minute.
// Expired measurements are ignored, and absence is never written as a failure.
func (s *Store) Add(m model.Measurement) error {
	if err := validateMeasurement(m); err != nil {
		return err
	}
	m.At = m.At.UTC()
	value, err := json.Marshal(m)
	if err != nil || len(value) > maxRecordBytes {
		return fmt.Errorf("%w: measurement encoding is too large", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return ErrClosed
	}
	if m.At.After(s.now().Add(5 * time.Minute)) {
		return fmt.Errorf("%w: measurement is more than five minutes in the future", ErrInvalid)
	}
	if err := s.maintainLocked(); err != nil {
		return err
	}
	if m.At.Truncate(time.Minute).Before(s.cutoff()) {
		return nil
	}
	series := seriesKey(m.TargetID, m.TargetRevision)
	key := measurementKey(series, m.At)
	err = s.db.Update(func(tx *bolt.Tx) error {
		rows := tx.Bucket(rowsBucket)
		count, err := rowCount(tx)
		if err != nil {
			return err
		}
		if previous := rows.Get(key); previous != nil {
			var old model.Measurement
			if err := json.Unmarshal(previous, &old); err != nil {
				return fmt.Errorf("decode stored measurement: %w", err)
			}
			if old.At.After(m.At) {
				return nil
			}
		} else {
			count++
		}
		if err := rows.Put(key, value); err != nil {
			return err
		}
		if err := tx.Bucket(timeBucket).Put(timeKey(key), []byte{}); err != nil {
			return err
		}
		latest := tx.Bucket(latestBucket)
		if previous := latest.Get(series); previous == nil || bytes.Compare(key, previous) >= 0 {
			if err := latest.Put(series, key); err != nil {
				return err
			}
		}
		if err := setCount(tx, count); err != nil {
			return err
		}
		_, err = s.prune(tx, s.limits.rows, s.cutoff())
		return err
	})
	if err != nil {
		return err
	}
	return s.boundDiskLocked()
}

// Buckets returns UTC minutes in [start,end), after truncating both endpoints to
// the minute. It returns at most 1440 buckets. Unattempted minutes have status
// "na" and a nil Measurement; an attempted failure keeps its original status.
func (s *Store) Buckets(targetID string, revision uint64, start, end time.Time) ([]model.Bucket, error) {
	if err := validateSeries(targetID, revision); err != nil {
		return nil, err
	}
	if !validTime(start) || !validTime(end) || end.Before(start) {
		return nil, fmt.Errorf("%w: invalid history interval", ErrInvalid)
	}
	start, end = start.UTC().Truncate(time.Minute), end.UTC().Truncate(time.Minute)
	if end.After(start.Add(MaxQueryBuckets * time.Minute)) {
		return nil, ErrQueryTooLarge
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	result := make([]model.Bucket, 0, int(end.Sub(start)/time.Minute))
	series, cutoff := seriesKey(targetID, revision), s.cutoff()
	err := s.db.View(func(tx *bolt.Tx) error {
		rows := tx.Bucket(rowsBucket)
		for minute := start; minute.Before(end); minute = minute.Add(time.Minute) {
			bucket := model.Bucket{Minute: minute, Status: "na"}
			if !minute.Before(cutoff) {
				if raw := rows.Get(measurementKey(series, minute)); raw != nil {
					var m model.Measurement
					if err := json.Unmarshal(raw, &m); err != nil {
						return fmt.Errorf("decode stored measurement: %w", err)
					}
					bucket.Status, bucket.Measurement = m.Status, &m
				}
			}
			result = append(result, bucket)
		}
		return nil
	})
	return result, err
}

// Latest returns one retained attempted measurement per target ID and revision.
// Callers can select the catalogue's current revisions without merging history.
func (s *Store) Latest() ([]model.Measurement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	result := make([]model.Measurement, 0)
	cutoff := s.cutoff()
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(latestBucket).ForEach(func(_, key []byte) error {
			raw := tx.Bucket(rowsBucket).Get(key)
			if raw == nil {
				return errors.New("database latest index is inconsistent")
			}
			var m model.Measurement
			if err := json.Unmarshal(raw, &m); err != nil {
				return fmt.Errorf("decode stored measurement: %w", err)
			}
			if !m.At.Truncate(time.Minute).Before(cutoff) {
				result = append(result, m)
			}
			return nil
		})
	})
	return result, err
}

func (s *Store) cutoff() time.Time {
	return s.now().UTC().Add(-s.retention).Truncate(time.Minute)
}

func (s *Store) maintainLocked() error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		_, err := s.prune(tx, s.limits.rows, s.cutoff())
		return err
	}); err != nil {
		return err
	}
	return s.boundDiskLocked()
}

func (s *Store) prune(tx *bolt.Tx, maxRows uint64, cutoff time.Time) (uint64, error) {
	count, err := rowCount(tx)
	if err != nil {
		return 0, err
	}
	var removed uint64
	index := tx.Bucket(timeBucket).Cursor()
	cutoffMinute := uint64(cutoff.Unix() / 60)
	for key, _ := index.First(); key != nil; key, _ = index.First() {
		if len(key) < 26 {
			return removed, errors.New("database time index is malformed")
		}
		if count <= maxRows && binary.BigEndian.Uint64(key[:8]) >= cutoffMinute {
			break
		}
		rowKey := append([]byte(nil), key[8:]...)
		rows, latest := tx.Bucket(rowsBucket), tx.Bucket(latestBucket)
		if rows.Get(rowKey) == nil || count == 0 {
			return removed, errors.New("database time index is inconsistent")
		}
		if err := rows.Delete(rowKey); err != nil {
			return removed, err
		}
		if err := index.Delete(); err != nil {
			return removed, err
		}
		series := rowKey[:len(rowKey)-8]
		if bytes.Equal(latest.Get(series), rowKey) {
			// If the deleted row was the last one in its series, remove the
			// latest entry too. Otherwise point it at the newest surviving row.
			cursor := rows.Cursor()
			seek := append(append([]byte(nil), series...), bytes.Repeat([]byte{255}, 8)...)
			candidate, _ := cursor.Seek(seek)
			if candidate == nil || bytes.Compare(candidate, seek) > 0 {
				candidate, _ = cursor.Prev()
			}
			if bytes.HasPrefix(candidate, series) {
				if err := latest.Put(series, candidate); err != nil {
					return removed, err
				}
			} else if err := latest.Delete(series); err != nil {
				return removed, err
			}
		}
		count--
		removed++
	}
	return removed, setCount(tx, count)
}

func (s *Store) boundDiskLocked() error {
	for {
		info, err := os.Stat(s.path)
		if err != nil {
			return err
		}
		stats := s.db.Stats()
		freeBytes := int64(stats.FreePageN+stats.PendingPageN) * int64(s.db.Info().PageSize)
		pressure := info.Size() >= s.limits.diskBytes-s.limits.headroom
		fragmented := info.Size() >= s.limits.compactMinBytes && freeBytes >= info.Size()/3
		if !pressure && !fragmented {
			return nil
		}
		if pressure {
			if err := s.db.Update(func(tx *bolt.Tx) error {
				count, err := rowCount(tx)
				if err != nil {
					return err
				}
				if count == 0 {
					return errors.New("database exceeds storage limit without measurements")
				}
				keep := count - count/10 - 1
				_, err = s.prune(tx, keep, s.cutoff())
				return err
			}); err != nil {
				return err
			}
		}
		if err := s.compactLocked(); err != nil {
			return err
		}
		if !pressure {
			return nil
		}
	}
}

// Compaction temporarily needs a second file no larger than the original.
// The original is retained until the compacted replacement is safely opened.
func (s *Store) compactLocked() error {
	temporary, previous := s.path+".compact", s.path+".previous"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create compact database: %w", err)
	}
	if err := file.Close(); err != nil {
		removeIfExists(temporary)
		return err
	}
	dst, err := openDB(temporary)
	if err != nil {
		removeIfExists(temporary)
		return err
	}
	err = bolt.Compact(dst, s.db, 1<<20)
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		removeIfExists(temporary)
		return fmt.Errorf("compact database: %w", err)
	}
	if err := s.db.Close(); err != nil {
		return err
	}
	s.db = nil
	if err := os.Rename(s.path, previous); err != nil {
		s.db, _ = openDB(s.path)
		removeIfExists(temporary)
		return fmt.Errorf("preserve database before compaction: %w", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		recoveryErr := os.Rename(previous, s.path)
		s.db, _ = openDB(s.path)
		return errors.Join(fmt.Errorf("replace compact database: %w", err), recoveryErr)
	}
	s.db, err = openDB(s.path)
	if err != nil {
		return fmt.Errorf("reopen compact database: %w", err)
	}
	return removeIfExists(previous)
}

func rowCount(tx *bolt.Tx) (uint64, error) {
	raw := tx.Bucket(metaBucket).Get(countKey)
	if len(raw) != 8 {
		return 0, errors.New("database row count is malformed")
	}
	return binary.BigEndian.Uint64(raw), nil
}

func setCount(tx *bolt.Tx, count uint64) error {
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], count)
	return tx.Bucket(metaBucket).Put(countKey, value[:])
}

func seriesKey(targetID string, revision uint64) []byte {
	key := make([]byte, 2+len(targetID)+8)
	binary.BigEndian.PutUint16(key[:2], uint16(len(targetID)))
	copy(key[2:], targetID)
	binary.BigEndian.PutUint64(key[2+len(targetID):], revision)
	return key
}

func measurementKey(series []byte, at time.Time) []byte {
	key := make([]byte, len(series)+8)
	copy(key, series)
	binary.BigEndian.PutUint64(key[len(series):], uint64(at.Unix()/60))
	return key
}

func timeKey(row []byte) []byte {
	key := make([]byte, 8+len(row))
	copy(key, row[len(row)-8:])
	copy(key[8:], row)
	return key
}

func validateSeries(id string, revision uint64) error {
	if len(id) == 0 || len(id) > 128 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || revision == 0 {
		return fmt.Errorf("%w: target ID or revision", ErrInvalid)
	}
	for _, r := range id {
		if r < 32 || r == 127 {
			return fmt.Errorf("%w: target ID contains control characters", ErrInvalid)
		}
	}
	return nil
}

func validTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 1970 && t.UTC().Year() <= 9999
}

func validateMeasurement(m model.Measurement) error {
	if err := validateSeries(m.TargetID, m.TargetRevision); err != nil {
		return err
	}
	if !validTime(m.At) || len(m.Detail) > 512 || !utf8.ValidString(m.Detail) {
		return fmt.Errorf("%w: measurement time or detail", ErrInvalid)
	}
	switch m.Status {
	case model.StatusOK:
		if m.LatencyMS == nil || math.IsNaN(*m.LatencyMS) || math.IsInf(*m.LatencyMS, 0) || *m.LatencyMS < 0 || *m.LatencyMS > 60000 {
			return fmt.Errorf("%w: successful probe requires latency in 0..60000 ms", ErrInvalid)
		}
	case model.StatusTimeout, model.StatusUnreachable, model.StatusDNS,
		model.StatusPermission, model.StatusUnsupported, model.StatusError:
		if m.LatencyMS != nil {
			return fmt.Errorf("%w: failed probe cannot include a latency", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown attempted probe status", ErrInvalid)
	}
	return nil
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
