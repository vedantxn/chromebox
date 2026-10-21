// Command chromebox-guest runs inside the VM. It listens on vsock for commands
// from the host and does the two things a restored guest cannot do for itself:
// take a new network identity, and learn what time it is.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
)

const port = 5000

type request struct {
	Cmd      string `json:"cmd"`
	Addr     string `json:"addr"`
	Gateway  string `json:"gateway"`
	UnixNano int64  `json:"unix_nano"`
}

type response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Rand     string `json:"rand,omitempty"`
	UnixNano int64  `json:"unix_nano,omitempty"`
}

func main() {
	// A listening vsock socket survives a snapshot and restore; only the
	// connections that were open at the time are reset.
	l, err := vsock.Listen(port, nil)
	if err != nil {
		log.Fatalf("listen vsock %d: %v", port, err)
	}
	log.Printf("chromebox-guest listening on vsock port %d", port)
	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(c)
	}
}

func handle(c net.Conn) {
	defer c.Close()
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		reply(c, response{Error: "bad request: " + err.Error()})
		return
	}
	switch req.Cmd {
	case "set-identity":
		if err := setIdentity(req); err != nil {
			reply(c, response{Error: err.Error()})
			return
		}
		reply(c, response{OK: true})
	case "ping":
		b := make([]byte, 8)
		_, _ = rand.Read(b) // getrandom: the kernel's generator, which a restore must reseed
		reply(c, response{OK: true, Rand: hex.EncodeToString(b), UnixNano: time.Now().UnixNano()})
	default:
		reply(c, response{Error: "unknown command " + req.Cmd})
	}
}

func reply(c net.Conn, r response) { _ = json.NewEncoder(c).Encode(r) }

// setIdentity replaces eth0's address and default route, and sets the clock.
// The old address belonged to whichever VM the snapshot was taken from.
func setIdentity(r request) error {
	if r.Addr == "" || r.Gateway == "" {
		return fmt.Errorf("addr and gateway are required")
	}
	// One `ip` process for all four steps: starting a process in the guest costs
	// several milliseconds each, and this is on the critical path of every start.
	batch := fmt.Sprintf("addr flush dev eth0\naddr add %s dev eth0\nlink set eth0 up\nroute replace default via %s dev eth0\n",
		r.Addr, r.Gateway)
	cmd := exec.Command("ip", "-4", "-batch", "-")
	cmd.Stdin = strings.NewReader(batch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip: %w: %s", err, out)
	}
	if r.UnixNano != 0 {
		tv := syscall.NsecToTimeval(r.UnixNano)
		if err := syscall.Settimeofday(&tv); err != nil {
			return fmt.Errorf("set clock: %w", err)
		}
	}
	// One packet to the gateway makes it learn this guest's address and MAC
	// (ARP) before any real traffic arrives.
	go exec.Command("ping", "-c1", "-W1", r.Gateway).Run()
	return nil
}
