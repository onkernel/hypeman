package instances

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onkernel/hypeman/lib/vmm"
)

// StandbyInstance puts an instance in standby state
// Multi-hop orchestration: Running → Paused → Standby
func (m *manager) standbyInstance(
	ctx context.Context,
	
	id string,
) (*Instance, error) {
	// 1. Load instance
	meta, err := m.loadMetadata(id)
	if err != nil {
		return nil, err
	}

	inst := meta.ToInstance()

	// 2. Validate state transition (must be Running to start standby flow)
	if inst.State != StateRunning {
		return nil, fmt.Errorf("%w: cannot standby from state %s", ErrInvalidState, inst.State)
	}

	// 3. Create VMM client
	client, err := vmm.NewVMM(inst.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("create vmm client: %w", err)
	}

	// 4. Reduce memory to base size (virtio-mem balloon)
	if err := reduceMemory(ctx, client, inst.Size); err != nil {
		// Log warning but continue - snapshot will just be larger
	}

	// 5. Transition: Running → Paused
	pauseResp, err := client.PauseVMWithResponse(ctx)
	if err != nil || pauseResp.StatusCode() != 204 {
		return nil, fmt.Errorf("pause vm failed: %w", err)
	}

	inst.State = StatePaused
	meta = &metadata{Instance: *inst}
	if err := m.saveMetadata(meta); err != nil {
		// Metadata save failed but VM is paused - try to resume
		client.ResumeVMWithResponse(ctx)
		return nil, fmt.Errorf("save metadata after pause: %w", err)
	}

	// 6. Create snapshot
	snapshotDir := filepath.Join(inst.DataDir, "snapshots", "snapshot-latest")
	if err := createSnapshot(ctx, client, snapshotDir); err != nil {
		// Snapshot failed - try to resume VM
		client.ResumeVMWithResponse(ctx)
		inst.State = StateRunning
		meta := &metadata{Instance: *inst}
		m.saveMetadata(meta)
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	// 7. Compress snapshot (best effort)
	compressSnapshot(snapshotDir)

	// 8. Stop VMM
	if err := m.stopVMM(ctx, inst); err != nil {
		// Log but continue - snapshot was created successfully
	}

	// 9. Transition: Paused → Standby
	inst.State = StateStandby
	inst.HasSnapshot = true
	now := time.Now()
	inst.StoppedAt = &now

	meta = &metadata{Instance: *inst}
	if err := m.saveMetadata(meta); err != nil {
		return nil, fmt.Errorf("save metadata: %w", err)
	}

	return inst, nil
}

// reduceMemory attempts to reduce VM memory to minimize snapshot size
func reduceMemory(ctx context.Context, client *vmm.VMM, targetBytes int64) error {
	resizeConfig := vmm.VmResize{DesiredRam: &targetBytes}
	if resp, err := client.PutVmResizeWithResponse(ctx, resizeConfig); err != nil || resp.StatusCode() != 204 {
		return fmt.Errorf("memory resize failed")
	}

	// Wait for balloon to deflate
	// TODO: Poll actual memory usage instead of fixed sleep
	time.Sleep(2 * time.Second)

	return nil
}

// createSnapshot creates a Cloud Hypervisor snapshot
func createSnapshot(ctx context.Context, client *vmm.VMM, snapshotDir string) error {
	// Remove old snapshot
	os.RemoveAll(snapshotDir)

	// Create snapshot directory
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	// Create snapshot via VMM API
	snapshotURL := "file://" + snapshotDir
	snapshotConfig := vmm.VmSnapshotConfig{DestinationUrl: &snapshotURL}

	resp, err := client.PutVmSnapshotWithResponse(ctx, snapshotConfig)
	if err != nil {
		return fmt.Errorf("snapshot api call: %w", err)
	}
	if resp.StatusCode() != 204 {
		return fmt.Errorf("snapshot failed with status %d", resp.StatusCode())
	}

	return nil
}

// compressSnapshot compresses the memory-ranges file with LZ4 (best effort)
func compressSnapshot(snapshotDir string) error {
	memoryFile := filepath.Join(snapshotDir, "memory-ranges")

	// Check if memory-ranges exists
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		return nil // No memory file, nothing to compress
	}

	// Compress with lz4 fast mode (-1), remove original
	cmd := exec.Command("lz4", "-1", "--rm", memoryFile, memoryFile+".lz4")
	if err := cmd.Run(); err != nil {
		// Best effort - don't fail standby if compression fails
		return err
	}

	return nil
}

