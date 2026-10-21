package vm

import (
	"context"
	"fmt"
	"net"
	"time"
)

// WaitTCP blocks until a TCP connection to addr succeeds. Used to confirm
// that the host can reach the guest over the TAP, not just that the guest
// says it is ready.
func WaitTCP(ctx context.Context, addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		d := net.Dialer{Timeout: 200 * time.Millisecond}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			return conn.Close()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s not reachable: %w", addr, err)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
