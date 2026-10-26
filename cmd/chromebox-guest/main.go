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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if n := dropBridged("/proc"); n > 0 {
		log.Printf("set-identity: dropped %d DevTools connection(s) from before the snapshot", n)
	}
	return nil
}

// dropBridged ends every connection chrome-bridge carries, by killing socat's
// forked children (one per connection), and returns how many it killed.
//
// Why: a restored copy (a fork's copy, a resumed session, a session from a
// profile) wakes up with the DevTools clients its parent had at the snapshot
// still connected, as far as Chrome knows. Their TCP connections now lead
// nowhere (the address they used is gone) and would take ~15 minutes to time
// out. Until then Chrome keeps their sessions, and a client like Playwright
// has asked for every new target to wait for it (waitForDebuggerOnStart), so
// the copy's navigations hang. Killed, the children close their end on
// 127.0.0.1:9222, Chrome sees the clients leave and lets the targets run.
// Nobody else is connected yet: the host sends set-identity before it hands
// the VM out, and a migration, which keeps its clients, never sends it.
//
// It reads /proc rather than running pkill: starting a process in the guest
// costs several milliseconds, and this is on the critical path of every start.
func dropBridged(proc string) int {
	n := 0
	for _, pid := range bridged(proc) {
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
	return n
}

// bridged returns the pids of socat's forked children: socat processes whose
// parent is socat (the listener, which stays).
func bridged(proc string) []int {
	stats, _ := filepath.Glob(filepath.Join(proc, "[0-9]*", "stat"))
	comm := map[int]string{}
	parent := map[int]int{}
	for _, f := range stats {
		b, err := os.ReadFile(f)
		if err != nil {
			continue // it exited
		}
		if pid, c, ppid, ok := parseStat(b); ok {
			comm[pid], parent[pid] = c, ppid
		}
	}
	var pids []int
	for pid, c := range comm {
		if c == "socat" && comm[parent[pid]] == "socat" {
			pids = append(pids, pid)
		}
	}
	return pids
}

// parseStat reads the pid, command name and parent pid from /proc/<pid>/stat:
// "pid (comm) state ppid ...". The name may itself hold spaces or ")", so it
// ends at the last ")".
func parseStat(b []byte) (pid int, comm string, ppid int, ok bool) {
	s := string(b)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return 0, "", 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return 0, "", 0, false
	}
	f := strings.Fields(s[end+1:]) // state, ppid, ...
	if len(f) < 2 {
		return 0, "", 0, false
	}
	ppid, err = strconv.Atoi(f[1])
	if err != nil {
		return 0, "", 0, false
	}
	return pid, s[open+1 : end], ppid, true
}
