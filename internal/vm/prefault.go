package vm

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Prefault reads the whole of a running VM's guest memory once through
// /proc/<pid>/mem, which makes the kernel fill in Firecracker's page tables
// for it. A VM made from a base maps the base's memory file but only touches
// the pages it uses (~320 of 1024 MiB for a fresh browser), so the first
// snapshot, which reads every page, took a page fault per untouched page
// inside the pause: 247-376 ms against ~200 ms for a VM whose memory was all
// mapped (m6.md). Doing it before the pause costs the same work, but with the
// VM still running. size is the guest memory's size, which picks the mapping.
func Prefault(p Paths, size int64) error {
	b, err := os.ReadFile(p.PIDFile())
	if err != nil {
		return err
	}
	pid := strings.TrimSpace(string(b))
	maps, err := os.Open("/proc/" + pid + "/maps")
	if err != nil {
		return err
	}
	defer maps.Close()
	var start int64 = -1
	sc := bufio.NewScanner(maps)
	for sc.Scan() {
		lo, hi, ok := strings.Cut(strings.Fields(sc.Text())[0], "-")
		if !ok {
			continue
		}
		a, err1 := strconv.ParseInt(lo, 16, 64)
		z, err2 := strconv.ParseInt(hi, 16, 64)
		if err1 == nil && err2 == nil && z-a == size {
			start = a
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("no %d-byte guest memory mapping in firecracker %s", size, pid)
	}
	mem, err := os.Open("/proc/" + pid + "/mem")
	if err != nil {
		return err
	}
	defer mem.Close()
	buf := make([]byte, 4<<20)
	for off := int64(0); off < size; off += int64(len(buf)) {
		if _, err := mem.ReadAt(buf, start+off); err != nil {
			return fmt.Errorf("read guest memory at %#x: %w", start+off, err)
		}
	}
	return nil
}
