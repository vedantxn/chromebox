// Package guestctl is the host side of the channel to the small agent inside
// the guest (cmd/chromebox-guest). The channel is vsock: a private pipe between
// host and guest that needs no IP address, so it works at the one moment the
// guest's network settings are wrong, right after a restore from a snapshot.
// Firecracker exposes the guest's vsock ports as a Unix socket on the host:
// connect, send "CONNECT <port>\n", and the stream is wired to the guest.
package guestctl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// Port is the guest agent's vsock port.
const Port = 5000

// Request is one command for the guest agent.
type Request struct {
	Cmd      string `json:"cmd"`                 // "set-identity" or "ping"
	Addr     string `json:"addr,omitempty"`      // set-identity: the guest's address with prefix, e.g. 10.200.1.2/30
	Gateway  string `json:"gateway,omitempty"`   // set-identity: the guest's default gateway
	UnixNano int64  `json:"unix_nano,omitempty"` // set-identity: the wall-clock time to set
	// set-identity: end the DevTools connections the snapshot was taken with.
	// Only a restore of a running browser (a fork image, a stored session) can
	// have any; looking costs the guest time, so a clone of a base skips it.
	DropClients bool `json:"drop_clients,omitempty"`
}

// Response is the guest agent's answer.
type Response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Rand     string `json:"rand,omitempty"`      // ping: 8 random bytes from the guest kernel, hex
	UnixNano int64  `json:"unix_nano,omitempty"` // ping: the guest's wall clock
}

// Call sends one request through the VM's vsock socket and returns the answer.
// Right after a restore the guest may not accept connections for a few
// milliseconds, so connecting is retried until ctx ends.
func Call(ctx context.Context, udsPath string, req Request) (Response, error) {
	var lastErr error
	for {
		resp, err := callOnce(ctx, udsPath, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return Response{}, fmt.Errorf("guest agent via %s: %w (last error: %v)", udsPath, ctx.Err(), lastErr)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func callOnce(ctx context.Context, udsPath string, req Request) (Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", udsPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	br := bufio.NewReader(conn)

	// Firecracker's handshake: ask to be connected to a guest port.
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", Port); err != nil {
		return Response{}, err
	}
	line, err := br.ReadString('\n')
	if err != nil {
		return Response{}, err
	}
	if !strings.HasPrefix(line, "OK") {
		return Response{}, fmt.Errorf("vsock handshake: %q", strings.TrimSpace(line))
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(br).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("guest agent: %s", resp.Error)
	}
	return resp, nil
}

// SetIdentity gives a freshly restored guest its own address and the correct
// time, and with dropClients ends the DevTools connections it was snapshotted
// with (see Request.DropClients).
func SetIdentity(ctx context.Context, udsPath, addr, gateway string, now time.Time, dropClients bool) error {
	_, err := Call(ctx, udsPath, Request{Cmd: "set-identity", Addr: addr, Gateway: gateway, UnixNano: now.UnixNano(), DropClients: dropClients})
	return err
}

// Ping checks that the guest agent answers and returns what it reports.
func Ping(ctx context.Context, udsPath string) (Response, error) {
	return Call(ctx, udsPath, Request{Cmd: "ping"})
}
