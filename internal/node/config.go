// Package node reads the small per-node configuration file.
package node

import (
	"encoding/json"
	"errors"
	"os"
)

// Path is where scripts/host/overlay.sh writes the config on every node.
const Path = "/var/lib/chromebox/node.json"

// Config says who this node is and where its network edge lives.
type Config struct {
	Name    string `json:"name"`    // e.g. "node-a"; also names this node's tunnel on the gateway ("gre-node-a")
	Gateway string `json:"gateway"` // ssh target of the gateway, e.g. "ec2-user@172.31.5.9"
}

// Load returns the node's config, or nil if there is none (a node used on its
// own, without a gateway).
func Load() (*Config, error) {
	b, err := os.ReadFile(Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Tunnel is the name of this node's tunnel interface on the gateway.
func (c Config) Tunnel() string { return "gre-" + c.Name }
