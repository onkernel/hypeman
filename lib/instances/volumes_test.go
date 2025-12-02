package instances

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onkernel/hypeman/lib/images"
	"github.com/onkernel/hypeman/lib/paths"
	"github.com/onkernel/hypeman/lib/system"
	"github.com/onkernel/hypeman/lib/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForExecAgent polls until exec-agent is ready
func waitForExecAgent(ctx context.Context, mgr *manager, instanceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		logs, err := collectLogs(ctx, mgr, instanceID, 100)
		if err == nil && strings.Contains(logs, "[exec-agent] listening on vsock port 2222") {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

// execWithRetry runs a command with retries until exec-agent is ready
func execWithRetry(ctx context.Context, vsockSocket string, command []string) (string, int, error) {
	var output string
	var code int
	var err error

	for i := 0; i < 10; i++ {
		output, code, err = execCommand(ctx, vsockSocket, command...)
		if err == nil {
			return output, code, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return output, code, err
}

// TestVolumeMultiAttachReadOnly tests that a volume can be:
// 1. Attached read-write to one instance, written to
// 2. Detached (by deleting the instance)
// 3. Attached read-only to multiple instances simultaneously
// 4. Data persists and is readable from all instances
func TestVolumeMultiAttachReadOnly(t *testing.T) {
	// Require KVM
	if _, err := os.Stat("/dev/kvm"); os.IsNotExist(err) {
		t.Fatal("/dev/kvm not available - ensure KVM is enabled and user is in 'kvm' group")
	}

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	manager, tmpDir := setupTestManager(t)
	ctx := context.Background()
	p := paths.New(tmpDir)

	// Setup: prepare image and system files
	imageManager, err := images.NewManager(p, 1)
	require.NoError(t, err)

	t.Log("Pulling alpine image...")
	_, err = imageManager.CreateImage(ctx, images.CreateImageRequest{
		Name: "docker.io/library/alpine:latest",
	})
	require.NoError(t, err)

	for i := 0; i < 60; i++ {
		img, err := imageManager.GetImage(ctx, "docker.io/library/alpine:latest")
		if err == nil && img.Status == images.StatusReady {
			break
		}
		time.Sleep(1 * time.Second)
	}
	t.Log("Image ready")

	systemManager := system.NewManager(p)
	t.Log("Ensuring system files...")
	err = systemManager.EnsureSystemFiles(ctx)
	require.NoError(t, err)
	t.Log("System files ready")

	// Create volume
	volumeManager := volumes.NewManager(p, 0)
	t.Log("Creating volume...")
	vol, err := volumeManager.CreateVolume(ctx, volumes.CreateVolumeRequest{
		Name:   "shared-data",
		SizeGb: 1,
	})
	require.NoError(t, err)
	t.Logf("Volume created: %s", vol.Id)

	// Phase 1: Create instance with volume attached read-write
	t.Log("Phase 1: Creating writer instance with read-write volume...")
	writerInst, err := manager.CreateInstance(ctx, CreateInstanceRequest{
		Name:           "writer",
		Image:          "docker.io/library/alpine:latest",
		Size:           512 * 1024 * 1024,
		HotplugSize:    512 * 1024 * 1024,
		OverlaySize:    1024 * 1024 * 1024,
		Vcpus:          1,
		NetworkEnabled: false,
		Volumes: []VolumeAttachment{
			{VolumeID: vol.Id, MountPath: "/data", Readonly: false},
		},
	})
	require.NoError(t, err)
	t.Logf("Writer instance created: %s", writerInst.Id)

	// Wait for exec-agent
	err = waitForExecAgent(ctx, manager, writerInst.Id, 30*time.Second)
	require.NoError(t, err, "exec-agent should be ready")

	// Write test file
	t.Log("Writing test file to volume...")
	_, code, err := execWithRetry(ctx, writerInst.VsockSocket, []string{
		"/bin/sh", "-c", "echo 'Hello from writer' > /data/test.txt",
	})
	require.NoError(t, err)
	require.Equal(t, 0, code, "Write command should succeed")

	// Verify file was written
	output, code, err := execWithRetry(ctx, writerInst.VsockSocket, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	assert.Contains(t, output, "Hello from writer")
	t.Log("Test file written successfully")

	// Delete writer instance (detaches volume)
	t.Log("Deleting writer instance...")
	err = manager.DeleteInstance(ctx, writerInst.Id)
	require.NoError(t, err)

	// Verify volume is detached
	vol, err = volumeManager.GetVolume(ctx, vol.Id)
	require.NoError(t, err)
	assert.Empty(t, vol.Attachments, "Volume should be detached")

	// Phase 2: Create two instances with the volume attached read-only
	t.Log("Phase 2: Creating two reader instances with read-only volume...")

	reader1, err := manager.CreateInstance(ctx, CreateInstanceRequest{
		Name:           "reader-1",
		Image:          "docker.io/library/alpine:latest",
		Size:           512 * 1024 * 1024,
		HotplugSize:    512 * 1024 * 1024,
		OverlaySize:    1024 * 1024 * 1024,
		Vcpus:          1,
		NetworkEnabled: false,
		Volumes: []VolumeAttachment{
			{VolumeID: vol.Id, MountPath: "/data", Readonly: true},
		},
	})
	require.NoError(t, err)
	t.Logf("Reader 1 created: %s", reader1.Id)

	reader2, err := manager.CreateInstance(ctx, CreateInstanceRequest{
		Name:           "reader-2",
		Image:          "docker.io/library/alpine:latest",
		Size:           512 * 1024 * 1024,
		HotplugSize:    512 * 1024 * 1024,
		OverlaySize:    1024 * 1024 * 1024,
		Vcpus:          1,
		NetworkEnabled: false,
		Volumes: []VolumeAttachment{
			{VolumeID: vol.Id, MountPath: "/data", Readonly: true},
		},
	})
	require.NoError(t, err)
	t.Logf("Reader 2 created: %s", reader2.Id)

	// Verify volume has two attachments
	vol, err = volumeManager.GetVolume(ctx, vol.Id)
	require.NoError(t, err)
	assert.Len(t, vol.Attachments, 2, "Volume should have 2 attachments")

	// Wait for exec-agent on both readers
	err = waitForExecAgent(ctx, manager, reader1.Id, 30*time.Second)
	require.NoError(t, err, "reader-1 exec-agent should be ready")

	err = waitForExecAgent(ctx, manager, reader2.Id, 30*time.Second)
	require.NoError(t, err, "reader-2 exec-agent should be ready")

	// Verify data is readable from reader-1
	t.Log("Verifying data from reader-1...")
	output, code, err = execWithRetry(ctx, reader1.VsockSocket, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	assert.Contains(t, output, "Hello from writer", "Reader 1 should see the file")

	// Verify data is readable from reader-2
	t.Log("Verifying data from reader-2...")
	output, code, err = execWithRetry(ctx, reader2.VsockSocket, []string{"cat", "/data/test.txt"})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	assert.Contains(t, output, "Hello from writer", "Reader 2 should see the file")

	// Verify write fails on read-only mount (reader-1)
	t.Log("Verifying read-only enforcement...")
	_, code, err = execWithRetry(ctx, reader1.VsockSocket, []string{
		"/bin/sh", "-c", "echo 'illegal write' > /data/illegal.txt",
	})
	require.NoError(t, err, "Exec should succeed even if command fails")
	assert.NotEqual(t, 0, code, "Write to read-only volume should fail with non-zero exit")

	t.Log("Multi-attach read-only test passed!")

	// Cleanup
	t.Log("Cleaning up...")
	manager.DeleteInstance(ctx, reader1.Id)
	manager.DeleteInstance(ctx, reader2.Id)
	volumeManager.DeleteVolume(ctx, vol.Id)
}
