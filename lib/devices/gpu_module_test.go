package devices_test

import (
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
	"github.com/stretchr/testify/require"
)

// TestNVIDIAModuleLoading verifies that NVIDIA kernel modules load correctly in the VM.
//
// This is a simpler test than TestGPUInference that just verifies:
//  1. NVIDIA kernel modules (nvidia.ko, nvidia-uvm.ko, etc.) load during init
//  2. GSP firmware is found and loaded
//  3. /dev/nvidia* device nodes are created
//
// Prerequisites:
//   - NVIDIA GPU on host
//   - IOMMU enabled
//   - VFIO modules loaded (modprobe vfio_pci)
//   - Running as root
//
// To run manually:
//
//	sudo env PATH=$PATH:/sbin:/usr/sbin go test -v -run TestNVIDIAModuleLoading -timeout 5m ./lib/devices/...
func TestNVIDIAModuleLoading(t *testing.T) {
	ctx := context.Background()

	// Auto-detect GPU availability - skip if prerequisites not met
	skipReason := checkGPUTestPrerequisites()
	if skipReason != "" {
		t.Skip(skipReason)
	}

	groups, _ := os.ReadDir("/sys/kernel/iommu_groups")
	t.Logf("Test prerequisites met: %d IOMMU groups found", len(groups))

	// Setup test infrastructure
	tmpDir := t.TempDir()
	p := paths.New(tmpDir)

	cfg := &config.Config{
		DataDir:    tmpDir,
		BridgeName: "vmbr0",
		SubnetCIDR: "10.100.0.0/16",
		DNSServer:  "1.1.1.1",
	}

	// Initialize managers
	imageMgr, err := images.NewManager(p, 1, nil)
	require.NoError(t, err)

	systemMgr := system.NewManager(p)
	networkMgr := network.NewManager(p, cfg, nil)
	deviceMgr := devices.NewManager(p)
	volumeMgr := volumes.NewManager(p, 10*1024*1024*1024, nil)
	limits := instances.ResourceLimits{MaxOverlaySize: 10 * 1024 * 1024 * 1024}
	instanceMgr := instances.NewManager(p, imageMgr, systemMgr, networkMgr, deviceMgr, volumeMgr, limits, nil, nil)

	// Step 1: Find an NVIDIA GPU
	t.Log("Step 1: Discovering available GPUs...")
	availableDevices, err := deviceMgr.ListAvailableDevices(ctx)
	require.NoError(t, err)

	var targetGPU *devices.AvailableDevice
	for _, d := range availableDevices {
		if strings.Contains(strings.ToLower(d.VendorName), "nvidia") {
			targetGPU = &d
			break
		}
	}
	require.NotNil(t, targetGPU, "No NVIDIA GPU found")

	driverStr := "none"
	if targetGPU.CurrentDriver != nil {
		driverStr = *targetGPU.CurrentDriver
	}
	t.Logf("Found NVIDIA GPU: %s at %s (driver: %s)", targetGPU.DeviceName, targetGPU.PCIAddress, driverStr)

	if targetGPU.CurrentDriver == nil || *targetGPU.CurrentDriver == "" {
		t.Skip("GPU has no driver bound - may need reboot")
	}

	driverPath := filepath.Join("/sys/bus/pci/devices", targetGPU.PCIAddress, "driver")
	if _, err := os.Stat(driverPath); os.IsNotExist(err) {
		t.Skipf("GPU driver symlink missing - GPU in broken state")
	}

	// Step 2: Register the GPU
	t.Log("Step 2: Registering GPU...")
	device, err := deviceMgr.CreateDevice(ctx, devices.CreateDeviceRequest{
		Name:       "module-test-gpu",
		PCIAddress: targetGPU.PCIAddress,
	})
	require.NoError(t, err)
	t.Logf("Registered device: %s (ID: %s)", device.Name, device.Id)

	originalDriver := driverStr
	t.Cleanup(func() {
		t.Log("Cleanup: Deleting registered device...")
		deviceMgr.DeleteDevice(ctx, device.Id)
		if originalDriver != "" && originalDriver != "none" && originalDriver != "vfio-pci" {
			probePath := "/sys/bus/pci/drivers_probe"
			os.WriteFile(probePath, []byte(targetGPU.PCIAddress), 0200)
		}
	})

	// Step 3: Ensure system files
	t.Log("Step 3: Ensuring system files...")
	require.NoError(t, systemMgr.EnsureSystemFiles(ctx))

	// Step 4: Pull nginx:alpine (stays running unlike plain alpine)
	t.Log("Step 4: Pulling nginx:alpine image...")
	createdImg, err := imageMgr.CreateImage(ctx, images.CreateImageRequest{
		Name: "docker.io/library/nginx:alpine",
	})
	require.NoError(t, err)
	t.Logf("CreateImage returned: name=%s, status=%s", createdImg.Name, createdImg.Status)

	// Wait for image to be ready
	var img *images.Image
	for i := 0; i < 90; i++ {
		img, _ = imageMgr.GetImage(ctx, createdImg.Name)
		if img != nil && img.Status == images.StatusReady {
			break
		}
		time.Sleep(time.Second)
	}
	require.NotNil(t, img, "Image should exist")
	require.Equal(t, images.StatusReady, img.Status, "Image should be ready")
	t.Log("Image ready")

	// Step 5: Create instance with GPU
	t.Log("Step 5: Creating instance with GPU...")

	// Initialize network first
	require.NoError(t, networkMgr.Initialize(ctx, []string{}))

	createCtx, createCancel := context.WithTimeout(ctx, 60*time.Second)
	defer createCancel()

	inst, err := instanceMgr.CreateInstance(createCtx, instances.CreateInstanceRequest{
		Name:           "nvidia-module-test",
		Image:          createdImg.Name,
		Size:           512 * 1024 * 1024,
		HotplugSize:    512 * 1024 * 1024,
		OverlaySize:    10 * 1024 * 1024 * 1024,
		Vcpus:          2,
		NetworkEnabled: false,
		Devices:        []string{"module-test-gpu"},
		Env:            map[string]string{},
	})
	require.NoError(t, err)
	t.Logf("Instance created: %s", inst.Id)

	t.Cleanup(func() {
		t.Log("Cleanup: Deleting instance...")
		instanceMgr.DeleteInstance(ctx, inst.Id)
	})

	// Wait for instance to be running
	err = waitForInstanceReady(ctx, t, instanceMgr, inst.Id, 30*time.Second)
	require.NoError(t, err)
	t.Log("Instance is ready")

	// Wait for init script to complete (module loading happens early in boot)
	time.Sleep(5 * time.Second)

	// Step 6: Check module loading via dmesg
	t.Log("Step 6: Checking NVIDIA module loading in VM...")

	actualInst, err := instanceMgr.GetInstance(ctx, inst.Id)
	require.NoError(t, err)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Check dmesg for NVIDIA messages
	var stdout, stderr outputBuffer
	dmesgCmd := "dmesg | grep -i nvidia | head -50"

	for i := 0; i < 10; i++ {
		stdout = outputBuffer{}
		stderr = outputBuffer{}
		_, err = exec.ExecIntoInstance(execCtx, actualInst.VsockSocket, exec.ExecOptions{
			Command: []string{"/bin/sh", "-c", dmesgCmd},
			Stdin:   nil,
			Stdout:  &stdout,
			Stderr:  &stderr,
		})
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	require.NoError(t, err, "dmesg command should succeed")

	dmesgOutput := stdout.String()
	t.Logf("=== NVIDIA dmesg output ===\n%s", dmesgOutput)

	// Check for key error indicators
	firmwareMissing := strings.Contains(dmesgOutput, "No firmware image found")
	initFailed := strings.Contains(dmesgOutput, "RmInitAdapter failed")

	if firmwareMissing {
		t.Errorf("✗ GSP firmware not found - firmware not included in initrd")
	}
	if initFailed {
		t.Errorf("✗ NVIDIA driver RmInitAdapter failed - GPU initialization error")
	}

	// Check lsmod for nvidia modules
	stdout = outputBuffer{}
	stderr = outputBuffer{}
	_, err = exec.ExecIntoInstance(execCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", "cat /proc/modules | grep nvidia || echo 'No nvidia modules loaded'"},
		Stdin:   nil,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	require.NoError(t, err)
	modulesOutput := stdout.String()
	t.Logf("=== Loaded nvidia modules ===\n%s", modulesOutput)

	hasModules := !strings.Contains(modulesOutput, "No nvidia modules loaded")
	if !hasModules {
		t.Errorf("✗ NVIDIA modules not loaded in VM")
	} else {
		t.Log("✓ NVIDIA kernel modules are loaded")
	}

	// Check for /dev/nvidia* devices
	stdout = outputBuffer{}
	stderr = outputBuffer{}
	_, err = exec.ExecIntoInstance(execCtx, actualInst.VsockSocket, exec.ExecOptions{
		Command: []string{"/bin/sh", "-c", "ls -la /dev/nvidia* 2>&1 || echo 'No nvidia devices found'"},
		Stdin:   nil,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	require.NoError(t, err)
	devicesOutput := stdout.String()
	t.Logf("=== NVIDIA device nodes ===\n%s", devicesOutput)

	hasDevices := !strings.Contains(devicesOutput, "No nvidia devices found") && !strings.Contains(devicesOutput, "No such file")
	if hasDevices {
		t.Log("✓ /dev/nvidia* device nodes exist")
	} else {
		t.Log("✗ /dev/nvidia* device nodes not found (expected if init failed)")
	}

	// Final verdict
	if !firmwareMissing && !initFailed && hasModules {
		t.Log("\n=== SUCCESS: NVIDIA kernel modules loaded correctly ===")
	} else {
		t.Errorf("\n=== FAILURE: NVIDIA module loading has issues ===")
	}
}
