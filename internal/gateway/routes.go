package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"

	"github.com/vedantxn/chromebox/internal/remote"
)

// SetRoute points a VM's address block at the tunnel to the node that holds
// it. `replace` is atomic: one instant the packets go to the old node, the
// next to the new one, with no moment in between when the route is missing.
// Migrating a VM is, from the network's point of view, one call to this.
func SetRoute(ctx context.Context, gw *remote.Remote, block netip.Prefix, tunnel string) error {
	_, err := gw.Run(ctx, "sudo", "ip", "route", "replace", block.String(), "dev", tunnel)
	return err
}

// DelRoute removes a VM's route. A route that is already gone is not an error.
func DelRoute(ctx context.Context, gw *remote.Remote, block netip.Prefix) error {
	_, err := gw.Run(ctx, "sudo", "ip", "route", "del", block.String())
	if err != nil {
		return nil
	}
	return nil
}

// rtoMin is the shortest TCP retransmission timeout for connections to a VM.
// Linux's default floor is 200 ms, and each retry doubles the wait (0.2, 0.6,
// 1.4, 3.0 s after the loss), so a connection that lost packets while its VM
// was paused for half a second next tries at 0.6 or 1.4 s: the stall is set by
// the retry schedule, not by the outage (m2.md). The gateway is under a
// millisecond from every VM, so a 20 ms floor is still far above the real
// round trip, and the retries land at 20, 60, 140, 300, 620 ms...
const rtoMin = "20ms"

// SetRouteLocal is SetRoute for the machine that is itself the gateway (the
// control-plane host, where the orchestrator runs): no ssh, just `ip`, run
// with the orchestrator's own CAP_NET_ADMIN (no sudo). The route also carries
// rtoMin for the proxy's connections to the VM.
func SetRouteLocal(ctx context.Context, block netip.Prefix, tunnel string) error {
	out, err := exec.CommandContext(ctx, "ip", "route", "replace", block.String(), "dev", tunnel, "rto_min", rtoMin).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip route replace %s dev %s: %w: %s", block, tunnel, err, bytes.TrimSpace(out))
	}
	return nil
}

// DelRouteLocal removes a VM's route on the local machine. A route that is
// already gone is not an error.
func DelRouteLocal(ctx context.Context, block netip.Prefix) error {
	_ = exec.CommandContext(ctx, "ip", "route", "del", block.String()).Run()
	return nil
}
