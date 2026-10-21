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

// NetworkInterface attaches a host TAP device to the guest as eth0-style NIC.
type NetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMAC    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

// InstanceInfo is what GET / reports about the VM.
type InstanceInfo struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	VMMVersion string `json:"vmm_version"`
}

// SnapshotCreate says where to write a snapshot. The VM must be paused.
type SnapshotCreate struct {
	SnapshotPath string `json:"snapshot_path"` // device and vCPU state ("vmstate")
	MemFilePath  string `json:"mem_file_path"` // guest RAM
	SnapshotType string `json:"snapshot_type,omitempty"`
	// SyncSnapshotFiles asks Firecracker to fsync both files before replying.
	// Nil leaves Firecracker's default (true).
	SyncSnapshotFiles *bool `json:"sync_snapshot_files,omitempty"`
}

// MemBackend says where guest RAM is loaded from on restore. "File" maps a
// memory file; "Uffd" hands page faults to another process (M5).
type MemBackend struct {
	BackendType string `json:"backend_type"`
	BackendPath string `json:"backend_path"`
}

// SnapshotLoad restores a VM into a fresh, unconfigured Firecracker process.
type SnapshotLoad struct {
	SnapshotPath string     `json:"snapshot_path"`
	MemBackend   MemBackend `json:"mem_backend"`
	ResumeVM     bool       `json:"resume_vm"`
}

type vmState struct {
	State string `json:"state"`
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

func (c *Client) PutNetworkInterface(ctx context.Context, n NetworkInterface) error {
	return c.do(ctx, http.MethodPut, "/network-interfaces/"+n.IfaceID, n, nil)
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

// PauseVM stops the vCPUs. The guest sees no time pass and nothing is lost:
// registers and memory stay exactly as they were.
func (c *Client) PauseVM(ctx context.Context) error {
	return c.do(ctx, http.MethodPatch, "/vm", vmState{State: "Paused"}, nil)
}

func (c *Client) ResumeVM(ctx context.Context) error {
	return c.do(ctx, http.MethodPatch, "/vm", vmState{State: "Resumed"}, nil)
}

func (c *Client) CreateSnapshot(ctx context.Context, s SnapshotCreate) error {
	return c.do(ctx, http.MethodPut, "/snapshot/create", s, nil)
}

func (c *Client) LoadSnapshot(ctx context.Context, s SnapshotLoad) error {
	return c.do(ctx, http.MethodPut, "/snapshot/load", s, nil)
}
