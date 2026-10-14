//go:build windows

package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/Shaderx/PingObservatory/internal/model"
)

// Go's syscall package registers iphlpapi.dll as a system DLL, so it is loaded
// from System32. These functions require neither a raw socket nor elevation.
var (
	icmpDLL    = syscall.NewLazyDLL("iphlpapi.dll")
	icmpCreate = icmpDLL.NewProc("IcmpCreateFile")
	icmpSend   = icmpDLL.NewProc("IcmpSendEcho")
	icmpClose  = icmpDLL.NewProc("IcmpCloseHandle")
)

func nativeProbe(ctx context.Context, addr netip.Addr) model.Measurement {
	for _, proc := range []*syscall.LazyProc{icmpCreate, icmpSend, icmpClose} {
		if err := proc.Find(); err != nil {
			return failure(model.StatusUnsupported, "Windows IPv4 ICMP API unavailable")
		}
	}
	handle, _, err := icmpCreate.Call()
	if handle == ^uintptr(0) || handle == 0 {
		return windowsFailure(uint32(err.(syscall.Errno)))
	}
	defer icmpClose.Call(handle)
	deadline, ok := ctx.Deadline()
	if !ok {
		return failure(model.StatusError, "missing probe deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || ctx.Err() != nil {
		return failure(model.StatusError, "probe cancelled before ICMP send")
	}
	// IcmpSendEcho is synchronous. Do not abandon a goroutine holding native
	// buffers on cancellation: the call always finishes within this budget,
	// with at most one millisecond rounding for the Windows API.
	milliseconds := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	request := []byte("PingObservatory")
	// This accommodates either native or ICMP_ECHO_REPLY32 header layout, the
	// payload and the extra 8 bytes documented for an ICMP error message. Only
	// the first three DWORD fields are read, at identical offsets on both ABIs.
	reply := make([]byte, 256)
	ip := addr.As4()
	count, _, sendErr := icmpSend.Call(handle,
		uintptr(binary.LittleEndian.Uint32(ip[:])),
		uintptr(unsafe.Pointer(&request[0])), uintptr(len(request)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(milliseconds))
	runtime.KeepAlive(request)
	runtime.KeepAlive(reply)
	if count == 0 {
		return windowsFailure(uint32(sendErr.(syscall.Errno)))
	}
	status := binary.LittleEndian.Uint32(reply[4:8])
	if status != 0 {
		return windowsFailure(status)
	}
	latency := float64(binary.LittleEndian.Uint32(reply[8:12]))
	return model.Measurement{Status: model.StatusOK, LatencyMS: &latency}
}

func windowsFailure(code uint32) model.Measurement {
	status := model.StatusError
	switch code {
	case 5, 10013: // ERROR_ACCESS_DENIED, WSAEACCES
		status = model.StatusPermission
	case 50, 10043, 10047: // ERROR_NOT_SUPPORTED, protocol/address family
		status = model.StatusUnsupported
	case 11010, 10060: // IP_REQ_TIMED_OUT, WSAETIMEDOUT
		status = model.StatusTimeout
	case 11002, 11003, 11004, 11005, 11012, 11013, 11014, 11018, 10051, 10065:
		status = model.StatusUnreachable
	}
	return failure(status, fmt.Sprintf("Windows ICMP status %d", code))
}
