package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/netip"
	"sort"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

var storedStatuses = []string{"", model.StatusOK, model.StatusTimeout, model.StatusUnreachable, model.StatusDNS, model.StatusPermission, model.StatusUnsupported, model.StatusError, model.StatusSuspended, model.StatusUnavailable, model.StatusDegraded}

func statusCode(status string) byte {
	for i, s := range storedStatuses {
		if s == status && i > 0 {
			return byte(i)
		}
	}
	return 0
}

// The binary codec retains every original successful timing, its timestamp
// and sequence. No percentile, timing quantization or averaged loss is stored.
func encodeMeasurement(m model.Measurement) ([]byte, error) {
	var b bytes.Buffer
	version := byte(2)
	if m.DestinationIP != "" {
		version = 3
	}
	b.WriteByte(version)
	putInt(&b, m.At.UnixNano())
	putInt(&b, m.BatchStartedAt.UnixNano()-m.At.UnixNano())
	putInt(&b, m.BatchEndedAt.UnixNano()-m.At.UnixNano())
	b.WriteByte(statusCode(m.Status))
	flags := byte(0)
	if m.HasObservation {
		flags |= 1
	}
	if m.Discarded {
		flags |= 2
	}
	if m.LatencyMS != nil {
		flags |= 4
	}
	b.WriteByte(flags)
	for _, n := range []int{m.Attempted, m.Sent, m.Received, m.UnableToMeasure, m.UnobservedSent} {
		putUint(&b, uint64(n))
	}
	if m.LatencyMS != nil {
		putFloat(&b, *m.LatencyMS)
	}
	putUint(&b, uint64(len(m.Samples)))
	for _, r := range m.Samples {
		putInt(&b, r.At.UnixNano()-m.At.UnixNano())
		putFloat(&b, r.LatencyMS)
		putUint(&b, uint64(r.Sequence))
	}
	if version == 3 {
		if !validDestinationIP(m.DestinationIP, m.Family) || m.Sent <= 0 || m.Discarded {
			return nil, fmt.Errorf("%w: invalid captured destination", ErrInvalid)
		}
		ip, _ := netip.ParseAddr(m.DestinationIP)
		data := ip.AsSlice()
		b.WriteByte(byte(len(data)))
		b.Write(data)
	}
	if b.Len() > 1024 {
		return nil, fmt.Errorf("%w: encoded batch too large", ErrInvalid)
	}
	return b.Bytes(), nil
}

func decodeMeasurement(data []byte) (model.Measurement, error) {
	var m model.Measurement
	bad := func() (model.Measurement, error) {
		return model.Measurement{}, fmt.Errorf("corrupt compact measurement")
	}
	if len(data) > 1024 {
		return bad()
	}
	r := bytes.NewReader(data)
	version, e := r.ReadByte()
	if e != nil || (version != 2 && version != 3) {
		return bad()
	}
	at, e := binary.ReadVarint(r)
	if e != nil {
		return bad()
	}
	start, e := binary.ReadVarint(r)
	if e != nil {
		return bad()
	}
	end, e := binary.ReadVarint(r)
	if e != nil {
		return bad()
	}
	code, e := r.ReadByte()
	if e != nil || int(code) >= len(storedStatuses) || code == 0 {
		return bad()
	}
	flags, e := r.ReadByte()
	if e != nil || flags&^byte(7) != 0 {
		return bad()
	}
	m.At = time.Unix(0, at).UTC()
	m.BatchStartedAt = time.Unix(0, at+start).UTC()
	m.BatchEndedAt = time.Unix(0, at+end).UTC()
	m.Status = storedStatuses[code]
	m.HasObservation = flags&1 != 0
	m.Discarded = flags&2 != 0
	counts := []*int{&m.Attempted, &m.Sent, &m.Received, &m.UnableToMeasure, &m.UnobservedSent}
	for _, n := range counts {
		v, e := binary.ReadUvarint(r)
		if e != nil || v > maxSamples {
			return bad()
		}
		*n = int(v)
	}
	if flags&4 != 0 {
		v, e := readFloat(r)
		if e != nil || !finite(v) || v < 0 || v > 120000 {
			return bad()
		}
		m.LatencyMS = &v
	}
	n, e := binary.ReadUvarint(r)
	if e != nil || n > maxSamples {
		return bad()
	}
	m.Samples = make([]model.RTTSample, 0, int(n))
	for i := uint64(0); i < n; i++ {
		offset, e := binary.ReadVarint(r)
		if e != nil {
			return bad()
		}
		v, e := readFloat(r)
		if e != nil || !finite(v) || v < 0 || v > 120000 {
			return bad()
		}
		seq, e := binary.ReadUvarint(r)
		if e != nil || seq > 65535 {
			return bad()
		}
		m.Samples = append(m.Samples, model.RTTSample{At: time.Unix(0, at+offset).UTC(), LatencyMS: v, Sequence: int(seq)})
	}
	if version == 3 {
		n, err := r.ReadByte()
		if err != nil || (n != 4 && n != 16) {
			return bad()
		}
		data := make([]byte, int(n))
		if _, err := io.ReadFull(r, data); err != nil {
			return bad()
		}
		ip, ok := netip.AddrFromSlice(data)
		if !ok || !validDestinationIP(ip.String(), "") || m.Sent <= 0 || m.Discarded {
			return bad()
		}
		m.DestinationIP = ip.String()
	}
	if r.Len() != 0 || m.UnobservedSent > m.Sent || m.Received > m.Sent-m.UnobservedSent || m.Sent > m.Attempted || len(m.Samples) != m.Received {
		return bad()
	}
	return m, nil
}

