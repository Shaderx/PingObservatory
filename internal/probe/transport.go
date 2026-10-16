package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func networkFor(protocol string, addr netip.Addr) string {
	if addr.Is4() {
		return protocol + "4"
	}
	return protocol + "6"
}

func endpoint(addr netip.Addr, port int) string {
	return net.JoinHostPort(addr.String(), strconv.Itoa(port))
}

func operationFailure(err error, sent int, stage string) model.Measurement {
	m := failure(model.StatusError, err.Error())
	m.Attempted, m.Sent, m.FailureStage = 1, sent, stage
	m.UnobservedSent = sent
	switch {
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		m.Status = model.StatusPermission
	case errors.Is(err, syscall.EAFNOSUPPORT), errors.Is(err, syscall.EPROTONOSUPPORT):
		m.Status = model.StatusUnsupported
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		m.Status = model.StatusUnreachable // local no-route is not packet loss
	case errors.Is(err, syscall.ECONNREFUSED):
		m.Status, m.Sent, m.UnableToMeasure, m.HasObservation = model.StatusUnreachable, 1, 0, true
		m.UnobservedSent = 0
	default:
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
			m.Status = model.StatusTimeout
			if sent > 0 {
				m.UnableToMeasure, m.HasObservation = 0, true
				m.UnobservedSent = 0
			}
		}
	}
	return m
}

func probeTCP(ctx context.Context, addr netip.Addr, port int, p model.ProbeSettings) model.Measurement {
	call, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	started := time.Now()
	conn, err := (&net.Dialer{}).DialContext(call, networkFor("tcp", addr), endpoint(addr, port))
	ended := time.Now()
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		m := operationFailure(err, 0, "connect")
		// Numeric connect timeout is a failed network operation, not a count
		// of TCP packets. Permission/setup/no-route failures remain unable.
		if m.Status == model.StatusTimeout {
			m.Sent, m.UnableToMeasure, m.HasObservation = 1, 0, true
		}
		return m
	}
	if conn == nil {
		return failure(model.StatusError, "TCP dialer returned no connection")
	}
	ms := float64(ended.Sub(started)) / float64(time.Millisecond)
	return model.Measurement{Status: model.StatusOK, Attempted: 1, Sent: 1, Received: 1, HasObservation: true, LatencyMS: &ms,
		Samples: []model.RTTSample{{At: ended.UTC(), LatencyMS: ms, Sequence: 1}}, Timings: map[string]float64{"connect": ms}}
}
