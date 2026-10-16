//go:build linux

package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"syscall"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func nativeProbe6(ctx context.Context, addr netip.Addr) model.Measurement {
	return nativeProbe6Payload(ctx, addr, 32)
}

func nativeProbe6Payload(ctx context.Context, addr netip.Addr, payloadBytes int) model.Measurement {
	fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMPV6)
	raw := false
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		fd, err = syscall.Socket(syscall.AF_INET6, syscall.SOCK_RAW|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMPV6)
		raw = true
	}
	if err != nil {
		return linuxFailure(err)
	}
	f := os.NewFile(uintptr(fd), "icmp6")
	if f == nil {
		syscall.Close(fd)
		return failure(model.StatusError, "could not open ICMPv6 socket")
	}
	defer f.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		return failure(model.StatusError, "missing probe deadline")
	}
	if err := f.SetDeadline(deadline); err != nil {
		return failure(model.StatusUnsupported, "ICMPv6 socket deadlines unavailable")
	}
	stop := context.AfterFunc(ctx, func() { _ = f.SetDeadline(time.Now()) })
	defer stop()
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_RECVERR, 1); err != nil {
		return linuxFailure(err)
	}
	if payloadBytes < 16 || payloadBytes > 300 {
		return failure(model.StatusUnsupported, "ICMPv6 payload must be 16..300 bytes")
	}
	nonce := make([]byte, payloadBytes)
	if _, err := rand.Read(nonce); err != nil {
		return failure(model.StatusError, "could not create ICMPv6 request token")
	}
	identifier := binary.BigEndian.Uint16(nonce[:2])
	if !raw {
		if err := syscall.Bind(fd, &syscall.SockaddrInet6{}); err != nil {
			return linuxFailure(err)
		}
		local, err := syscall.Getsockname(fd)
		if err != nil {
			return linuxFailure(err)
		}
		bound, ok := local.(*syscall.SockaddrInet6)
		if !ok {
			return failure(model.StatusError, "unexpected ICMPv6 socket address")
		}
		identifier = uint16(bound.Port)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrInet6{Addr: addr.As16()}); err != nil {
		return linuxFailure(err)
	}
	local, err := syscall.Getsockname(fd)
	if err != nil {
		return linuxFailure(err)
	}
	bound, ok := local.(*syscall.SockaddrInet6)
	if !ok {
		return failure(model.StatusError, "unexpected ICMPv6 source address")
	}
	source := netip.AddrFrom16(bound.Addr)
	// Linux fills the ICMPv6 checksum for both ping and ICMPv6 raw sockets.
	request := echoRequest6(identifier, 1, nonce)
	if ctx.Err() != nil {
		return failure(model.StatusError, "probe cancelled before ICMPv6 send")
	}
	started := time.Now()
	n, err := f.Write(request)
	if err != nil {
		return linuxFailure(err)
	}
	if n != len(request) {
		return failure(model.StatusError, "incomplete ICMPv6 request write")
	}
	buffer := make([]byte, 1500)
	for {
		n, err := f.Read(buffer)
		if err != nil {
			return icmpAfterSend(linuxFailure(err))
		}
		// IPv6 raw sockets return the ICMPv6 message without an IPv6 header.
		if matchesEchoReply6(buffer[:n], identifier, 1, nonce, addr, source) {
			latency := float64(time.Since(started)) / float64(time.Millisecond)
			return model.Measurement{Status: model.StatusOK, LatencyMS: &latency, Sent: 1, Received: 1, HasObservation: true}
		}
		if ctx.Err() != nil {
			return icmpAfterSend(failure(model.StatusTimeout, "ICMPv6 reply deadline reached"))
		}
	}
}
