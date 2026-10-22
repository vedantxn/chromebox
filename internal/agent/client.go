package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// Client is the orchestrator's handle on one node's agent.
type Client struct {
	URL   string
	Token string
	http  *http.Client
}

func NewClient(url, token string) *Client {
	return &Client{URL: url, Token: token, http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	return c.doBody(ctx, method, path, body, out)
}

// PutState hands the target the snapshot's small files (device state, network
// index, image and base names), by name.
func (c *Client) PutState(ctx context.Context, id string, files map[string][]byte) error {
	return c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/state", files, nil)
}

// PutDelta streams the disk delta (blockdiff format) into the target's copy.
func (c *Client) PutDelta(ctx context.Context, id string, delta io.Reader) error {
	return c.doBody(ctx, http.MethodPost, "/v1/vms/"+id+"/delta", delta, nil)
}

func (c *Client) doBody(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent %s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(msg))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) CreateVM(ctx context.Context, req api.CreateVMRequest) (api.CreateVMResponse, error) {
	var out api.CreateVMResponse
	err := c.do(ctx, http.MethodPost, "/v1/vms", req, &out)
	return out, err
}

func (c *Client) DeleteVM(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vms/"+id, nil, nil)
}

func (c *Client) Status(ctx context.Context) (api.Status, error) {
	var out api.Status
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &out)
	return out, err
}

func (c *Client) Migrate(ctx context.Context, id string, req api.MigrateRequest) (api.MigrateResponse, error) {
	var out api.MigrateResponse
	err := c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/migrate", req, &out)
	return out, err
}

func (c *Client) Incoming(ctx context.Context, req api.IncomingRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/incoming", req, nil)
}

func (c *Client) Accept(ctx context.Context, id string, req api.AcceptRequest) (api.CreateVMResponse, error) {
	var out api.CreateVMResponse
	err := c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/accept", req, &out)
	return out, err
}

// Commit retires the source copy. After a post-copy migration it returns only
// once the target holds every page.
// ForkImage makes a fork image from running VM id; the agent has already
// reported it in a heartbeat when this returns.
func (c *Client) ForkImage(ctx context.Context, id, image string) (api.ForkImageResponse, error) {
	var out api.ForkImageResponse
	err := c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/fork-image", api.ForkImageRequest{Image: image}, &out)
	return out, err
}

// DeleteBase removes a fork image from the node.
func (c *Client) DeleteBase(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/bases/"+name, nil, nil)
}

func (c *Client) Commit(ctx context.Context, id string) (api.CommitResponse, error) {
	var out api.CommitResponse
	err := c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/commit", nil, &out)
	return out, err
}

func (c *Client) Abort(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/vms/"+id+"/abort", nil, nil)
}
