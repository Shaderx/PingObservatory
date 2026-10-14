//go:build windows

package probe

import (
	"testing"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func TestWindowsStatusClassification(t *testing.T) {
	for _, tt := range []struct {
		code   uint32
		status string
	}{
		{5, model.StatusPermission}, {10013, model.StatusPermission},
		{50, model.StatusUnsupported}, {10047, model.StatusUnsupported},
		{11010, model.StatusTimeout}, {10060, model.StatusTimeout},
		{11002, model.StatusUnreachable}, {11003, model.StatusUnreachable},
		{11004, model.StatusUnreachable}, {11005, model.StatusUnreachable},
		{11013, model.StatusUnreachable}, {11014, model.StatusUnreachable},
		{11050, model.StatusError}, {87, model.StatusError},
	} {
		m := windowsFailure(tt.code)
		if m.Status != tt.status || m.LatencyMS != nil || m.Detail == "" {
			t.Fatalf("Windows status %d = %+v, want %s", tt.code, m, tt.status)
		}
	}
}
