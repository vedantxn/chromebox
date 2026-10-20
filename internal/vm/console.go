package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// WaitForConsole blocks until marker appears in the VM's serial log, i.e.
// until the guest prints it. This is how the host knows the guest's app is
// actually running, not just that Firecracker accepted InstanceStart.
func WaitForConsole(ctx context.Context, p Paths, marker string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	f, err := os.Open(p.SerialLog())
	if err != nil {
		return err
	}
	defer f.Close()

	// Read only the bytes appended since the last poll. Keep the tail of the
	// previous read so a marker split across two reads is still found.
	want := []byte(marker)
	var tail []byte
	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := append(tail, buf[:n]...)
			if bytes.Contains(chunk, want) {
				return nil
			}
			keep := min(len(want)-1, len(chunk))
			tail = append([]byte(nil), chunk[len(chunk)-keep:]...)
		}
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%q never appeared in %s: %w", marker, p.SerialLog(), ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
	}
}
