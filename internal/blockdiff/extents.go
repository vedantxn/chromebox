// Package blockdiff finds which parts of a disk image changed, without reading
// the image. The VM's disk is a copy-on-write clone (reflink) of a reference
// copy made earlier: blocks the VM has not written are physically shared with
// the reference, and a block it writes gets a new physical location. So
// comparing the two files' extent maps (where their blocks live on the
// device) tells us exactly what changed, in milliseconds, whatever the size
// of the image. No checksums, no scanning.
package blockdiff

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Extent is a run of a file's bytes stored contiguously on the device.
type Extent struct {
	Logical  uint64 // offset within the file
	Physical uint64 // offset on the device
	Length   uint64
}

const (
	fsIocFiemap  = 0xC020660B // FS_IOC_FIEMAP
	flagSync     = 0x1        // FIEMAP_FLAG_SYNC: write out delayed allocations first
	extLast      = 0x1        // FIEMAP_EXTENT_LAST
	extUnwritten = 0x800      // FIEMAP_EXTENT_UNWRITTEN: allocated but reads as zeros
	extDelalloc  = 0x4        // FIEMAP_EXTENT_DELALLOC: no device location yet

	headerSize = 32 // struct fiemap
	extentSize = 56 // struct fiemap_extent
	batch      = 1024
)

// Extents returns the file's mapped extents in order. Holes and unwritten
// extents (both read as zeros) are not included. SYNC makes the filesystem
// allocate space for data still waiting in memory, so every extent has a real
// device address.
func Extents(path string) ([]Extent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Extent
	start := uint64(0)
	buf := make([]byte, headerSize+extentSize*batch)
	for {
		for i := range buf[:headerSize] {
			buf[i] = 0
		}
		binary.LittleEndian.PutUint64(buf[0:], start)      // fm_start
		binary.LittleEndian.PutUint64(buf[8:], ^uint64(0)) // fm_length: to the end
		binary.LittleEndian.PutUint32(buf[16:], flagSync)  // fm_flags
		binary.LittleEndian.PutUint32(buf[24:], batch)     // fm_extent_count
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocFiemap, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
			return nil, fmt.Errorf("fiemap %s: %w", path, errno)
		}
		n := int(binary.LittleEndian.Uint32(buf[20:])) // fm_mapped_extents
		if n == 0 {
			return out, nil
		}
		last := false
		for i := 0; i < n; i++ {
			e := buf[headerSize+i*extentSize:]
			ext := Extent{
				Logical:  binary.LittleEndian.Uint64(e[0:]),
				Physical: binary.LittleEndian.Uint64(e[8:]),
				Length:   binary.LittleEndian.Uint64(e[16:]),
			}
			flags := binary.LittleEndian.Uint32(e[40:])
			if flags&extDelalloc != 0 {
				return nil, fmt.Errorf("fiemap %s: extent at %d has no device address", path, ext.Logical)
			}
			if flags&extUnwritten == 0 {
				out = append(out, ext)
			}
			if flags&extLast != 0 {
				last = true
			}
			start = ext.Logical + ext.Length
		}
		if last {
			return out, nil
		}
	}
}
