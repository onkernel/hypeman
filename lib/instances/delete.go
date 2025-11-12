package instances

import (
	"context"
	"fmt"
	"os"

	"github.com/onkernel/hypeman/lib/vmm"
)

// deleteInstance stops and deletes an instance
func (m *manager) deleteInstance(
	ctx context.Context,
	id string,
) error {
	// 1. Load instance
	meta, err := m.loadMetadata(id)
	if err != nil {
		return err
	}

	inst := m.toInstance(ctx, meta)

	// 2. If VMM might be running, try to stop it
	if inst.State.RequiresVMM() {
		if err := m.stopVMM(ctx, &inst); err != nil {
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

// stopVMM attempts to gracefully stop the VMM
func (m *manager) stopVMM(ctx context.Context, inst *Instance) error {
	// Check if socket exists
	if _, err := os.Stat(inst.SocketPath); os.IsNotExist(err) {
		// No socket - VMM not running
		return nil
	}

	// Try to connect to VMM
	client, err := vmm.NewVMM(inst.SocketPath)
	if err != nil {
		// Socket exists but can't connect - stale socket
		// Don't delete it here - it will be cleaned up by vmm.StartProcess if needed
		return nil
	}

	// Verify VMM responds
	if _, err := client.GetVmmPingWithResponse(ctx); err != nil {
		// VMM unresponsive - stale socket
		return nil
	}

	// VMM is alive - try graceful shutdown
	client.ShutdownVMMWithResponse(ctx)
	// Cloud Hypervisor will remove the socket on successful shutdown
	
	return nil
}

