package qemu

import (
	"context"
	"fmt"
	"time"

	"github.com/onkernel/hypeman/lib/hypervisor"
)

// QEMU implements hypervisor.Hypervisor for QEMU VMM.
type QEMU struct {
	client *QMPClient
}

// New creates a new QEMU client for an existing QMP socket.
func New(socketPath string) (*QEMU, error) {
	client, err := NewQMPClient(socketPath)
	if err != nil {
		return nil, fmt.Errorf("create qmp client: %w", err)
	}
	return &QEMU{client: client}, nil
}

// Verify QEMU implements the interface
var _ hypervisor.Hypervisor = (*QEMU)(nil)

// Capabilities returns the features supported by QEMU.
func (q *QEMU) Capabilities() hypervisor.Capabilities {
	return hypervisor.Capabilities{
		SupportsSnapshot:       false, // Not implemented in first pass
		SupportsHotplugMemory:  false, // Not implemented in first pass
		SupportsPause:          true,
		SupportsVsock:          true,
		SupportsGPUPassthrough: true,
	}
}

// CreateVM configures the VM in QEMU.
// For QEMU, the VM is configured via command-line args when the process starts,
// so this is a no-op. The configuration is applied in StartProcess.
func (q *QEMU) CreateVM(ctx context.Context, config hypervisor.VMConfig) error {
	// QEMU doesn't have a separate create step - configuration is done at process start
	// This is a no-op for QEMU
	return nil
}

// BootVM starts the configured VM.
// For QEMU, the VM starts automatically when the process starts,
// so this is a no-op.
func (q *QEMU) BootVM(ctx context.Context) error {
	// QEMU starts running immediately when the process starts
	// This is a no-op for QEMU
	return nil
}

// DeleteVM removes the VM configuration from QEMU.
// This sends a graceful shutdown signal to the guest.
func (q *QEMU) DeleteVM(ctx context.Context) error {
	return q.client.SystemPowerdown()
}

// Shutdown stops the QEMU process.
func (q *QEMU) Shutdown(ctx context.Context) error {
	return q.client.Quit()
}

// GetVMInfo returns current VM state.
func (q *QEMU) GetVMInfo(ctx context.Context) (*hypervisor.VMInfo, error) {
	status, running, err := q.client.QueryStatus()
	if err != nil {
		return nil, fmt.Errorf("query status: %w", err)
	}

	var state hypervisor.VMState
	switch {
	case running:
		state = hypervisor.StateRunning
	case status == "paused":
		state = hypervisor.StatePaused
	case status == "shutdown":
		state = hypervisor.StateShutdown
	case status == "prelaunch":
		state = hypervisor.StateCreated
	default:
		// Map other QEMU states to appropriate hypervisor states
		if status == "inmigrate" || status == "postmigrate" {
			state = hypervisor.StatePaused
		} else {
			state = hypervisor.StateRunning
		}
	}

	return &hypervisor.VMInfo{
		State:            state,
		MemoryActualSize: nil, // Not implemented in first pass
	}, nil
}

// Pause suspends VM execution.
func (q *QEMU) Pause(ctx context.Context) error {
	return q.client.Stop()
}

// Resume continues VM execution.
func (q *QEMU) Resume(ctx context.Context) error {
	return q.client.Continue()
}

// Snapshot creates a VM snapshot.
// Not implemented in first pass.
func (q *QEMU) Snapshot(ctx context.Context, destPath string) error {
	return fmt.Errorf("snapshot not supported by QEMU implementation")
}

// Restore loads a VM from snapshot.
// Not implemented in first pass.
func (q *QEMU) Restore(ctx context.Context, sourcePath string) error {
	return fmt.Errorf("restore not supported by QEMU implementation")
}

// ResizeMemory changes the VM's memory allocation.
// Not implemented in first pass.
func (q *QEMU) ResizeMemory(ctx context.Context, bytes int64) error {
	return fmt.Errorf("memory resize not supported by QEMU implementation")
}

// ResizeMemoryAndWait changes the VM's memory allocation and waits for it to stabilize.
// Not implemented in first pass.
func (q *QEMU) ResizeMemoryAndWait(ctx context.Context, bytes int64, timeout time.Duration) error {
	return fmt.Errorf("memory resize not supported by QEMU implementation")
}
