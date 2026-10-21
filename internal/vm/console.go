package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"
)

// The counter guest prints one "count N" line every 100 ms to the serial
// console, which Firecracker writes to serial.log.
var countLine = regexp.MustCompile(`(?m)^count (\d+)\r?\n`)

// LogSize is the current length of the VM's serial log in bytes. Take it
// before an action, then pass it to WaitForCount to ignore everything older.
func LogSize(p Paths) int64 {
	st, err := os.Stat(p.SerialLog())
	if err != nil {
		return 0
	}
	return st.Size()
}

// LastCount returns the highest count the guest has printed so far.
func LastCount(p Paths) (int, error) {
	b, err := os.ReadFile(p.SerialLog())
	if err != nil {
		return 0, err
	}
	m := countLine.FindAllSubmatch(b, -1)
	if len(m) == 0 {
		return 0, fmt.Errorf("no count lines in %s", p.SerialLog())
	}
	return strconv.Atoi(string(m[len(m)-1][1]))
}

// WaitForCount blocks until the guest prints a "count N" line after byte
// offset from, and returns N. That proves the guest's app is running, not
// merely that Firecracker accepted a command. On boot from=0; after a restore
// from is the log size taken beforehand, so old lines don't count.
func WaitForCount(ctx context.Context, p Paths, from int64, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	f, err := os.Open(p.SerialLog())
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return 0, err
	}

	var acc []byte // everything read since from, so a line split across reads still matches
	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			acc = append(acc, buf[:n]...)
			if m := countLine.FindSubmatch(acc); m != nil {
				return strconv.Atoi(string(m[1]))
			}
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		if n == 0 {
			select {
			case <-ctx.Done():
				return 0, fmt.Errorf("no count line after offset %d in %s: %w", from, p.SerialLog(), ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
	}
}
