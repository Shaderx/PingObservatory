package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

type historyWindow struct {
	duration, step time.Duration
	count          int
}

var historyWindows = map[string]historyWindow{
	"1h": {time.Hour, time.Minute, 60}, "24h": {24 * time.Hour, 5 * time.Minute, 288},
	"7d": {7 * 24 * time.Hour, 30 * time.Minute, 336}, "30d": {30 * 24 * time.Hour, 2 * time.Hour, 360},
}

// History returns only completed, UTC-aligned bins. Every median/percentile is
// recalculated from the retained individual RTTs, including archived chunks.
func (s *Store) History(targetID string, revision uint64, method, family, window string, now time.Time) (model.History, error) {
	definition, ok := historyWindows[window]
	if !ok || !validSeries(method, family) || targetID == "" || revision == 0 || !validQueryTime(now) {
		return model.History{}, fmt.Errorf("%w: history series/window/time", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return model.History{}, ErrClosed
	}
	end := now.UTC().Truncate(definition.step)
	start := end.Add(-definition.duration)
	h := model.History{TargetID: targetID, TargetRevision: revision, Method: method, Family: family, Window: window, StepSeconds: int(definition.step / time.Second), Start: start, End: end, AsOf: now.UTC(), RetentionDays: int(s.retention / (24 * time.Hour)), Buckets: make([]model.Bucket, definition.count)}
	readStart := start
	cutoff := s.now().UTC().Add(-s.retention)
	h.RetainedFrom = cutoff
	if readStart.Before(cutoff) {
		readStart = cutoff
	}
	data, err := s.measurements(targetID, revision, method, family, readStart, now.UTC().Add(time.Nanosecond))
	if err != nil {
		return model.History{}, err
	}
	values := make([][]float64, definition.count)
	latest := make([]*model.Measurement, definition.count)
	type sampleKey struct {
		at       int64
		sequence int
	}
	seen := make(map[sampleKey]bool)
	for i := range h.Buckets {
		at := start.Add(time.Duration(i) * definition.step)
		h.Buckets[i] = model.Bucket{Minute: at, End: at.Add(definition.step), Status: "na", RetentionExpired: !at.Add(definition.step).After(cutoff)}
	}
	for i := range data {
		m := &data[i]
		if m.Discarded {
			continue
		}
		if !m.At.Before(start) && m.At.Before(end) {
			index := int(m.At.Sub(start) / definition.step)
			b := &h.Buckets[index]
			b.Attempted += m.Attempted
			b.UnableToMeasure += m.UnableToMeasure
			b.Sent += m.Sent
			b.UnobservedSent += m.UnobservedSent
			if m.HasObservation {
				b.Received += m.Received
			}
			if latest[index] == nil || m.At.After(latest[index].At) {
				latest[index] = m
				b.Status = m.Status
			}
		}
		if m.HasObservation {
			for _, sample := range m.Samples {
				if sample.At.Before(readStart) || !sample.At.Before(end) {
					continue
				}
				key := sampleKey{sample.At.UnixNano(), sample.Sequence}
				if seen[key] {
					continue
				}
				seen[key] = true
				index := int(sample.At.Sub(start) / definition.step)
				values[index] = append(values[index], sample.LatencyMS)
			}
		}
	}
	for i := range h.Buckets {
		fillMetrics(&h.Buckets[i], method, values[i])
		if definition.step == time.Minute && latest[i] != nil {
			copy := *latest[i]
			h.Buckets[i].Measurement = &copy
		}
	}
	return h, nil
}

// Buckets preserves the original minute-query API for the default ICMP/IPv4
// series. The newer History API selects method/family and fixed windows.
func (s *Store) Buckets(targetID string, revision uint64, start, end time.Time) ([]model.Bucket, error) {
	if targetID == "" || revision == 0 || !validQueryTime(start) || !validQueryTime(end) || end.Before(start) {
		return nil, ErrInvalid
	}
	start, end = start.UTC().Truncate(time.Minute), end.UTC().Truncate(time.Minute)
	if end.After(start.Add(MaxQueryBuckets * time.Minute)) {
		return nil, ErrQueryTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	readStart := start
	if cutoff := s.now().Add(-s.retention); readStart.Before(cutoff) {
		readStart = cutoff
	}
	data, err := s.measurements(targetID, revision, model.MethodICMP, model.FamilyIPv4, readStart, end)
	if err != nil {
		return nil, err
	}
	byMinute := map[int64]model.Measurement{}
	for _, m := range data {
		byMinute[m.At.Unix()/60] = m
	}
	out := make([]model.Bucket, 0, int(end.Sub(start)/time.Minute))
	for at := start; at.Before(end); at = at.Add(time.Minute) {
		var records []model.Measurement
		if m, ok := byMinute[at.Unix()/60]; ok {
			records = []model.Measurement{m}
		}
		bucket := aggregateBucket(records, model.MethodICMP, at, at.Add(time.Minute))
		bucket.RetentionExpired = !bucket.End.After(s.now().Add(-s.retention))
		if len(records) > 0 {
			copy := records[0]
			bucket.Measurement = &copy
		}
		out = append(out, bucket)
	}
	return out, nil
}

// Statistics returns separate series, latest-batch metrics, a rolling hour and
// a24h baseline. The baseline needs at least five unique successful timings.
func (s *Store) Statistics(targets []model.Target, now time.Time) ([]model.SeriesStats, error) {
	if !validQueryTime(now) || len(targets) > 528 {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrClosed
	}
	out := make([]model.SeriesStats, 0, len(targets)*2)
	for _, target := range targets {
		settings := model.Settings(target)
		for _, family := range settings.Families {
			if !validSeries(settings.Method, family) {
				return nil, ErrInvalid
			}
			observation, err := s.statistics(target.ID, target.Revision, settings.Method, family, now.UTC())
			if err != nil {
				return nil, err
			}
			out = append(out, model.SeriesStats{TargetID: target.ID, TargetRevision: target.Revision, Observation: observation})
		}
	}
	return out, nil
}

func (s *Store) statistics(target string, revision uint64, method, family string, now time.Time) (model.Observation, error) {
	o := model.Observation{Method: method, Family: family, Status: "na", Detail: "No local measurement is available."}
	var latest []byte
	var lastSuccess sql.NullInt64
	err := s.db.QueryRow("SELECT latest,last_success_ns FROM series WHERE target=? AND revision=? AND method=? AND family=?", target, revision, method, family).Scan(&latest, &lastSuccess)
	if errors.Is(err, sql.ErrNoRows) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	var m model.Measurement
	if err = json.Unmarshal(latest, &m); err != nil {
		return o, err
	}
	cutoff := s.now().UTC().Add(-s.retention)
	if m.At.After(now) || m.At.Before(cutoff) {
		return o, nil
	}
	o.Status = m.Status
	o.Detail = m.Detail
	o.ObservedAt = timePointer(m.At)
	o.BatchStartedAt = timePointer(m.BatchStartedAt)
	o.BatchEndedAt = timePointer(m.BatchEndedAt)
	o.Sent = m.Sent
	o.Received = m.Received
	o.Attempted = m.Attempted
	o.UnableToMeasure = m.UnableToMeasure
	o.UnobservedSent = m.UnobservedSent
	if m.Discarded {
		o.Sent = 0
		o.Received = 0
		o.Attempted = 0
		o.UnobservedSent = 0
		o.UnableToMeasure = 0
	}
	o.FailureStage = m.FailureStage
	o.HTTPStatus = m.HTTPStatus
	o.Timings = m.Timings
	o.POPHint = m.POPHint
	if m.HasObservation && !m.Discarded {
		timings := rtts([]model.Measurement{m}, m.BatchStartedAt, now.Add(time.Nanosecond))
		if len(timings) > 0 {
			o.MedianMS = number(quantile(timings, .5))
			o.MinMS = number(minimum(timings))
			o.MaxMS = number(maximum(timings))
		}
	}
	if lastSuccess.Valid {
		at := time.Unix(0, lastSuccess.Int64).UTC()
		if !at.Before(cutoff) && !at.After(now) {
			o.LastSuccessAt = &at
		}
	}
	start := now.Add(-24 * time.Hour)
	if start.Before(cutoff) {
		start = cutoff
	}
	data, err := s.measurements(target, revision, method, family, start, now.Add(time.Nanosecond))
	if err != nil {
		return o, err
	}
	rolling := make([]model.Measurement, 0)
	for _, record := range data {
		if !record.At.Before(now.Add(-time.Hour)) {
			rolling = append(rolling, record)
		}
	}
	b := aggregateBucket(rolling, method, now.Add(-time.Hour), now.Add(time.Nanosecond))
	o.Rolling1hSent = b.Sent
	o.Rolling1hReceived = b.Received
	o.Rolling1hUnobservedSent = b.UnobservedSent
	o.LossPct = b.LossPct
	o.FailurePct = b.FailurePct
	o.P95MS = b.P95MS
	o.StddevMS = b.StddevMS
	for _, record := range rolling {
		if record.HasObservation && !record.Discarded && (o.RollingAsOf == nil || record.At.After(*o.RollingAsOf)) {
			o.RollingAsOf = timePointer(record.At)
		}
	}
	if o.RollingAsOf != nil {
		o.RollingWindowEndedAt = timePointer(now)
	}
	baseline := rtts(data, now.Add(-24*time.Hour), now.Add(time.Nanosecond))
	o.BaselineSampleCount = len(baseline)
	if len(baseline) >= 5 {
		median := quantile(baseline, .5)
		if median > 0 {
			o.BaselineMS = number(median)
		}
		for _, record := range data {
			if record.HasObservation && !record.Discarded {
				for _, sample := range record.Samples {
					if !sample.At.Before(now.Add(-24*time.Hour)) && !sample.At.After(now) && (o.BaselineAsOf == nil || sample.At.After(*o.BaselineAsOf)) {
						o.BaselineAsOf = timePointer(sample.At)
					}
				}
			}
		}
	}
	return o, nil
}

func aggregateBucket(records []model.Measurement, method string, start, end time.Time) model.Bucket {
	b := model.Bucket{Minute: start, End: end, Status: "na"}
	var latest *model.Measurement
	for i := range records {
		m := &records[i]
		if m.Discarded {
			continue
		}
		b.Attempted += m.Attempted
		b.UnableToMeasure += m.UnableToMeasure
		if latest == nil || m.At.After(latest.At) {
			latest = m
		}
		b.Sent += m.Sent
		b.UnobservedSent += m.UnobservedSent
		if m.HasObservation {
			b.Received += m.Received
		}
	}
	if latest != nil {
		b.Status = latest.Status
	}
	timings := rtts(records, start, end)
	fillMetrics(&b, method, timings)
	return b
}

func fillMetrics(b *model.Bucket, method string, timings []float64) {
	if len(timings) > 0 {
		b.MedianMS = number(quantile(timings, .5))
		b.MinMS = number(minimum(timings))
		b.MaxMS = number(maximum(timings))
		b.P95MS = number(quantile(timings, .95))
		b.StddevMS = number(stddev(timings))
		if b.Status == "na" {
			b.Status = model.StatusOK
		}
	}
	if eligible := b.Sent - b.UnobservedSent; eligible > 0 {
		rate := float64(eligible-b.Received) * 100 / float64(eligible)
		if method == model.MethodICMP || method == model.MethodUDP {
			b.LossPct = &rate
		} else {
			b.FailurePct = &rate
		}
	}
}

// Sample identity is timestamp+sequence within one exact series. The persisted
// minute replacement rule and this deduplication prevent repeated timings from
// contributing twice across raw/chunk boundaries or malformed duplicate batches.
func rtts(records []model.Measurement, start, end time.Time) []float64 {
	type key struct {
		at  int64
		seq int
	}
	seen := map[key]bool{}
	out := make([]float64, 0)
	for _, m := range records {
		if !m.HasObservation || m.Discarded {
			continue
		}
		for _, sample := range m.Samples {
			if sample.At.Before(start) || !sample.At.Before(end) {
				continue
			}
			k := key{sample.At.UnixNano(), sample.Sequence}
			if !seen[k] {
				seen[k] = true
				out = append(out, sample.LatencyMS)
			}
		}
	}
	return out
}

func quantile(values []float64, q float64) float64 {
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	if len(ordered) == 1 {
		return ordered[0]
	}
	position := q * float64(len(ordered)-1)
	low := int(math.Floor(position))
	if low == len(ordered)-1 {
		return ordered[low]
	}
	return ordered[low] + (ordered[low+1]-ordered[low])*(position-float64(low))
}
func stddev(values []float64) float64 {
	mean := 0.
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	variance := 0.
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	return math.Sqrt(variance / float64(len(values)))
}
func minimum(values []float64) float64 {
	v := values[0]
	for _, n := range values[1:] {
		if n < v {
			v = n
		}
	}
	return v
}
func maximum(values []float64) float64 {
	v := values[0]
	for _, n := range values[1:] {
		if n > v {
			v = n
		}
	}
	return v
}
func number(n float64) *float64          { return &n }
func timePointer(t time.Time) *time.Time { t = t.UTC(); return &t }
func validQueryTime(t time.Time) bool    { return !t.IsZero() && t.Year() >= 1970 && t.Year() <= 2261 }
