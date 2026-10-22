package uffd

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Handler owns one VM's memory while it fills in. Two paths install pages:
//
//   - faults: the guest touched a missing page and its vCPU is stopped until
//     the page is there, so this path is latency-critical and does one page;
//   - Install: everything else, in big chunks, so that the handler can finish.
//
// Both may race for the same page; whichever loses gets EEXIST, which is fine.
// Finish then gives the memory back to the kernel: pages never installed are
// zero pages, and the kernel fills those itself on first touch, so zero pages
// never have to be sent or installed at all.
type Handler struct {
	fd       int
	regions  []Region
	pageSize uint64
	// fetch returns bytes of the memory file around offset off: a block that
	// holds the page at off, and the block's own offset. A block bigger than a
	// page brings the neighbours in with the same round trip.
	fetch func(off uint64) (uint64, []byte, error)

	mu        sync.Mutex
	faultTime []time.Duration
	zeroFault int
	installed uint64 // bytes installed by Install
}

func New(fd int, regions []Region, fetch func(off uint64) (uint64, []byte, error)) (*Handler, error) {
	if len(regions) == 0 || regions[0].PageSize == 0 {
		return nil, fmt.Errorf("bad memory layout: %+v", regions)
	}
	return &Handler{fd: fd, regions: regions, pageSize: regions[0].PageSize, fetch: fetch}, nil
}

// Block returns the aligned block of n bytes holding off, clipped to the end
// of its region: what a fault should ask for.
func (h *Handler) Block(off, n uint64) (uint64, uint64) {
	start := off &^ (n - 1)
	for _, r := range h.regions {
		if off >= r.Offset && off < r.Offset+r.Size {
			start = max(start, r.Offset)
			return start, min(n, r.Offset+r.Size-start)
		}
	}
	return off, h.pageSize
}

func (h *Handler) PageSize() uint64  { return h.pageSize }
func (h *Handler) Regions() []Region { return h.regions }

// addr maps an offset in the memory file to the guest page's address in
// Firecracker, and says how many bytes of the region are left from there.
func (h *Handler) addr(off uint64) (uint64, uint64, bool) {
	for _, r := range h.regions {
		if off >= r.Offset && off < r.Offset+r.Size {
			return r.BaseHostVirtAddr + off - r.Offset, r.Offset + r.Size - off, true
		}
	}
	return 0, 0, false
}

// offset is the reverse: a faulting address to its offset in the memory file.
func (h *Handler) offset(addr uint64) (uint64, bool) {
	for _, r := range h.regions {
		if addr >= r.BaseHostVirtAddr && addr < r.BaseHostVirtAddr+r.Size {
			return r.Offset + addr - r.BaseHostVirtAddr, true
		}
	}
	return 0, false
}

// ServeFaults answers page faults until stop is closed.
func (h *Handler) ServeFaults(stop <-chan struct{}) error {
	buf := make([]byte, 16*msgSize)
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		evs, err := ReadEvents(h.fd, 50, buf)
		if err != nil {
			return err
		}
		for _, ev := range evs {
			if err := h.fault(ev.Addr); err != nil {
				return err
			}
		}
	}
}

func (h *Handler) fault(addr uint64) error {
	start := time.Now()
	addr &^= h.pageSize - 1
	off, ok := h.offset(addr)
	if !ok {
		return fmt.Errorf("fault at %#x is outside guest memory", addr)
	}
	at, block, err := h.fetch(off)
	if err != nil {
		return fmt.Errorf("page at %#x: %w", off, err)
	}
	data := block[off-at : off-at+h.pageSize]
	zero := isZero(data)
	if zero {
		err = Zero(h.fd, addr, h.pageSize)
	} else {
		_, err = Copy(h.fd, addr, data)
	}
	if errors.Is(err, unix.EEXIST) {
		err = Wake(h.fd, addr, h.pageSize) // Install got there first
	}
	if err != nil {
		return fmt.Errorf("install page %#x: %w", addr, err)
	}
	elapsed := time.Since(start) // the vCPU is running again; the rest is extra
	if len(block) > int(h.pageSize) {
		if err := h.Install(at, block); err != nil {
			return err
		}
	}
	h.mu.Lock()
	h.faultTime = append(h.faultTime, elapsed)
	if zero {
		h.zeroFault++
	}
	h.mu.Unlock()
	return nil
}

// Install puts the memory file's bytes data, which start at offset off, in
// place: runs of non-zero pages are copied, runs of zero pages are mapped to
// the zero page (no copy, no memory), and pages already there are skipped.
// Mapping zero pages matters: left missing, each one costs the guest a round
// trip to the source just to learn it is zero (718 of 2,869 faults in C2).
// data must not cross a region's end.
func (h *Handler) Install(off uint64, data []byte) error {
	dst, left, ok := h.addr(off)
	if !ok || uint64(len(data)) > left {
		return fmt.Errorf("install %d bytes at %#x: outside guest memory", len(data), off)
	}
	ps := h.pageSize
	for i := uint64(0); i < uint64(len(data)); {
		zero := isZero(data[i : i+ps])
		j := i + ps // extend the run of pages of the same kind, to install it in one call
		for j < uint64(len(data)) && isZero(data[j:j+ps]) == zero {
			j += ps
		}
		var err error
		if zero {
			err = ZeroRange(h.fd, dst+i, j-i, ps)
		} else {
			err = CopyRange(h.fd, dst+i, data[i:j], ps)
			h.mu.Lock()
			h.installed += j - i
			h.mu.Unlock()
		}
		if err != nil {
			return err
		}
		i = j
	}
	return nil
}

// InstallZero maps zero pages over n bytes of the memory file from off.
func (h *Handler) InstallZero(off, n uint64) error {
	dst, left, ok := h.addr(off)
	if !ok || n > left {
		return fmt.Errorf("zero %d bytes at %#x: outside guest memory", n, off)
	}
	return ZeroRange(h.fd, dst, n, h.pageSize)
}

// Finish hands every region back to the kernel. Call it once all non-zero
// pages are installed; after it the VM needs no handler.
func (h *Handler) Finish() error {
	for _, r := range h.regions {
		if err := Unregister(h.fd, r.BaseHostVirtAddr, r.Size); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) Close() error { return unix.Close(h.fd) }

// Stats is what the handler did, for timing a migration.
type Stats struct {
	Faults       int     `json:"faults"`
	ZeroFaults   int     `json:"zero_faults"`
	FaultP50Us   float64 `json:"fault_p50_us"`
	FaultP99Us   float64 `json:"fault_p99_us"`
	FaultMaxUs   float64 `json:"fault_max_us"`
	InstalledMiB float64 `json:"installed_mib"`
	CompleteMs   float64 `json:"complete_ms"` // from connection to Finish
}

func (h *Handler) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := Stats{Faults: len(h.faultTime), ZeroFaults: h.zeroFault, InstalledMiB: float64(h.installed) / (1 << 20)}
	if n := len(h.faultTime); n > 0 {
		d := append([]time.Duration(nil), h.faultTime...)
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		us := func(x time.Duration) float64 { return float64(x.Microseconds()) }
		s.FaultP50Us, s.FaultP99Us, s.FaultMaxUs = us(d[n/2]), us(d[n*99/100]), us(d[n-1])
	}
	return s
}

func isZero(b []byte) bool {
	for len(b) >= 8 {
		if *(*uint64)(unsafe.Pointer(&b[0])) != 0 {
			return false
		}
		b = b[8:]
	}
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
