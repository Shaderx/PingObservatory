//go:build linux

package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func nativeProbe(ctx context.Context, addr netip.Addr) model.Measurement {
	return nativeProbePayload(ctx, addr, 32)
}

func nativeProbePayload(ctx context.Context, addr netip.Addr, payloadBytes int) model.Measurement {
	// Prefer an unprivileged Linux ping socket. If the existing group policy
	// denies it, try a raw socket using only privileges already held by this
	// process. Do not elevate, change capabilities, or modify host policy.
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMP)
	raw := false
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		fd, err = syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMP)
		raw = true
	}
	if err != nil {
		return linuxFailure(err)
	}
	f := os.NewFile(uintptr(fd), "icmp")
	if f == nil {
		syscall.Close(fd)
		return failure(model.StatusError, "could not open ICMP socket")
	}
	defer f.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		return failure(model.StatusError, "missing probe deadline")
	}
	if err := f.SetDeadline(deadline); err != nil {
		return failure(model.StatusUnsupported, "ICMP socket deadlines unavailable")
	}
	// os.NewFile retains nonblocking mode and uses Go's runtime poller for
	// socket deadlines. Context cancellation wakes a pending read or write.
	stop := context.AfterFunc(ctx, func() { _ = f.SetDeadline(time.Now()) })
	defer stop()
	// Receive socket errors for ICMP failures instead of converting an
	// explicit unreachable response into an apparent reply timeout.
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_RECVERR, 1); err != nil {
		return linuxFailure(err)
	}
	if payloadBytes < 16 || payloadBytes > 300 {
		return failure(model.StatusUnsupported, "ICMP payload must be 16..300 bytes")
	}
	nonce := make([]byte, payloadBytes)
	if _, err := rand.Read(nonce); err != nil {
		return failure(model.StatusError, "could not create ICMP request token")
	}
	identifier := binary.BigEndian.Uint16(nonce[:2])
	if !raw {
		if err := syscall.Bind(fd, &syscall.SockaddrInet4{}); err != nil {
			return linuxFailure(err)
		}
		local, err := syscall.Getsockname(fd)
		if err != nil {
			return linuxFailure(err)
		}
		bound, ok := local.(*syscall.SockaddrInet4)
		if !ok {
			return failure(model.StatusError, "unexpected ICMP socket address")
		}
		// Linux chooses the ICMP identifier from the bound ping socket's port.
		identifier = uint16(bound.Port)
	}
	if err := syscall.Connect(fd, &syscall.SockaddrInet4{Addr: addr.As4()}); err != nil {
		return linuxFailure(err)
	}
	request := echoRequest(identifier, 1, nonce)
	if ctx.Err() != nil {
		return failure(model.StatusError, "probe cancelled before ICMP send")
	}
	started := time.Now()
	n, err := f.Write(request)
	if err != nil {
		return linuxFailure(err)
	}
	if n != len(request) {
		return failure(model.StatusError, "incomplete ICMP request write")
	}
	buffer := make([]byte, 1500)
	for {
		n, err := f.Read(buffer)
		if err != nil {
			return icmpAfterSend(linuxFailure(err))
		}
		packet := buffer[:n]
		if raw {
			packet = rawEchoPayload(packet, addr)
		}
		if matchesEchoReply(packet, identifier, 1, nonce) {
			latency := float64(time.Since(started)) / float64(time.Millisecond)
			return model.Measurement{Status: model.StatusOK, LatencyMS: &latency, Sent: 1, Received: 1, HasObservation: true}
		}
		// Unrelated or malformed packets do not reset the absolute deadline.
		if ctx.Err() != nil {
			return icmpAfterSend(failure(model.StatusTimeout, "ICMP reply deadline reached"))
		}
	}
}

func linuxFailure(err error) model.Measurement {
	status := model.StatusError
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return failure(model.StatusPermission, "Linux ICMP sockets are denied by the existing host policy")
	case errors.Is(err, syscall.EPROTONOSUPPORT), errors.Is(err, syscall.EAFNOSUPPORT), errors.Is(err, syscall.ESOCKTNOSUPPORT), errors.Is(err, syscall.ENOSYS):
		status = model.StatusUnsupported
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, syscall.ETIMEDOUT):
		status = model.StatusTimeout
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENETDOWN), errors.Is(err, syscall.EHOSTDOWN):
		status = model.StatusUnreachable
	}
	return failure(status, fmt.Sprintf("Linux ICMP: %s", err))
}
