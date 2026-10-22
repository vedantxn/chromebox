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

	"github.com/vedantxn/chromebox/internal/pageserver"
	"github.com/vedantxn/chromebox/internal/uffd"
	"github.com/vedantxn/chromebox/internal/vm"
)

// installChunk is how much memory the background path installs per call.
const installChunk = 2 << 20

// uffdCmd is the page-fault handler process for one VM:
//
//	chromebox uffd <id> file=<memory file> [delay=<ms>] [fault_kib=<n>]
//	chromebox uffd <id> source=<host:port> token=<t> [fault_kib=<n>]
//
// It waits for Firecracker on the VM's uffd.sock and serves page faults, while
// every non-zero page is installed in the background: from a local memory file
// (a restore), or from the page server of the node the VM migrated from
// (post-copy). Then it hands the memory back to the kernel, tells the source it
// is done, and exits, writing what it did to uffd.json. delay holds the
// background back, to watch faults alone; fault_kib is how much one fault
// fetches.
func uffdCmd(p vm.Paths, args []string) (err error) {
	var file, source, token string
	var delay time.Duration
	// A fault fetches 64 KiB around the page: Chrome waking on a new node
	// touched 812-1953 pages one by one at ~200 us each, but only 416 blocks
	// of 64 KiB, and answered ~100 ms sooner (m5.md, C2).
	faultBytes := uint64(64 << 10)
	for _, a := range args {
		k, v, _ := strings.Cut(a, "=")
		switch k {
		case "file":
			file = v
		case "source":
			source = v
		case "token":
			token = v
		case "delay", "fault_kib":
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			if k == "delay" {
				delay = time.Duration(n) * time.Millisecond
			} else {
				faultBytes = uint64(n) << 10
			}
		default:
			return fmt.Errorf("uffd: unknown argument %q", a)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.UffdSock(), Net: "unix"})
	if err != nil {
		return err
	}
	// Connect to the source while Firecracker gets ready, so the first fault
	// does not also pay for the TCP handshakes.
	var pages *pageserver.Client
	dialed := make(chan error, 1)
	if source != "" {
		go func() {
			var err error
			pages, err = pageserver.Dial(source, token)
			dialed <- err
		}()
	} else {
		dialed <- nil
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
	// From here the VM runs on memory only this process can complete. If it
	// can't (the source is gone), a guest left waiting on a missing page would
	// hang forever with its clients still connected; kill it instead, so the
	// orchestrator sees it vanish and marks it lost (M5, C3).
	defer func() {
		if err != nil {
			fmt.Printf("cannot complete the memory, killing the VM: %v\n", err)
			_ = vm.Kill(p)
		}
	}()
	if err := <-dialed; err != nil {
		return fmt.Errorf("connect to the source's page server: %w", err)
	}

	var h *uffd.Handler
	var background func() error
	if source != "" {
		buf := make([]byte, faultBytes)
		h, err = uffd.New(fd, regions, func(off uint64) (uint64, []byte, error) {
			at, n := h.Block(off, faultBytes)
			return at, buf[:n], pages.Fetch(at, buf[:n])
		})
		background = func() error { return pages.Stream(h.Install, h.InstallZero) }
	} else {
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
		h, err = uffd.New(fd, regions, func(off uint64) (uint64, []byte, error) {
			at, n := h.Block(off, faultBytes)
			if at+n > uint64(len(mem)) {
				return 0, nil, fmt.Errorf("offset %#x beyond the %d-byte memory file", at, len(mem))
			}
			return at, mem[at : at+n], nil
		})
		if err != nil {
			return err
		}
		background = func() error {
			time.Sleep(delay)
			for _, r := range regions {
				for off := r.Offset; off < r.Offset+r.Size; off += installChunk {
					n := min(uint64(installChunk), r.Offset+r.Size-off)
					if err := h.Install(off, mem[off:off+n]); err != nil {
						return err
					}
				}
			}
			return nil
		}
	}
	if err != nil {
		return err
	}

	stop := make(chan struct{})
	faults := make(chan error, 1)
	go func() { faults <- h.ServeFaults(stop) }()
	bg := make(chan error, 1)
	go func() { bg <- background() }()
	select {
	case err := <-faults:
		return fmt.Errorf("fault loop stopped: %v", err)
	case err := <-bg:
		if err != nil {
			return fmt.Errorf("background install: %w", err)
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
	// The VM holds all its memory now; nothing below may fail it.
	if pages != nil {
		if err := pages.Ack(); err != nil {
			fmt.Printf("could not tell the source we are done (it will time out): %v\n", err)
		}
		pages.Close()
	}
	s := h.Stats()
	s.CompleteMs = float64(time.Since(start).Microseconds()) / 1000
	b, _ := json.Marshal(s)
	fmt.Printf("complete: %s\n", b)
	_ = os.WriteFile(filepath.Join(p.Dir, "uffd.json"), b, 0o644)
	return nil
}
