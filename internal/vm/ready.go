package vm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WaitRestored blocks until a VM that was just restored is demonstrably
// running again, and returns the counter value it resumed at (0 for images
// without a counter). from is the serial log length taken before the restore.
//
// The proof depends on what the guest runs. The counter prints a new count.
// The browser guest says nothing on its console after a restore, so the host
// asks it over the network: an HTTP answer from Chrome's CDP endpoint through
// the VM's TAP shows the whole chain (guest, virtual NIC, TAP) works.
func WaitRestored(ctx context.Context, p Paths, from int64, timeout time.Duration) (int, error) {
	_, img, err := ImageOf(p)
	if err != nil {
		return 0, err
	}
	if img.Counter {
		return WaitForCount(ctx, p, from, timeout)
	}
	nn, err := NetOf(p)
	if err != nil {
		return 0, err
	}
	if nn == nil || img.CDPPort == 0 {
		return 0, fmt.Errorf("cannot tell whether %s is running: image has no counter and no network", p.Dir)
	}
	url := fmt.Sprintf("http://%s:%d/json/version", nn.GuestIP, img.CDPPort)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return 0, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("%s never answered after the restore: %w", url, ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
