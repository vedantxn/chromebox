package gateway

import (
	"context"
	"net/netip"

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