func encodeChunk(records []model.Measurement) ([]byte, error) {
	if len(records) > 30 {
		return nil, fmt.Errorf("%w: more than30 batches in half-hour chunk", ErrInvalid)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].At.Before(records[j].At) })
	var b bytes.Buffer
	b.WriteByte(1)
	putUint(&b, uint64(len(records)))
	for _, m := range records {
		data, e := encodeMeasurement(m)
		if e != nil {
			return nil, e
		}
		putUint(&b, uint64(len(data)))
		b.Write(data)
	}
	if b.Len() > 32768 {
		return nil, fmt.Errorf("%w: compact chunk exceeds32KiB", ErrInvalid)
	}
	return b.Bytes(), nil
}

func decodeChunk(data []byte) ([]model.Measurement, error) {
	if len(data) > 32768 {
		return nil, fmt.Errorf("corrupt compact chunk")
	}
	r := bytes.NewReader(data)
	v, e := r.ReadByte()
	if e != nil || v != 1 {
		return nil, fmt.Errorf("corrupt chunk version")
	}
	n, e := binary.ReadUvarint(r)
	if e != nil || n > 30 {
		return nil, fmt.Errorf("corrupt chunk count")
	}
	out := make([]model.Measurement, 0, int(n))
	seen := map[int64]bool{}
	for i := uint64(0); i < n; i++ {
		length, e := binary.ReadUvarint(r)
		if e != nil || length > 1024 || int(length) > r.Len() {
			return nil, fmt.Errorf("corrupt chunk record length")
		}
		raw := make([]byte, int(length))
		if _, e = io.ReadFull(r, raw); e != nil {
			return nil, e
		}
		m, e := decodeMeasurement(raw)
		if e != nil {
			return nil, e
		}
		minute := m.At.Unix() / 60
		if seen[minute] {
			return nil, fmt.Errorf("duplicate compact minute")
		}
		seen[minute] = true
		out = append(out, m)
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("trailing compact chunk bytes")
	}
	return out, nil
}

func putInt(b *bytes.Buffer, n int64) {
	var raw [10]byte
	k := binary.PutVarint(raw[:], n)
	b.Write(raw[:k])
}
func putUint(b *bytes.Buffer, n uint64) {
	var raw [10]byte
	k := binary.PutUvarint(raw[:], n)
	b.Write(raw[:k])
}
func putFloat(b *bytes.Buffer, n float64) {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], math.Float64bits(n))
	b.Write(raw[:])
}
func readFloat(r *bytes.Reader) (float64, error) {
	var raw [8]byte
	_, e := io.ReadFull(r, raw[:])
	return math.Float64frombits(binary.LittleEndian.Uint64(raw[:])), e
}
