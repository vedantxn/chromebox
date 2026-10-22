package gateway

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func frame(payload int, masked bool) []byte {
	var b bytes.Buffer
	b.WriteByte(0x81) // FIN + text
	m := byte(0)
	if masked {
		m = 0x80
	}
	switch {
	case payload < 126:
		b.WriteByte(m | byte(payload))
	case payload < 1<<16:
		b.WriteByte(m | 126)
		_ = binary.Write(&b, binary.BigEndian, uint16(payload))
	default:
		b.WriteByte(m | 127)
		_ = binary.Write(&b, binary.BigEndian, uint64(payload))
	}
	if masked {
		b.Write([]byte{1, 2, 3, 4})
	}
	b.Write(make([]byte, payload))
	return b.Bytes()
}

func TestFrameTracker(t *testing.T) {
	stream := bytes.Join([][]byte{frame(0, false), frame(5, false), frame(300, true), frame(70000, false)}, nil)
	ends := map[int]bool{}
	off := 0
	for _, n := range []int{2, 7, 2 + 2 + 4 + 300, 2 + 8 + 70000} {
		off += n
		ends[off] = true
	}
	// Feed the stream in every chunk size from 1 to 9 bytes: the tracker must
	// be at a boundary exactly where a frame ends, wherever the chunks split.
	for chunk := 1; chunk <= 9; chunk++ {
		var f frameTracker
		for i := 0; i < len(stream); i += chunk {
			j := min(i+chunk, len(stream))
			f.feed(stream[i:j])
			if f.atBoundary() != ends[j] {
				t.Fatalf("chunk %d: atBoundary=%v at %d, want %v", chunk, f.atBoundary(), j, ends[j])
			}
		}
	}
	// And never at a boundary mid-frame.
	var f frameTracker
	for i := range stream {
		f.feed(stream[i : i+1])
		if f.atBoundary() != ends[i+1] {
			t.Fatalf("byte %d: atBoundary=%v, want %v", i+1, f.atBoundary(), ends[i+1])
		}
	}
}
