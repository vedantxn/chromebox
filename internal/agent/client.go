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
