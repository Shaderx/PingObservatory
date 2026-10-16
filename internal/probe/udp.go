package probe

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sort"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

const awsBeaconPort = 7770
const awsHeaderSize = 28

var awsPayloadMagic = [4]byte{'P', 'L', 'G', '1'}

// This preserves the reviewed LG echo framing. Local fixtures prove framing
// and reply validation, not interoperability with any GameLift endpoint.
func makeAWSPayload(size int, nonce [16]byte, sequence uint64) ([]byte, error) {
	if size < awsHeaderSize || size > 300 {
		return nil, errors.New("UDP echo payload must be 28..300 bytes")
	}
	packet := make([]byte, size)
	copy(packet[:4], awsPayloadMagic[:])
	copy(packet[4:20], nonce[:])
	binary.BigEndian.PutUint64(packet[20:28], sequence)
	_, err := rand.Read(packet[28:])
	return packet, err
}

func parseSequence(packet []byte, nonce [16]byte) (uint64, bool) {
	if len(packet) < awsHeaderSize || !bytes.Equal(packet[:4], awsPayloadMagic[:]) || !bytes.Equal(packet[4:20], nonce[:]) {
		return 0, false
	}
	return binary.BigEndian.Uint64(packet[20:28]), true
}

type pendingEcho struct {
	packet  []byte
	sent    time.Time
	expires time.Time
}

func probeUDP(ctx context.Context, addr netip.Addr, port int, p model.ProbeSettings) model.Measurement {
	m := model.Measurement{Status: model.StatusTimeout}
	conn, err := net.DialUDP(networkFor("udp", addr), nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, uint16(port))))
	if err != nil {
		return operationFailure(err, 0, "connect")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return failure(model.StatusError, "could not create UDP request nonce")
	}
	pending := map[uint64]pendingEcho{}
	buffer := make([]byte, 1024)
	nextSend := time.Now()
	count := 0
	for count < p.PacketsPerCycle || len(pending) > 0 {
		if ctx.Err() != nil {
			return interruptedMeasurement(ctx, m, len(pending), "UDP echo cycle deadline interrupted pending measurements")
		}
		now := time.Now()
		if count < p.PacketsPerCycle && !now.Before(nextSend) {
			seq := uint64(count + 1)
			packet, err := makeAWSPayload(p.PayloadBytes, nonce, seq)
			if err != nil {
				m.Status, m.Detail, m.UnableToMeasure = model.StatusError, err.Error(), 1
				m.UnobservedSent = len(pending)
				break
			}
			if err := conn.SetWriteDeadline(now.Add(time.Duration(p.TimeoutMS) * time.Millisecond)); err != nil {
				m.Status, m.Detail, m.UnableToMeasure = model.StatusError, err.Error(), 1
				m.UnobservedSent = len(pending)
				break
			}
			m.Attempted++
			n, err := conn.Write(packet)
			if err != nil || n != len(packet) {
				if err == nil {
					err = errors.New("incomplete UDP echo write")
				}
				f := operationFailure(err, 0, "request")
				m.Status, m.Detail, m.FailureStage, m.UnableToMeasure = f.Status, f.Detail, f.FailureStage, 1
				m.UnobservedSent = len(pending)
				break
			}
			m.Sent++
			count++
			sent := time.Now()
			pending[seq] = pendingEcho{packet: packet, sent: sent, expires: sent.Add(time.Duration(p.TimeoutMS) * time.Millisecond)}
			nextSend = sent.Add(time.Duration(p.PacketSpacingMS) * time.Millisecond)
			continue
		}
		wake := time.Time{}
		if count < p.PacketsPerCycle {
			wake = nextSend
		}
		for _, packet := range pending {
			if wake.IsZero() || packet.expires.Before(wake) {
				wake = packet.expires
			}
		}
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(wake) {
			wake = deadline
		}
		if err := conn.SetReadDeadline(wake); err != nil {
			m.Status, m.Detail, m.UnableToMeasure = model.StatusError, err.Error(), 1
			m.UnobservedSent = len(pending)
			break
		}
		n, err := conn.Read(buffer)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				for seq, packet := range pending {
					if !time.Now().Before(packet.expires) {
						delete(pending, seq)
					}
				}
				continue
			}
			f := operationFailure(err, m.Sent, "response")
			m.Status, m.Detail, m.FailureStage = f.Status, f.Detail, f.FailureStage
			// A socket error does not identify the pending sequence. Preserve
			// earlier replies/timeouts, but do not invent pending outcomes.
			m.UnableToMeasure, m.UnobservedSent = 1, len(pending)
			break
		}
		received := time.Now()
		seq, ok := parseSequence(buffer[:n], nonce)
		packet, exists := pending[seq]
		if !ok || !exists || received.After(packet.expires) || !bytes.Equal(buffer[:n], packet.packet) {
			continue
		}
		delete(pending, seq)
		m.Received++
		m.Samples = append(m.Samples, model.RTTSample{At: received.UTC(), LatencyMS: float64(received.Sub(packet.sent)) / float64(time.Millisecond), Sequence: int(seq)})
	}
	sort.Slice(m.Samples, func(i, j int) bool { return m.Samples[i].Sequence < m.Samples[j].Sequence })
	return summarize(m)
}
