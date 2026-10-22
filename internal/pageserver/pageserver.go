// Package pageserver moves a paused VM's memory to the node it migrated to,
// after the VM is already running there (post-copy). The source serves its
// snapshot's memory file; the target's page-fault handler connects twice:
//
//   - fault connection: "send me n bytes at offset off", answered at once. A
//     vCPU on the target is stopped until the reply arrives, so nothing else
//     ever queues on this connection.
//   - stream connection: the source sends every run of pages, in order (a
//     zero run as a header only), then an end marker. The target installs them as they come, and
//     acknowledges once its handler has finished: from then on the VM needs
//     nothing from the source, which may free its copy.
//
// Zero pages never travel: a zero run is only its header.
package pageserver

import (
	"bufio"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	kindFault  = 'F'
	kindStream = 'S'

	chunk    = 2 << 20 // the stream scans memory in pieces of this size
	pageSize = 4096
	zeroRun  = 1 << 31 // set in a stream record's length: the run is all zeros, no bytes follow
)

// Server serves one migration's memory.
type Server struct {
	ln    net.Listener
	token string
	allow string // the only remote IP accepted: the target node

	mu   sync.Mutex
	mem  []byte
	sent int64 // bytes streamed

	done chan error // the target acknowledged the end of the stream (or failed)
	once sync.Once
}

// Listen reserves a port before the pause, so the address can be handed to
// the target; nothing is served until Start.
func Listen(ip, token, allow string) (*Server, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln, token: token, allow: allow, done: make(chan error, 1)}, nil
}

func (s *Server) Addr() string  { return s.ln.Addr().String() }
func (s *Server) Token() string { return s.token }

// Start serves mem (the memory file, mapped) until Close.
func (s *Server) Start(mem []byte) {
	s.mu.Lock()
	s.mem = mem
	s.mu.Unlock()
	go func() {
		for {
			c, err := s.ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
}

func (s *Server) finish(err error) { s.once.Do(func() { s.done <- err }) }

// Wait returns once the target has every page, or after timeout.
func (s *Server) Wait(timeout time.Duration) error {
	select {
	case err := <-s.done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("target did not finish taking the memory within %s", timeout)
	}
}

// Sent is how many bytes the stream has sent so far.
func (s *Server) Sent() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

func (s *Server) Close() error { return s.ln.Close() }

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	if host, _, _ := net.SplitHostPort(c.RemoteAddr().String()); s.allow != "" && host != s.allow {
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, err := readHello(c, s.token)
	if err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	switch kind {
	case kindFault:
		s.serveFaults(c)
	case kindStream:
		s.finish(s.stream(c))
	}
}

func (s *Server) serveFaults(c net.Conn) {
	r := bufio.NewReader(c)
	var req [12]byte
	for {
		if _, err := io.ReadFull(r, req[:]); err != nil {
			return
		}
		off := binary.LittleEndian.Uint64(req[0:])
		n := uint64(binary.LittleEndian.Uint32(req[8:]))
		if off+n > uint64(len(s.mem)) || n == 0 {
			return
		}
		if _, err := c.Write(s.mem[off : off+n]); err != nil {
			return
		}
	}
}

// stream sends the memory as runs of pages, in order: a non-zero run as
// (offset, length, bytes), a zero run as (offset, length|zeroRun) with no
// bytes, so the target can map those pages without a round trip. Then (0, 0)
// to say that was all, then it waits for the target's one-byte ack.
func (s *Server) stream(c net.Conn) error {
	w := bufio.NewWriterSize(c, 1<<20)
	var hdr [12]byte
	mem := s.mem
	for base := 0; base < len(mem); base += chunk {
		end := min(base+chunk, len(mem))
		for i := base; i < end; {
			zero := isZero(mem[i : i+pageSize])
			j := i + pageSize
			for j < end && isZero(mem[j:j+pageSize]) == zero {
				j += pageSize
			}
			n := uint32(j - i)
			if zero {
				n |= zeroRun
			}
			binary.LittleEndian.PutUint64(hdr[0:], uint64(i))
			binary.LittleEndian.PutUint32(hdr[8:], n)
			if _, err := w.Write(hdr[:]); err != nil {
				return err
			}
			if !zero {
				if _, err := w.Write(mem[i:j]); err != nil {
					return err
				}
				s.mu.Lock()
				s.sent += int64(j - i)
				s.mu.Unlock()
			}
			i = j
		}
	}
	clear(hdr[:])
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	var ack [1]byte
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
	if _, err := io.ReadFull(c, ack[:]); err != nil {
		return fmt.Errorf("no acknowledgement from the target: %w", err)
	}
	return nil
}

// The hello is the token (length-prefixed) and one byte naming the connection.
func writeHello(c net.Conn, token string, kind byte) error {
	b := append([]byte{byte(len(token))}, token...)
	_, err := c.Write(append(b, kind))
	return err
}

func readHello(c net.Conn, token string) (byte, error) {
	var n [1]byte
	if _, err := io.ReadFull(c, n[:]); err != nil {
		return 0, err
	}
	b := make([]byte, int(n[0])+1)
	if _, err := io.ReadFull(c, b); err != nil {
		return 0, err
	}
	if subtle.ConstantTimeCompare(b[:n[0]], []byte(token)) != 1 {
		return 0, errors.New("bad token")
	}
	return b[n[0]], nil
}

// Client is the target's side: both connections to one source.
type Client struct {
	fault  net.Conn
	stream net.Conn
	mu     sync.Mutex // one fault request at a time
}

func Dial(addr, token string) (*Client, error) {
	var conns [2]net.Conn
	for i, kind := range []byte{kindFault, kindStream} {
		c, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			for _, o := range conns[:i] {
				o.Close()
			}
			return nil, err
		}
		if err := writeHello(c, token, kind); err != nil {
			c.Close()
			return nil, err
		}
		conns[i] = c
	}
	return &Client{fault: conns[0], stream: conns[1]}, nil
}

// Fetch fills buf with the memory at off.
func (c *Client) Fetch(off uint64, buf []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var req [12]byte
	binary.LittleEndian.PutUint64(req[0:], off)
	binary.LittleEndian.PutUint32(req[8:], uint32(len(buf)))
	if _, err := c.fault.Write(req[:]); err != nil {
		return err
	}
	_, err := io.ReadFull(c.fault, buf)
	return err
}

// Stream calls install for each non-zero run the source sends and zero for
// each zero run, until the end marker.
func (c *Client) Stream(install func(off uint64, data []byte) error, zero func(off, n uint64) error) error {
	r := bufio.NewReaderSize(c.stream, 1<<20)
	var hdr [12]byte
	buf := make([]byte, chunk)
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return err
		}
		off := binary.LittleEndian.Uint64(hdr[0:])
		n := binary.LittleEndian.Uint32(hdr[8:])
		if n == 0 {
			return nil
		}
		if n&zeroRun != 0 {
			if err := zero(off, uint64(n&^zeroRun)); err != nil {
				return err
			}
			continue
		}
		if int(n) > len(buf) {
			return fmt.Errorf("stream record of %d bytes is larger than a chunk", n)
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return err
		}
		if err := install(off, buf[:n]); err != nil {
			return err
		}
	}
}

// Ack tells the source the target needs nothing more from it.
func (c *Client) Ack() error {
	_, err := c.stream.Write([]byte{1})
	return err
}

func (c *Client) Close() {
	c.fault.Close()
	c.stream.Close()
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
