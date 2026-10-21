package blockdiff

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// maxChunk bounds how much of a range travels in one record.
const maxChunk = 1 << 20

// Send writes the given ranges of f to w as records: an 8-byte offset, a 4-byte
// length and that many bytes. A zero length ends the stream.
func Send(w io.Writer, f *os.File, ranges []Range) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	var hdr [12]byte
	buf := make([]byte, maxChunk)
	for _, r := range ranges {
		for done := uint64(0); done < r.Length; {
			n := r.Length - done
			if n > maxChunk {
				n = maxChunk
			}
			// ReadAt past the end or into a hole returns zeros / short reads;
			// a short read is padded with zeros, matching what the guest sees.
			got, err := f.ReadAt(buf[:n], int64(r.Offset+done))
			if err != nil && err != io.EOF {
				return err
			}
			for i := got; i < int(n); i++ {
				buf[i] = 0
			}
			binary.BigEndian.PutUint64(hdr[0:], r.Offset+done)
			binary.BigEndian.PutUint32(hdr[8:], uint32(n))
			if _, err := bw.Write(hdr[:]); err != nil {
				return err
			}
			if _, err := bw.Write(buf[:n]); err != nil {
				return err
			}
			done += n
		}
	}
	binary.BigEndian.PutUint64(hdr[0:], 0)
	binary.BigEndian.PutUint32(hdr[8:], 0)
	if _, err := bw.Write(hdr[:]); err != nil {
		return err
	}
	return bw.Flush()
}

// Apply reads records from r and writes each into the existing file at path.
func Apply(r io.Reader, path string) (int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(r, 1<<20)
	var hdr [12]byte
	buf := make([]byte, maxChunk)
	var total int64
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return total, fmt.Errorf("delta stream ended early: %w", err)
		}
		off := binary.BigEndian.Uint64(hdr[0:])
		n := binary.BigEndian.Uint32(hdr[8:])
		if n == 0 {
			return total, f.Sync()
		}
		if n > maxChunk {
			return total, fmt.Errorf("delta record of %d bytes", n)
		}
		if _, err := io.ReadFull(br, buf[:n]); err != nil {
			return total, err
		}
		if _, err := f.WriteAt(buf[:n], int64(off)); err != nil {
			return total, err
		}
		total += int64(n)
	}
}
