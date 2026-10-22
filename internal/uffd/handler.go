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
	page     func(off uint64) ([]byte, error) // one page of the memory file

	mu        sync.Mutex
	faultTime []time.Duration
	zeroFault int
	installed uint64 // bytes installed by Install
}

func New(fd int, regions []Region, page func(off uint64) ([]byte, error)) (*Handler, error) {
	if len(regions) == 0 || regions[0].PageSize == 0 {
		return nil, fmt.Errorf("bad memory layout: %+v", regions)
	}
	return &Handler{fd: fd, regions: regions, pageSize: regions[0].PageSize, page: page}, nil
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
	data, err := h.page(off)
	if err != nil {
		return fmt.Errorf("page at %#x: %w", off, err)
	}
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
	h.mu.Lock()
	h.faultTime = append(h.faultTime, time.Since(start))
	if zero {
		h.zeroFault++
	}
	h.mu.Unlock()
	return nil
}

// Install puts the memory file's bytes data, which start at offset off, in
// place, skipping zero pages and pages that are already there. data must not
// cross a region's end.
func (h *Handler) Install(off uint64, data []byte) error {
	dst, left, ok := h.addr(off)
	if !ok || uint64(len(data)) > left {
		return fmt.Errorf("install %d bytes at %#x: outside guest memory", len(data), off)
	}
	ps := h.pageSize
	for i := uint64(0); i < uint64(len(data)); {
		if isZero(data[i : i+ps]) {
			i += ps
			continue
		}
		j := i + ps // extend the run of non-zero pages, to copy it in one call
		for j < uint64(len(data)) && !isZero(data[j:j+ps]) {
			j += ps
		}
		if err := CopyRange(h.fd, dst+i, data[i:j], ps); err != nil {
			return err
		}
		h.mu.Lock()
		h.installed += j - i
		h.mu.Unlock()
		i = j
	}
	return nil
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
