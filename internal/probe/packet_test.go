package probe

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestChecksumKnownVector(t *testing.T) {
	// RFC 1071 section 3's eight-byte example folds to 0xddf2.
	if got := checksum([]byte{0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}); got != 0x220d {
		t.Fatalf("checksum = %04x, want 220d", got)
	}
	if got := checksum([]byte{0x12}); got != 0xedff {
		t.Fatalf("odd-length checksum = %04x, want edff", got)
	}
}

func TestICMPv6ChecksumAndIdentity(t *testing.T) {
	address := netip.MustParseAddr("::1")
	packet := []byte{129, 0, 0, 0, 0, 0, 0, 1}
	if got := checksum6(packet, address, address); got != 0x7eba {
		t.Fatalf("ICMPv6 checksum = %04x, want 7eba", got)
	}
	binary.BigEndian.PutUint16(packet[2:4], 0x7eba)
	if !matchesEchoReply6(packet, 0, 1, nil, address, address) {
		t.Fatal("valid ICMPv6 reply rejected")
	}
	if matchesEchoReply6(packet, 0, 2, nil, address, address) || matchesEchoReply6(packet, 0, 1, nil, address, netip.MustParseAddr("::2")) {
		t.Fatal("unrelated ICMPv6 reply accepted")
	}
	request := echoRequest6(7, 1, []byte("nonce"))
	if request[0] != 128 || request[2] != 0 || request[3] != 0 {
		t.Fatalf("invalid kernel-checksummed ICMPv6 request: %x", request)
	}
}

func TestRawIPv4HeaderValidation(t *testing.T) {
	echo := echoRequest(123, 1, []byte("nonce"))
	echo[0], echo[2], echo[3] = 0, 0, 0
	binary.BigEndian.PutUint16(echo[2:4], checksum(echo))
	header := make([]byte, 24) // IPv4 options exercise variable IHL.
	header[0], header[8], header[9] = 0x46, 64, 1
	binary.BigEndian.PutUint16(header[2:4], uint16(len(header)+len(echo)))
	copy(header[12:16], []byte{127, 0, 0, 1})
	copy(header[16:20], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(header[10:12], checksum(header))
	packet := append(header, echo...)
	source := netip.MustParseAddr("127.0.0.1")
	if got := rawEchoPayload(packet, source); !bytes.Equal(got, echo) {
		t.Fatalf("valid raw reply rejected: %x", got)
	}
	if rawEchoPayload(packet, netip.MustParseAddr("127.0.0.2")) != nil {
		t.Fatal("wrong raw source accepted")
	}
	for _, offset := range []int{0, 2, 3, 6, 8, 9, 10, 12} {
		corrupt := append([]byte(nil), packet...)
		corrupt[offset] ^= 1
		if rawEchoPayload(corrupt, source) != nil {
			t.Fatalf("corrupt IPv4 header at %d accepted", offset)
		}
	}
	for length := 0; length < len(packet); length++ {
		if rawEchoPayload(packet[:length], source) != nil {
			t.Fatalf("truncated raw packet of length %d accepted", length)
		}
	}
	fragment := append([]byte(nil), packet...)
	fragment[6] = 0x20 // More fragments, with an otherwise valid checksum.
	fragment[10], fragment[11] = 0, 0
	binary.BigEndian.PutUint16(fragment[10:12], checksum(fragment[:24]))
	if rawEchoPayload(fragment, source) != nil {
		t.Fatal("fragment accepted")
	}
}

func TestEchoPacketAndReplyValidation(t *testing.T) {
	payload := []byte("a unique nonce")
	request := echoRequest(0x1234, 0x5678, payload)
	if request[0] != 8 || request[1] != 0 || binary.BigEndian.Uint16(request[4:6]) != 0x1234 || binary.BigEndian.Uint16(request[6:8]) != 0x5678 || checksum(request) != 0 {
		t.Fatalf("invalid request: %x", request)
	}
	if matchesEchoReply(request, 0x1234, 0x5678, payload) {
		t.Fatal("request was accepted as a reply")
	}
	reply := append([]byte(nil), request...)
	reply[0] = 0
	reply[2], reply[3] = 0, 0
	binary.BigEndian.PutUint16(reply[2:4], checksum(reply))
	if !matchesEchoReply(reply, 0x1234, 0x5678, payload) {
		t.Fatal("valid reply was rejected")
	}
	if matchesEchoReply(reply, 0x1235, 0x5678, payload) || matchesEchoReply(reply, 0x1234, 0x5679, payload) || matchesEchoReply(reply, 0x1234, 0x5678, []byte("different nonce")) {
		t.Fatal("unrelated reply was accepted")
	}
	for i := 0; i < len(reply); i++ {
		if matchesEchoReply(reply[:i], 0x1234, 0x5678, payload) {
			t.Fatalf("truncated reply of length %d accepted", i)
		}
	}
	reply[len(reply)-1] ^= 1
	if matchesEchoReply(reply, 0x1234, 0x5678, payload) {
		t.Fatal("corrupt reply was accepted")
	}
}
