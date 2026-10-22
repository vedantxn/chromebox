package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vedantxn/chromebox/internal/api"
	"github.com/vedantxn/chromebox/internal/base"
	"github.com/vedantxn/chromebox/internal/blockdiff"
	"github.com/vedantxn/chromebox/internal/pageserver"
	"github.com/vedantxn/chromebox/internal/timing"
)

// Fork images travel between nodes (M6, C3) when a fork's copies don't all
// fit on the parent's node. The node that needs the image pulls it from the
// node that has it, agent to agent:
//
//	manifest and vmstate   small files
//	disk                   only the blocks that differ from the regular base
//	                       both nodes hold (the extent diff of M2)
//	memory                 only the non-zero runs (the M5 stream format);
//	                       zero pages stay holes in the receiver's RAM file

func (a *Agent) imageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/bases/{name}/manifest", a.serveManifest)
	mux.HandleFunc("GET /v1/bases/{name}/vmstate", a.serveVmstate)
	mux.HandleFunc("GET /v1/bases/{name}/disk", a.serveDisk)
	mux.HandleFunc("GET /v1/bases/{name}/memory", a.serveMemory)
	mux.HandleFunc("POST /v1/bases/{name}/pull", a.pullImage)
}

func (a *Agent) serveManifest(w http.ResponseWriter, r *http.Request) {
	m, err := base.Read(r.PathValue("name"))
	if err != nil || !base.IsFork(m.Name) {
		http.Error(w, "no fork image "+r.PathValue("name"), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *Agent) serveVmstate(w http.ResponseWriter, r *http.Request) {
	vmstate, _, _ := base.ImageFiles(r.PathValue("name"))
	http.ServeFile(w, r, vmstate)
}

// serveDisk streams the image's disk as the ranges that differ from base
// ?against=, in blockdiff format.
func (a *Agent) serveDisk(w http.ResponseWriter, r *http.Request) {
	_, _, disk := base.ImageFiles(r.PathValue("name"))
	ranges, err := diskDelta(disk, base.Disk(r.URL.Query().Get("against")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := os.Open(disk)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	bw := bufio.NewWriterSize(w, 1<<20)
	if err := blockdiff.Send(bw, f, ranges); err == nil {
		_ = bw.Flush()
	}
}

func diskDelta(to, from string) ([]blockdiff.Range, error) {
	a, err := blockdiff.Extents(to)
	if err != nil {
		return nil, err
	}
	b, err := blockdiff.Extents(from)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(to)
	if err != nil {
		return nil, err
	}
	return blockdiff.Diff(a, b, uint64(st.Size())), nil
}

func (a *Agent) serveMemory(w http.ResponseWriter, r *http.Request) {
	_, mem, _ := base.ImageFiles(r.PathValue("name"))
	m, err := mapFile(mem)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer unix.Munmap(m)
	bw := bufio.NewWriterSize(w, 1<<20)
	if err := pageserver.WriteRuns(bw, m, nil); err == nil {
		_ = bw.Flush()
	}
}

// pullImage fetches fork image name from the agent at {"from": url} and
// installs it here, then reports it in a heartbeat at once.
func (a *Agent) pullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.From == "" {
		http.Error(w, "need from", http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	src := NewClient(req.From, a.cfg.Token)
	ctx := r.Context()
	sw := timing.Start()
	var m base.Manifest
	if err := src.do(ctx, http.MethodGet, "/v1/bases/"+name+"/manifest", nil, &m); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	vmstate, err := src.fetch(ctx, "/v1/bases/"+name+"/vmstate", io.ReadAll)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	var diskBytes, memBytes int64
	err = base.Install(m, vmstate,
		func(path string) error {
			_, err := src.fetch(ctx, "/v1/bases/"+name+"/disk?against="+m.DiskBase, func(body io.Reader) ([]byte, error) {
				n, err := blockdiff.Apply(body, path)
				diskBytes = n
				return nil, err
			})
			return err
		},
		func(path string) error {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := f.Truncate(int64(m.MemMiB) << 20); err != nil {
				return err
			}
			_, err = src.fetch(ctx, "/v1/bases/"+name+"/memory", func(body io.Reader) ([]byte, error) {
				return nil, pageserver.ReadRuns(body, func(off uint64, data []byte) error {
					memBytes += int64(len(data))
					_, err := f.WriteAt(data, int64(off))
					return err
				}, nil)
			})
			return err
		})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sw.Lap("image received")
	if err := a.heartbeat(ctx); err != nil {
		log.Printf("pull %s: heartbeat: %v", name, err)
	}
	writeJSON(w, http.StatusOK, api.PullImageResponse{Laps: sw.Record(), DiskMiB: float64(diskBytes) / (1 << 20), MemMiB: float64(memBytes) / (1 << 20)})
}

// fetch GETs path and hands the body to read.
func (c *Client) fetch(ctx context.Context, path string, read func(io.Reader) ([]byte, error)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %s: %s", path, resp.Status, msg)
	}
	return read(resp.Body)
}

// PullImage has this agent fetch fork image name from the agent at from.
func (c *Client) PullImage(ctx context.Context, name, from string) (api.PullImageResponse, error) {
	var out api.PullImageResponse
	err := c.do(ctx, http.MethodPost, "/v1/bases/"+name+"/pull", map[string]string{"from": from}, &out)
	return out, err
}
