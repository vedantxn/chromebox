package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"

	"golang.org/x/sys/unix"

	"github.com/vedantxn/chromebox/internal/blockdiff"
	"github.com/vedantxn/chromebox/internal/vm"
)

// routeTo returns this node's address on the path to the agent at agentURL,
// and that agent's host. Dialing UDP sends nothing; it only asks the kernel
// which local address it would use.
func routeTo(agentURL string) (local, remote string, err error) {
	u, err := url.Parse(agentURL)
	if err != nil {
		return "", "", err
	}
	c, err := net.Dial("udp", net.JoinHostPort(u.Hostname(), "7000"))
	if err != nil {
		return "", "", err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), u.Hostname(), nil
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// mapFile maps a whole file read-only.
func mapFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return unix.Mmap(int(f.Fd()), 0, int(st.Size()), unix.PROT_READ, unix.MAP_SHARED)
}

// pushState sends the target what it needs to resume the VM, straight to its
// agent: the snapshot's small files and the disk's changes since ref,
// concurrently. (Stop-and-copy used tar and apply-delta through ssh.) It
// returns the delta's size.
func pushState(ctx context.Context, target *Client, id string, p vm.Paths, ref string) (int64, error) {
	files := map[string][]byte{}
	for name, path := range stateFiles(p) {
		b, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		files[name] = b
	}
	errc := make(chan error, 1)
	go func() { errc <- target.PutState(ctx, id, files) }()
	n, err := pushDelta(ctx, target, id, p, ref, p.Rootfs(), false)
	if e := <-errc; e != nil && err == nil {
		err = e
	}
	return n, err
}

// pushDelta makes the target's disk equal to file "to", given that it holds
// "from" now: only the ranges where the two differ travel. With snapshot set,
// "to" is first made as a reflink clone of the VM's live disk (instant), so
// the VM can keep running and writing while its changes so far are sent.
func pushDelta(ctx context.Context, target *Client, id string, p vm.Paths, from, to string, snapshot bool) (int64, error) {
	if snapshot {
		if err := vm.CloneFile(p.Rootfs(), to); err != nil {
			return 0, err
		}
	}
	a, err := blockdiff.Extents(to)
	if err != nil {
		return 0, err
	}
	b, err := blockdiff.Extents(from)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(to)
	if err != nil {
		return 0, err
	}
	ranges := blockdiff.Diff(a, b, uint64(st.Size()))
	f, err := os.Open(to)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(blockdiff.Send(pw, f, ranges)) }()
	return int64(blockdiff.Total(ranges)), target.PutDelta(ctx, id, pr)
}

// stateFiles are the only names putState accepts, and where each one goes.
func stateFiles(p vm.Paths) map[string]string {
	return map[string]string{"vmstate": p.Vmstate(), "net": p.SnapNet(), "image": p.SnapImage(), "base": p.SnapBase()}
}

// putState is the target's side of pushState's small files.
func (a *Agent) putState(w http.ResponseWriter, r *http.Request) {
	p := vm.PathsFor(r.PathValue("id"))
	var files map[string][]byte
	if err := json.NewDecoder(r.Body).Decode(&files); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dst := stateFiles(p)
	for name, b := range files {
		path, ok := dst[name]
		if !ok {
			http.Error(w, "unknown state file "+name, http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// putDelta applies the disk delta to the target's copy of the base disk.
func (a *Agent) putDelta(w http.ResponseWriter, r *http.Request) {
	p := vm.PathsFor(r.PathValue("id"))
	if _, err := blockdiff.Apply(r.Body, p.SnapRootfs()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
