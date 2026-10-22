package agent

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// preallocate gives path size bytes of real memory (it is on tmpfs) now, so
// that writing it later costs only the copy.
func preallocate(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.Fallocate(int(f.Fd()), 0, 0, size); err != nil {
		return fmt.Errorf("allocate %s: %w", path, err)
	}
	return nil
}
