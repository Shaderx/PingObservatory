//go:build windows

package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net/netip"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/Shaderx/PingObservatory/internal/model"
)

var (
	icmp6Create = icmpDLL.NewProc("Icmp6CreateFile")
	icmp6Send   = icmpDLL.NewProc("Icmp6SendEcho2")
)

// windowsSockaddr6 is the Windows sockaddr_in6 ABI, which has the same layout
// on the supported amd64 and arm64 architectures.
type windowsSockaddr6 struct {
	Family   uint16
	Port     uint16
	FlowInfo uint32
	Address  [16]byte
	ScopeID  uint32
}

func nativeProbe6(ctx context.Context, addr netip.Addr) model.Measurement {
	return nativeProbe6Payload(ctx, addr, 32)
}

func nativeProbe6Payload(ctx context.Context, addr netip.Addr, payloadBytes int) model.Measurement {
	for _, proc := range []*syscall.LazyProc{icmp6Create, icmp6Send, icmpClose} {
		if err := proc.Find(); err != nil {
			return failure(model.StatusUnsupported, "Windows IPv6 ICMP API unavailable")
		}
	}
	handle, _, err := icmp6Create.Call()
	if handle == ^uintptr(0) || handle == 0 {
		return windowsFailure(uint32(err.(syscall.Errno)))
	}
	defer icmpClose.Call(handle)
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || ctx.Err() != nil {
		return failure(model.StatusError, "probe cancelled before ICMPv6 send")
	}
	milliseconds := uint32((time.Until(deadline) + time.Millisecond - 1) / time.Millisecond)
	source := windowsSockaddr6{Family: syscall.AF_INET6}
	destination := windowsSockaddr6{Family: syscall.AF_INET6, Address: addr.As16()}
	if payloadBytes < 16 || payloadBytes > 300 {
		return failure(model.StatusUnsupported, "ICMPv6 payload must be 16..300 bytes")
	}
	request := make([]byte, payloadBytes)
	if _, err := rand.Read(request); err != nil {
		return failure(model.StatusError, "could not generate ICMPv6 payload")
	}
	// ICMPV6_ECHO_REPLY (36 bytes), payload, 8 error bytes, and IO_STATUS_BLOCK.
	reply := make([]byte, payloadBytes+128)
	count, _, sendErr := icmp6Send.Call(handle, 0, 0, 0,
		uintptr(unsafe.Pointer(&source)), uintptr(unsafe.Pointer(&destination)),
		uintptr(unsafe.Pointer(&request[0])), uintptr(len(request)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(milliseconds))
	runtime.KeepAlive(source)
	runtime.KeepAlive(destination)
	runtime.KeepAlive(request)
	runtime.KeepAlive(reply)
	if count == 0 {
		code := uint32(sendErr.(syscall.Errno))
		m := windowsFailure(code)
		if code == 11010 || code >= 11002 && code <= 11005 || code == 11013 || code == 11014 {
			return icmpAfterSend(m)
		}
		return m
	}
	// IPV6_ADDRESS_EX occupies the first 28 bytes, followed by status and RTT.
	if status := binary.LittleEndian.Uint32(reply[28:32]); status != 0 {
		return icmpAfterSend(windowsFailure(status))
	}
	latency := float64(binary.LittleEndian.Uint32(reply[32:36]))
	return model.Measurement{Status: model.StatusOK, LatencyMS: &latency, Sent: 1, Received: 1, HasObservation: true}
}
