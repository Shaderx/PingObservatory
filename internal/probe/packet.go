package probe

import (
	"bytes"
	"encoding/binary"
	"net/netip"
)

func echoRequest(identifier, sequence uint16, payload []byte) []byte {
	packet := make([]byte, 8+len(payload))
	packet[0] = 8 // IPv4 Echo Request
	binary.BigEndian.PutUint16(packet[4:6], identifier)
	binary.BigEndian.PutUint16(packet[6:8], sequence)
	copy(packet[8:], payload)
	binary.BigEndian.PutUint16(packet[2:4], checksum(packet))
	return packet
}

func echoRequest6(identifier, sequence uint16, payload []byte) []byte {
	packet := echoRequest(identifier, sequence, payload)
	packet[0], packet[2], packet[3] = 128, 0, 0
	return packet
}

func checksum6(packet []byte, source, destination netip.Addr) uint16 {
	buffer := make([]byte, 40+len(packet))
	src, dst := source.As16(), destination.As16()
	copy(buffer[:16], src[:])
	copy(buffer[16:32], dst[:])
	binary.BigEndian.PutUint32(buffer[32:36], uint32(len(packet)))
	buffer[39] = 58 // IPv6 next-header: ICMPv6.
	copy(buffer[40:], packet)
	return checksum(buffer)
}

func matchesEchoReply6(packet []byte, identifier, sequence uint16, payload []byte, source, destination netip.Addr) bool {
	return len(packet) == 8+len(payload) && packet[0] == 129 && packet[1] == 0 &&
		binary.BigEndian.Uint16(packet[4:6]) == identifier &&
		binary.BigEndian.Uint16(packet[6:8]) == sequence &&
		checksum6(packet, source, destination) == 0 && bytes.Equal(packet[8:], payload)
}

func matchesEchoReply(packet []byte, identifier, sequence uint16, payload []byte) bool {
	return len(packet) == 8+len(payload) && packet[0] == 0 && packet[1] == 0 &&
		binary.BigEndian.Uint16(packet[4:6]) == identifier &&
		binary.BigEndian.Uint16(packet[6:8]) == sequence &&
		checksum(packet) == 0 && bytes.Equal(packet[8:], payload)
}

// Linux raw IPv4 sockets include the IPv4 header. Validate its lengths and
// source before exposing the ICMP message to the shared echo validator.
func rawEchoPayload(packet []byte, source netip.Addr) []byte {
	if len(packet) < 20 || packet[0]>>4 != 4 || packet[9] != 1 {
		return nil
	}
	headerLength := int(packet[0]&0xf) * 4
	totalLength := int(binary.BigEndian.Uint16(packet[2:4]))
	if headerLength < 20 || headerLength > len(packet) || totalLength < headerLength+8 || totalLength > len(packet) {
		return nil
	}
	// Fragments cannot contain a complete authenticated echo reply.
	if binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0 || checksum(packet[:headerLength]) != 0 {
		return nil
	}
	var sourceBytes [4]byte
	copy(sourceBytes[:], packet[12:16])
	if netip.AddrFrom4(sourceBytes) != source {
		return nil
	}
	return packet[headerLength:totalLength]
}

func checksum(packet []byte) uint16 {
	var sum uint32
	for len(packet) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[:2]))
		packet = packet[2:]
	}
	if len(packet) != 0 {
		sum += uint32(packet[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
