//go:build linux

package probe

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestLinuxStatusClassification(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status string
	}{
		{syscall.EACCES, model.StatusPermission}, {syscall.EPERM, model.StatusPermission},
		{syscall.EPROTONOSUPPORT, model.StatusUnsupported}, {syscall.EAFNOSUPPORT, model.StatusUnsupported},
		{os.ErrDeadlineExceeded, model.StatusTimeout}, {syscall.ETIMEDOUT, model.StatusTimeout},
		{syscall.ENETUNREACH, model.StatusUnreachable}, {syscall.EHOSTUNREACH, model.StatusUnreachable},
		{syscall.ECONNREFUSED, model.StatusUnreachable}, {syscall.EHOSTDOWN, model.StatusUnreachable},
		{syscall.EINVAL, model.StatusError},
	} {
		m := linuxFailure(fmt.Errorf("wrapped: %w", tt.err))
		if m.Status != tt.status || m.LatencyMS != nil || m.Detail == "" {
			t.Fatalf("Linux error %s = %+v, want %s", tt.err, m, tt.status)
		}
	}
}

func TestNativePermissionDenied(t *testing.T) {
	if os.Getenv("PINGOBSERVATORY_PERMISSION_TEST") != "1" {
		t.Skip("set PINGOBSERVATORY_PERMISSION_TEST=1 on a host that denies ping sockets")
	}
	m := Probe(context.Background(), "127.0.0.1", time.Second)
	if m.Status != model.StatusPermission || m.At.IsZero() || m.LatencyMS != nil {
		t.Fatalf("native permission result = %+v", m)
	}
	t.Logf("native denied policy: status=%s detail=%s", m.Status, m.Detail)
}

func TestNativeTargetPermissionDeniedFamilies(t *testing.T) {
	if os.Getenv("PINGOBSERVATORY_PERMISSION_TEST") != "1" {
		t.Skip("requires an existing policy that denies ICMP sockets")
	}
	for _, family := range []string{model.FamilyIPv4, model.FamilyIPv6} {
		t.Run(family, func(t *testing.T) {
			m := ProbeTarget(context.Background(), model.Target{Host: fixtureAddress(family).String(), Measurement: fixtureSettings(model.MethodICMP, family)}, family)
			if m.Status != model.StatusPermission || m.Sent != 0 || m.Received != 0 || m.HasObservation || m.UnableToMeasure == 0 || len(m.Samples) != 0 {
				t.Fatalf("permission denial became a network failure: %+v", m)
			}
		})
	}
}
