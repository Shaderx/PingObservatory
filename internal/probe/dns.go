package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func dnsQuestion(name, queryType string, id uint16) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[:2], id)
	packet[2], packet[5] = 1, 1 // RD, one question
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, errors.New("invalid DNS query label")
		}
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
	}
	packet = append(packet, 0, 0, 1, 0, 1)
	if queryType == "AAAA" {
		packet[len(packet)-3] = 28
	} else if queryType != "A" {
		return nil, errors.New("unsupported DNS query type")
	}
	return packet, nil
}

func probeDNS(ctx context.Context, addr netip.Addr, port int, p model.ProbeSettings) (result model.Measurement) {
	defer func() { result = captureDestination(result, addr) }()
	queryID := make([]byte, 2)
	if _, err := rand.Read(queryID); err != nil {
		return failure(model.StatusError, "could not create DNS transaction ID")
	}
	id := binary.BigEndian.Uint16(queryID)
	query, err := dnsQuestion(p.DNSQueryName, p.DNSQueryType, id)
	if err != nil {
		return failure(model.StatusUnsupported, err.Error())
	}
	conn, err := net.DialUDP(networkFor("udp", addr), nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, uint16(port))))
	if err != nil {
		return operationFailure(err, 0, "connect")
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Duration(p.TimeoutMS) * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return failure(model.StatusError, "could not set DNS deadline")
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	started := time.Now()
	n, err := conn.Write(query)
	if err != nil {
		return operationFailure(err, 0, "request")
	}
	if n != len(query) {
		return failure(model.StatusError, "incomplete DNS query write")
	}
	buffer := make([]byte, 4096)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			return operationFailure(err, 1, "response")
		}
		ended := time.Now()
		if n < 2 || binary.BigEndian.Uint16(buffer[:2]) != id {
			continue
		}
		matched, err := dnsAnswerMatches(buffer[:n], p.DNSQueryName, p.DNSQueryType, p.DNSExpectedAnswers)
		m := model.Measurement{Status: model.StatusDegraded, Attempted: 1, Sent: 1, HasObservation: true, FailureStage: "response"}
		if err != nil {
			m.Status, m.Detail, m.FailureStage, m.UnableToMeasure, m.HasObservation = model.StatusError, err.Error(), "parse", 1, false
			m.UnobservedSent = 1
			return m
		}
		if !matched {
			m.Detail = "DNS response did not match NOERROR and the expected answer set"
			return m
		}
		ms := float64(ended.Sub(started)) / float64(time.Millisecond)
		m.Status, m.Received, m.FailureStage, m.LatencyMS = model.StatusOK, 1, "", &ms
		m.Samples = []model.RTTSample{{At: ended.UTC(), LatencyMS: ms, Sequence: 1}}
		m.Timings = map[string]float64{"dns": ms}
		return m
	}
}

func dnsName(packet []byte, offset int) (string, int, error) {
	var labels []string
	next := -1
	visited := map[int]bool{}
	length := 0
	for steps := 0; steps < 128; steps++ {
		if offset < 0 || offset >= len(packet) || visited[offset] {
			return "", 0, errors.New("invalid DNS name compression")
		}
		visited[offset] = true
		n := int(packet[offset])
		offset++
		if n == 0 {
			if next < 0 {
				next = offset
			}
			return strings.ToLower(strings.Join(labels, ".")), next, nil
		}
		if n&0xc0 == 0xc0 {
			if offset >= len(packet) {
				return "", 0, errors.New("truncated DNS name pointer")
			}
			if next < 0 {
				next = offset + 1
			}
			offset = (n&0x3f)<<8 | int(packet[offset])
			continue
		}
		if n&0xc0 != 0 || n > 63 || offset+n > len(packet) {
			return "", 0, errors.New("invalid DNS name label")
		}
		length += n + 1
		if length > 254 {
			return "", 0, errors.New("DNS name is too long")
		}
		labels = append(labels, string(packet[offset:offset+n]))
		offset += n
	}
	return "", 0, errors.New("excessive DNS name compression")
}

func dnsAnswerMatches(packet []byte, name, queryType string, expected []string) (bool, error) {
	if len(packet) < 12 {
		return false, errors.New("truncated DNS response header")
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags&0x8000 == 0 || flags&0x7800 != 0 {
		return false, errors.New("DNS response has invalid QR/opcode")
	}
	if flags&0x020f != 0 {
		return false, nil
	} // truncation or non-NOERROR
	if binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return false, errors.New("DNS response must repeat one question")
	}
	question, offset, err := dnsName(packet, 12)
	if err != nil {
		return false, err
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	typeCode := uint16(1)
	if queryType == "AAAA" {
		typeCode = 28
	}
	if offset+4 > len(packet) || question != name || binary.BigEndian.Uint16(packet[offset:offset+2]) != typeCode || binary.BigEndian.Uint16(packet[offset+2:offset+4]) != 1 {
		return false, errors.New("DNS response question mismatch")
	}
	offset += 4
	count := int(binary.BigEndian.Uint16(packet[6:8]))
	if count == 0 {
		return false, nil
	}
	if count > 256 {
		return false, errors.New("excessive DNS answer count")
	}
	allowed := map[netip.Addr]bool{}
	for _, value := range expected {
		if ip, err := netip.ParseAddr(value); err == nil {
			allowed[ip] = true
		}
	}
	for i := 0; i < count; i++ {
		owner, next, err := dnsName(packet, offset)
		if err != nil {
			return false, err
		}
		if next+10 > len(packet) {
			return false, errors.New("truncated DNS resource record")
		}
		rrType, class := binary.BigEndian.Uint16(packet[next:next+2]), binary.BigEndian.Uint16(packet[next+2:next+4])
		size := int(binary.BigEndian.Uint16(packet[next+8 : next+10]))
		offset = next + 10
		if offset+size > len(packet) {
			return false, errors.New("truncated DNS answer data")
		}
		if owner != name || rrType != typeCode || class != 1 {
			return false, nil
		}
		ip, ok := netip.AddrFromSlice(packet[offset : offset+size])
		if !ok || !allowed[ip] {
			return false, nil
		}
		offset += size
	}
	return true, nil
}
