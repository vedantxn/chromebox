package gateway

import (
	"encoding/binary"
	"time"
)

// CloudFront closes a WebSocket that carries no data for 10 minutes (measured:
// an idle Playwright connection was cut at 10.0 min). An agent that waits on a
// slow task would lose its browser. So the proxy sends a WebSocket ping to the
// client after keepaliveAfter of quiet; the client answers with a pong, and the
// traffic in both directions keeps CloudFront's connection open.
const keepaliveAfter = 30 * time.Second

// pingFrame is a WebSocket ping with no payload, as a server sends it
// (unmasked): FIN + opcode 9, length 0.
var pingFrame = []byte{0x89, 0x00}

// frameTracker follows frame boundaries in a WebSocket byte stream that is
// written in arbitrary chunks. A ping may only be sent between two frames;
// inserted inside one, it would corrupt it.
type frameTracker struct {
	hdr  []byte // header bytes of the current frame read so far
	left uint64 // payload bytes of the current frame still to come
}

func (f *frameTracker) feed(p []byte) {
	for len(p) > 0 {
		if f.left > 0 {
			n := min(uint64(len(p)), f.left)
			f.left -= n
			p = p[n:]
			continue
		}
		f.hdr = append(f.hdr, p[0])
		p = p[1:]
		if n := headerLen(f.hdr); n > 0 && len(f.hdr) == n {
			f.left = payloadLen(f.hdr)
			f.hdr = f.hdr[:0]
		}
	}
}

func (f *frameTracker) atBoundary() bool { return f.left == 0 && len(f.hdr) == 0 }

// headerLen is the length of the frame header that starts with h, or 0 while
// too little of it is known. Byte 1 holds the mask bit and a 7-bit length; 126
// and 127 mean a 2- or 8-byte length follows; a masked frame adds a 4-byte key.
func headerLen(h []byte) int {
	if len(h) < 2 {
		return 0
	}
	n := 2
	switch h[1] & 0x7f {
	case 126:
		n += 2
	case 127:
		n += 8
	}
	if h[1]&0x80 != 0 {
		n += 4
	}
	return n
}

func payloadLen(h []byte) uint64 {
	switch l := h[1] & 0x7f; l {
	case 126:
		return uint64(binary.BigEndian.Uint16(h[2:4]))
	case 127:
		return binary.BigEndian.Uint64(h[2:10])
	default:
		return uint64(l)
	}
}
