//go:build !windows && !linux

package probe

import (
	"context"
	"net/netip"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func nativeProbe6(context.Context, netip.Addr) model.Measurement {
	return failure(model.StatusUnsupported, "native ICMP probing supports Windows and Linux; macOS is deferred")
}

func nativeProbe6Payload(ctx context.Context, addr netip.Addr, _ int) model.Measurement {
	return nativeProbe6(ctx, addr)
}
