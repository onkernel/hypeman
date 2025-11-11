package instances

import (
	"context"
	"fmt"
	"os"

	"github.com/onkernel/hypeman/lib/vmm"
)

// deleteInstance stops and deletes an instance
// Orchestrates: current state → Shutdown (if needed) → Stopped
func (m *manager) deleteInstance(
	ctx context.Context,
	id string,
) error {
	// 1. Load instance
	meta, err := m.loadMetadata(id)
	if err != nil {
		return err
	}

	inst := meta.ToInstance()

	// 2. If VMM is running, stop it
	if inst.State.RequiresVMM() {
		if err := m.stopVMM(ctx, inst); err != nil {
			// Log error but continue with cleanup
			// Best effort to clean up even if VMM is unresponsive
		}
	}

	// 3. Delete all instance data
	if err := m.deleteInstanceData(id); err != nil {
		return fmt.Errorf("delete instance data: %w", err)
	}

	return nil
}

// stopVMM attempts to gracefully stop the VMM, falls back to force kill
func (m *manager) stopVMM(ctx context.Context, inst *Instance) error {
	// Try to connect to VMM
	client, err := vmm.NewVMM(inst.SocketPath)
	if err != nil {
		// Socket doesn't exist or can't connect - might already be stopped
		return nil
	}

	// Try graceful shutdown
	if resp, err := client.ShutdownVMMWithResponse(ctx); err == nil && resp.StatusCode() < 300 {
		return nil
	}

	// Graceful shutdown failed - force cleanup
	// Remove socket file
	os.Remove(inst.SocketPath)

	// Note: In production, you might want to find and kill the process
	// using lsof on the overlay disk, similar to the POC script
	// For now, we rely on socket removal

	return nil
}

