package firecracker

import (
	"context"
	"net/http"
)

// BootSource is the guest kernel and its command line.
type BootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args,omitempty"`
}

// Drive is a host file exposed to the guest as a virtio block device.
type Drive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

// MachineConfig is the guest's CPU and memory size.
type MachineConfig struct {
	VCPUCount  int `json:"vcpu_count"`
	MemSizeMiB int `json:"mem_size_mib"`
}

// InstanceInfo is what GET / reports about the VM.
type InstanceInfo struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	VMMVersion string `json:"vmm_version"`
}

type action struct {
	ActionType string `json:"action_type"`
}

// The PUT calls below only record configuration. Nothing touches KVM until
// StartInstance.

func (c *Client) PutBootSource(ctx context.Context, b BootSource) error {
	return c.do(ctx, http.MethodPut, "/boot-source", b, nil)
}

func (c *Client) PutDrive(ctx context.Context, d Drive) error {
	return c.do(ctx, http.MethodPut, "/drives/"+d.DriveID, d, nil)
}

func (c *Client) PutMachineConfig(ctx context.Context, m MachineConfig) error {
	return c.do(ctx, http.MethodPut, "/machine-config", m, nil)
}

// StartInstance builds the VM from the recorded configuration and runs it.
func (c *Client) StartInstance(ctx context.Context) error {
	return c.do(ctx, http.MethodPut, "/actions", action{ActionType: "InstanceStart"}, nil)
}

func (c *Client) DescribeInstance(ctx context.Context) (InstanceInfo, error) {
	var info InstanceInfo
	err := c.do(ctx, http.MethodGet, "/", nil, &info)
	return info, err
}
