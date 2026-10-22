package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vedantxn/chromebox/internal/uffd"
	"github.com/vedantxn/chromebox/internal/vm"
)

// installChunk is how much memory the background path installs per call.
const installChunk = 2 << 20

// uffdCmd is the page-fault handler process for one VM:
//
//	chromebox uffd <id> file=<memory file> [delay=<ms>]
//
// It waits for Firecracker on the VM's uffd.sock, serves page faults from the
// memory file, and meanwhile installs every non-zero page in the background.
// When that is done it hands the memory back to the kernel and exits, writing
// what it did to uffd.json. delay holds the background back, to watch faults
// alone.
func uffdCmd(p vm.Paths, args []string) error {
	var file string
	var delay time.Duration
	for _, a := range args {
		k, v, _ := strings.Cut(a, "=")
		switch k {
		case "file":
			file = v
		case "delay":
			ms, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			delay = time.Duration(ms) * time.Millisecond
		default:
			return fmt.Errorf("uffd: unknown argument %q", a)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.UffdSock(), Net: "unix"})
	if err != nil {
		return err
	}
	_ = ln.SetDeadline(time.Now().Add(30 * time.Second))
	fd, regions, err := uffd.Receive(ln)
	ln.Close()
	os.Remove(p.UffdSock())
	if err != nil {
		return err
	}
	start := time.Now()
	fmt.Printf("firecracker connected: %d region(s) %+v\n", len(regions), regions)

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	mem, err := unix.Mmap(int(f.Fd()), 0, int(st.Size()), unix.PROT_READ, unix.MAP_SHARED)
	f.Close()
	if err != nil {
		return fmt.Errorf("map %s: %w", file, err)
	}
	ps := regions[0].PageSize
	h, err := uffd.New(fd, regions, func(off uint64) ([]byte, error) {
		if off+ps > uint64(len(mem)) {
			return nil, fmt.Errorf("offset %#x beyond the %d-byte memory file", off, len(mem))
		}
		return mem[off : off+ps], nil
	})
	if err != nil {
		return err
	}

	stop := make(chan struct{})
	faults := make(chan error, 1)
	go func() { faults <- h.ServeFaults(stop) }()
	time.Sleep(delay)
	for _, r := range regions {
		for off := r.Offset; off < r.Offset+r.Size; off += installChunk {
			n := min(uint64(installChunk), r.Offset+r.Size-off)
			if err := h.Install(off, mem[off:off+n]); err != nil {
				return err
			}
			select {
			case err := <-faults:
				return fmt.Errorf("fault loop stopped: %v", err)
			default:
			}
		}
	}
	if err := h.Finish(); err != nil {
		return err
	}
	close(stop)
	if err := <-faults; err != nil {
		return err
	}
	_ = h.Close()
	s := h.Stats()
	s.CompleteMs = float64(time.Since(start).Microseconds()) / 1000
	b, _ := json.Marshal(s)
	fmt.Printf("complete: %s\n", b)
	return os.WriteFile(filepath.Join(p.Dir, "uffd.json"), b, 0o644)
}
