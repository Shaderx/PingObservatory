//go:build !windows && !linux

package probe

import (
	"context"
	"net/netip"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func nativeProbe(context.Context, netip.Addr) model.Measurement {
	return failure(model.StatusUnsupported, "native ICMP probing supports Windows and Linux; macOS is deferred")
}
