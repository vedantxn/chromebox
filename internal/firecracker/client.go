// Package firecracker is a minimal client for Firecracker's REST API.
// Firecracker serves plain HTTP, but over a Unix socket instead of TCP.
package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Client talks to one Firecracker process, i.e. one VM.
type Client struct {
	http *http.Client
}

// New returns a client for the Firecracker API socket at socketPath.
func New(socketPath string) *Client {
	transport := &http.Transport{
		// Replacing the dialer swaps TCP for the Unix socket and leaves HTTP
		// itself untouched. This is what `curl --unix-socket` does.
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}}
}

// apiError is the body Firecracker sends with any non-2xx response.
type apiError struct {
	FaultMessage string `json:"fault_message"`
}

// do sends one request. in is encoded as the JSON body; out, if non-nil,
// receives the decoded JSON response.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	// The dialer ignores the host, so "localhost" only fills the Host header.
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var e apiError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, e.FaultMessage)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
