package devices_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onkernel/hypeman/cmd/api/config"
	"github.com/onkernel/hypeman/lib/devices"
	"github.com/onkernel/hypeman/lib/exec"
	"github.com/onkernel/hypeman/lib/images"
	"github.com/onkernel/hypeman/lib/instances"
	"github.com/onkernel/hypeman/lib/network"
	"github.com/onkernel/hypeman/lib/paths"
	"github.com/onkernel/hypeman/lib/system"
	"github.com/onkernel/hypeman/lib/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// persistentTestDataDir is used to persist volumes between test runs.
// This allows the ollama model cache to survive across test executions.
// Note: Uses /var/lib instead of /tmp because /tmp often has limited space
// and the ollama image is ~3GB.
const persistentTestDataDir = "/var/lib/hypeman-gpu-inference-test"

// TestGPUInference is an E2E test that verifies Ollama inference works with GPU passthrough.
//
// This test:
//  1. Creates a persistent volume for Ollama's model cache
//  2. Launches a VM with GPU passthrough + the volume attached + networking
//  3. Runs `ollama run tinyllama` to perform inference
//  4. Verifies the model generates output
//
// NOTE: Currently runs CPU inference because the ollama/ollama image doesn't include
// NVIDIA drivers. Full GPU inference would require a custom image with:
//   - NVIDIA drivers installed in the guest OS
//   - CUDA libraries
//   - nvidia-container-toolkit configured
//
// The volume persists between test runs, so:
//   - First run: Downloads TinyLlama model (~637MB) - takes 3-5 minutes
//   - Subsequent runs: Model already cached - takes ~30 seconds
//
// Prerequisites (same as TestGPUPassthrough):
//   - NVIDIA GPU on host
//   - IOMMU enabled
//   - VFIO modules loaded (modprobe vfio_pci)
//   - Running as root
//
// To run manually:
//
//	sudo env PATH=$PATH:/sbin:/usr/sbin go test -v -run TestGPUInference -timeout 15m ./lib/devices/...
//
// To clean up the persistent volume:
//
//	sudo rm -rf /var/lib/hypeman-gpu-inference-test
func TestGPUInference(t *testing.T) {
	ctx := context.Background()

	// Auto-detect GPU availability - skip if prerequisites not met
	skipReason := checkGPUTestPrerequisites()
	if skipReason != "" {
		t.Skip(skipReason)
	}

	// Log that prerequisites passed
	groups, _ := os.ReadDir("/sys/kernel/iommu_groups")
	t.Logf("GPU inference test prerequisites met: %d IOMMU groups found", len(groups))

	// Use persistent directory for volume storage (survives between test runs)
	// This allows the ollama model cache to persist
	if err := os.MkdirAll(persistentTestDataDir, 0755); err != nil {
		t.Fatalf("Failed to create persistent test directory: %v", err)
	}
	p := paths.New(persistentTestDataDir)

	cfg := &config.Config{
		DataDir:    persistentTestDataDir,
		BridgeName: "vmbr0",
		SubnetCIDR: "10.100.0.0/16",
		DNSServer:  "1.1.1.1",
	}

	// Initialize managers (nil meter/tracer disables metrics/tracing)
	imageMgr, err := images.NewManager(p, 1, nil)
	require.NoError(t, err)

	systemMgr := system.NewManager(p)
	networkMgr := network.NewManager(p, cfg, nil)
	deviceMgr := devices.NewManager(p)
	volumeMgr := volumes.NewManager(p, 100*1024*1024*1024, nil) // 100GB max volume storage
	limits := instances.ResourceLimits{
		MaxOverlaySize: 100 * 1024 * 1024 * 1024, // 100GB
	}
	instanceMgr := instances.NewManager(p, imageMgr, systemMgr, networkMgr, deviceMgr, volumeMgr, limits, nil, nil)

	// Step 1: Discover available GPUs
	t.Log("Step 1: Discovering available GPUs...")
	availableDevices, err := deviceMgr.ListAvailableDevices(ctx)
	require.NoError(t, err)

	// Find an NVIDIA GPU
	var targetGPU *devices.AvailableDevice
	for _, d := range availableDevices {
		if strings.Contains(strings.ToLower(d.VendorName), "nvidia") {
			targetGPU = &d
			break
		}
	}
	require.NotNil(t, targetGPU, "No NVIDIA GPU found on this system")
	driverStr := "none"
	if targetGPU.CurrentDriver != nil {
		driverStr = *targetGPU.CurrentDriver
	}
	t.Logf("Found NVIDIA GPU: %s at %s (driver: %s)", targetGPU.DeviceName, targetGPU.PCIAddress, driverStr)

	// Check GPU is in a usable state (has a driver bound)
	if targetGPU.CurrentDriver == nil || *targetGPU.CurrentDriver == "" {
		t.Skip("GPU has no driver bound - may need reboot to recover. Run: sudo reboot")
	}

	// Verify the driver path exists (GPU not in broken state)
	driverPath := filepath.Join("/sys/bus/pci/devices", targetGPU.PCIAddress, "driver")
	if _, err := os.Stat(driverPath); os.IsNotExist(err) {
		t.Skipf("GPU driver symlink missing at %s - GPU in broken state, reboot required", driverPath)
	}

	// Step 2: Register the GPU (check if already registered from previous run)
	t.Log("Step 2: Registering GPU...")
	device, err := deviceMgr.GetDevice(ctx, "inference-gpu")
	if err != nil {
		// Device not registered yet, create it
		device, err = deviceMgr.CreateDevice(ctx, devices.CreateDeviceRequest{
			Name:       "inference-gpu",
			PCIAddress: targetGPU.PCIAddress,
		})
		require.NoError(t, err)
		t.Logf("Registered new device: %s (ID: %s)", device.Name, device.Id)
	} else {
		t.Logf("Using existing device: %s (ID: %s)", device.Name, device.Id)
	}

	// Store original driver for cleanup
	originalDriver := driverStr

	// Cleanup: always unregister device and try to restore original driver
	t.Cleanup(func() {
		t.Log("Cleanup: Deleting registered device...")
		deviceMgr.DeleteDevice(ctx, device.Id)

		// Try to restore original driver binding via driver_probe
		if originalDriver != "" && originalDriver != "none" && originalDriver != "vfio-pci" {
			t.Logf("Cleanup: Triggering driver probe to restore %s driver...", originalDriver)
			probePath := "/sys/bus/pci/drivers_probe"
			if err := os.WriteFile(probePath, []byte(targetGPU.PCIAddress), 0200); err != nil {
				t.Logf("Warning: Could not trigger driver probe: %v (may need reboot)", err)
			} else {
				t.Logf("Cleanup: Driver probe triggered for %s", targetGPU.PCIAddress)
			}
		}
	})

	// Step 3: Initialize network (needed for model download)
	t.Log("Step 3: Initializing network...")
	err = networkMgr.Initialize(ctx, []string{}) // No running instances yet
	require.NoError(t, err, "Network initialization should succeed")
	t.Log("Network initialized")

	// Step 4: Create or get persistent volume for ollama models
	t.Log("Step 4: Setting up persistent volume for Ollama models...")
	vol, err := volumeMgr.GetVolumeByName(ctx, "ollama-models")
	if err != nil {
		// Volume doesn't exist, create it (5GB should fit TinyLlama with room to spare)
		vol, err = volumeMgr.CreateVolume(ctx, volumes.CreateVolumeRequest{
			Name:   "ollama-models",
			SizeGb: 5,
		})
		require.NoError(t, err)
		t.Logf("Created new volume: %s (ID: %s, Size: %dGB)", vol.Name, vol.Id, vol.SizeGb)
		t.Log("⚠️  First run - model will be downloaded (~637MB). This may take 1-2 minutes.")
	} else {
		t.Logf("Using existing volume: %s (ID: %s) - model may already be cached", vol.Name, vol.Id)
	}
	// Note: We intentionally don't delete the volume so it persists between test runs

	// Step 5: Ensure system files (kernel, initrd)
	t.Log("Step 5: Ensuring system files...")
	err = systemMgr.EnsureSystemFiles(ctx)
	require.NoError(t, err)
	t.Log("System files ready")

	// Step 6: Pull ollama image
	t.Log("Step 6: Pulling ollama:latest image...")
	createdImg, createErr := imageMgr.CreateImage(ctx, images.CreateImageRequest{
		Name: "docker.io/ollama/ollama:latest",
	})
	require.NoError(t, createErr, "CreateImage should succeed")
	t.Logf("CreateImage returned: name=%s, status=%s", createdImg.Name, createdImg.Status)

	imageName := createdImg.Name

	// Wait for image to be ready (ollama image is ~1.5GB, may take a while)
	var img *images.Image
	for i := 0; i < 180; i++ { // 3 minutes timeout for image pull
		img, err = imageMgr.GetImage(ctx, imageName)
		if err != nil {
			if i < 5 || i%30 == 0 {
				t.Logf("GetImage attempt %d: error=%v", i+1, err)
			}
		} else {
			if i < 5 || i%30 == 0 {
				t.Logf("GetImage attempt %d: status=%s", i+1, img.Status)
			}
			if img.Status == images.StatusReady {
				break
			}
			if img.Status == images.StatusFailed {
				errMsg := "unknown"
				if img.Error != nil {
					errMsg = *img.Error
				}
				t.Fatalf("Image build failed: %s", errMsg)
			}
		}
		time.Sleep(1 * time.Second)
	}
	require.NotNil(t, img, "Image should exist after 180 seconds")
	require.Equal(t, images.StatusReady, img.Status, "Image should be ready")
	t.Log("Image ready")

	// Step 7: Create instance with GPU and volume
	t.Log("Step 7: Creating instance with GPU and Ollama model volume...")
	createCtx, createCancel := context.WithTimeout(ctx, 120*time.Second)
	defer createCancel()

	inst, err := instanceMgr.CreateInstance(createCtx, instances.CreateInstanceRequest{
		Name:        "gpu-inference-test",
		Image:       imageName,
		Size:        4 * 1024 * 1024 * 1024,  // 4GB RAM (ollama needs more memory)
		HotplugSize: 4 * 1024 * 1024 * 1024,  // 4GB hotplug
		OverlaySize: 10 * 1024 * 1024 * 1024, // 10GB overlay
		Vcpus:       4,                       // More CPUs for faster inference
		Env: map[string]string{
			"OLLAMA_HOST":   "0.0.0.0",      // Allow external connections (not strictly needed for test)
			"OLLAMA_MODELS": "/data/models", // Use mounted volume for model storage
		},
		NetworkEnabled: true, // Needed for model download
		Devices:        []string{"inference-gpu"},
		Volumes: []instances.VolumeAttachment{
			{
				VolumeID:  vol.Id,
				MountPath: "/data/models", // Ollama will use this via OLLAMA_MODELS env var
				Readonly:  false,          // Need write access to download models
			},
		},
	})
	require.NoError(t, err)
	t.Logf("Instance created: %s", inst.Id)

	// Cleanup: always delete instance (but keep volume for model caching)
	t.Cleanup(func() {
		t.Log("Cleanup: Deleting instance...")
		instanceMgr.DeleteInstance(ctx, inst.Id)
		// Note: We intentionally keep the volume so models persist between runs
	})

	// Step 8: Wait for instance to be ready
	t.Log("Step 8: Waiting for instance to be ready...")
	err = waitForInstanceReady(ctx, t, instanceMgr, inst.Id, 60*time.Second)
	require.NoError(t, err)
	t.Log("Instance is ready")

	// Get instance details for exec
	actualInst, err := instanceMgr.GetInstance(ctx, inst.Id)
	require.NoError(t, err)

	// Step 9: Wait for Ollama server to be ready (it starts automatically as container entrypoint)
	t.Log("Step 9: Waiting for Ollama server to be ready...")

	// Poll using `ollama list` which requires the server to be running
	ollamaReady := false
	for i := 0; i < 30; i++ { // 30 attempts, 1 second apart = 30 second timeout
		healthCtx, healthCancel := context.WithTimeout(ctx, 5*time.Second)
		var healthStdout, healthStderr inferenceOutputBuffer

		_, err = exec.ExecIntoInstance(healthCtx, actualInst.VsockSocket, exec.ExecOptions{
			Command: []string{"/bin/sh", "-c", "ollama list 2>&1"},
			Stdin:   nil,
			Stdout:  &healthStdout,
			Stderr:  &healthStderr,
			TTY:     false,
		})
		healthCancel()

		output := healthStdout.String()
		// Success: either shows "NAME" header (has models) or empty list
		// Failure: "could not connect" or other error
		if err == nil && !strings.Contains(output, "could not connect") && !strings.Contains(output, "error") {
			t.Logf("Ollama is ready (attempt %d): %s", i+1, strings.TrimSpace(output))
			ollamaReady = true
			break
		}
		if i < 5 || i%5 == 0 {
			t.Logf("Waiting for Ollama (attempt %d/30): output=%q, err=%v",
				i+1, strings.TrimSpace(output), err)
		}
		time.Sleep(1 * time.Second)
	}
	require.True(t, ollamaReady, "Ollama server should become ready within 30 seconds")

	// Check GPU detection status
	t.Log("Checking Ollama GPU detection...")
	gpuCheckCtx, gpuCheckCancel := context.WithTimeout(ctx, 10*time.Second)
	defer gpuCheckCancel()
	var gpuStdout, gpuStderr inferenceOutputBuffer
	_, _ = exec.ExecIntoInstance(gpuCheckCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", "cat /var/log/ollama.log 2>/dev/null | grep -i gpu || echo 'No GPU log found'"},
		Stdin:   nil,
		Stdout:  &gpuStdout,
		Stderr:  &gpuStderr,
		TTY:     false,
	})
	t.Logf("GPU detection status: %s", strings.TrimSpace(gpuStdout.String()))

	// Also check if nvidia-smi works (indicates driver is loaded)
	var nvidiaSmiStdout, nvidiaSmiStderr inferenceOutputBuffer
	_, nvidiaSmiErr := exec.ExecIntoInstance(gpuCheckCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", "nvidia-smi 2>&1 || echo 'nvidia-smi not available'"},
		Stdin:   nil,
		Stdout:  &nvidiaSmiStdout,
		Stderr:  &nvidiaSmiStderr,
		TTY:     false,
	})
	nvidiaSmiOutput := strings.TrimSpace(nvidiaSmiStdout.String())
	if nvidiaSmiErr == nil && !strings.Contains(nvidiaSmiOutput, "not available") && !strings.Contains(nvidiaSmiOutput, "command not found") {
		t.Logf("nvidia-smi output:\n%s", nvidiaSmiOutput)
	} else {
		t.Log("nvidia-smi not available - GPU passthrough works but NVIDIA drivers not loaded in guest")
		t.Log("(The ollama/ollama image doesn't include NVIDIA drivers)")
	}

	// Step 10: Pull model (separate from inference for better error detection)
	t.Log("Step 10: Pulling TinyLlama model...")

	// First check if model is already downloaded
	listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
	var listStdout, listStderr inferenceOutputBuffer
	_, _ = exec.ExecIntoInstance(listCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", "ollama list 2>&1"},
		Stdin:   nil,
		Stdout:  &listStdout,
		Stderr:  &listStderr,
		TTY:     false,
	})
	listCancel()

	modelList := strings.TrimSpace(listStdout.String())
	modelAlreadyCached := strings.Contains(modelList, "tinyllama")
	if modelAlreadyCached {
		t.Logf("Model already cached: %s", modelList)
	} else {
		t.Log("Model not cached - pulling now (~637MB)...")

		// Pull model with timeout - this downloads the model
		pullCtx, pullCancel := context.WithTimeout(ctx, 10*time.Minute)
		defer pullCancel()

		var pullStdout, pullStderr inferenceOutputBuffer
		_, pullErr := exec.ExecIntoInstance(pullCtx, actualInst.VsockSocket, exec.ExecOptions{
			Command: []string{"/bin/sh", "-c", "ollama pull tinyllama 2>&1"},
			Stdin:   nil,
			Stdout:  &pullStdout,
			Stderr:  &pullStderr,
			TTY:     false,
		})

		pullOutput := pullStdout.String()
		t.Logf("Pull output: %s", truncateTail(pullOutput, 500))

		if pullErr != nil {
			t.Logf("Pull stderr: %s", pullStderr.String())
			// Check for common error patterns
			if strings.Contains(pullOutput, "could not connect") {
				t.Fatal("Model pull failed: Ollama server not responding")
			}
			if strings.Contains(pullOutput, "error") || strings.Contains(pullOutput, "Error") {
				t.Fatalf("Model pull failed: %s", pullOutput)
			}
			require.NoError(t, pullErr, "ollama pull should succeed")
		}

		// Verify model was downloaded
		verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
		var verifyStdout, verifyStderr inferenceOutputBuffer
		_, _ = exec.ExecIntoInstance(verifyCtx, actualInst.VsockSocket, exec.ExecOptions{
			Command: []string{"/bin/sh", "-c", "ollama list 2>&1"},
			Stdin:   nil,
			Stdout:  &verifyStdout,
			Stderr:  &verifyStderr,
			TTY:     false,
		})
		verifyCancel()
		t.Logf("Models after pull: %s", strings.TrimSpace(verifyStdout.String()))
	}

	// Step 11: Run inference
	t.Log("Step 11: Running inference...")
	prompt := "Say hello in exactly 3 words"
	ollamaCmd := "ollama run tinyllama '" + prompt + "' 2>&1"

	t.Logf("Executing: %s", ollamaCmd)

	// CPU inference can be slow without GPU (2-3 minutes for small models)
	// GPU inference would be much faster but requires NVIDIA drivers in guest
	inferenceCtx, inferenceCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer inferenceCancel()

	var inferenceStdout, inferenceStderr inferenceOutputBuffer

	_, execErr := exec.ExecIntoInstance(inferenceCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", ollamaCmd},
		Stdin:   nil,
		Stdout:  &inferenceStdout,
		Stderr:  &inferenceStderr,
		TTY:     false,
	})

	inferenceOutput := inferenceStdout.String()
	t.Logf("Inference output: %s", inferenceOutput)

	if execErr != nil {
		// Print console log for debugging
		consoleLogPath := p.InstanceConsoleLog(inst.Id)
		if consoleLog, err := os.ReadFile(consoleLogPath); err == nil {
			t.Logf("=== VM Console Log (last 3000 chars) ===\n%s\n=== End Console Log ===",
				truncateTail(string(consoleLog), 3000))
		}
		t.Logf("Inference stderr: %s", inferenceStderr.String())
	}
	require.NoError(t, execErr, "ollama inference should succeed")

	// Step 12: Verify output
	output := inferenceStdout.String()
	t.Logf("Inference output:\n%s", output)

	// Verify we got some output (the model generated text)
	assert.NotEmpty(t, output, "Model should generate output")
	assert.True(t, len(output) > 5, "Model output should be more than a few characters")

	t.Log("✅ GPU inference test PASSED!")
	t.Log("Note: The ollama-models volume has been preserved for faster subsequent runs.")
	t.Logf("To clean up: sudo rm -rf %s", persistentTestDataDir)
}

// inferenceOutputBuffer is a simple buffer for capturing command output
type inferenceOutputBuffer struct {
	buf bytes.Buffer
}

func (b *inferenceOutputBuffer) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

func (b *inferenceOutputBuffer) String() string {
	return b.buf.String()
}

// truncateTail returns the last n characters of s
func truncateTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
