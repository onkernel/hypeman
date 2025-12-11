package system

import "runtime"

// KernelVersion represents a Cloud Hypervisor kernel version
type KernelVersion string

const (
	// Kernel versions from onkernel/linux releases
	Kernel_202511182 KernelVersion = "ch-6.12.8-kernel-1-202511182"
	Kernel_20251210  KernelVersion = "ch-6.12.8-kernel-2-20251210" // First kernel with NVIDIA module support
)

var (
	// DefaultKernelVersion is the kernel version used for new instances
	DefaultKernelVersion = Kernel_20251210

	// SupportedKernelVersions lists all supported kernel versions
	SupportedKernelVersions = []KernelVersion{
		Kernel_202511182,
		Kernel_20251210,
		// Add future versions here
	}
)

// KernelDownloadURLs maps kernel versions and architectures to download URLs
var KernelDownloadURLs = map[KernelVersion]map[string]string{
	Kernel_202511182: {
		"x86_64":  "https://github.com/onkernel/linux/releases/download/ch-6.12.8-kernel-1-202511182/vmlinux-x86_64",
		"aarch64": "https://github.com/onkernel/linux/releases/download/ch-6.12.8-kernel-1-202511182/Image-arm64",
	},
	Kernel_20251210: {
		"x86_64":  "https://github.com/onkernel/linux/releases/download/ch-6.12.8-kernel-2-20251210/vmlinux-x86_64",
		"aarch64": "https://github.com/onkernel/linux/releases/download/ch-6.12.8-kernel-2-20251210/Image-arm64",
	},
	// Add future versions here
}

// NvidiaModuleURLs maps kernel versions and architectures to NVIDIA module tarball URLs
// These tarballs contain pre-built NVIDIA kernel modules that match the kernel version
var NvidiaModuleURLs = map[KernelVersion]map[string]string{
	Kernel_20251210: {
		"x86_64": "https://github.com/onkernel/linux/releases/download/ch-6.12.8-kernel-2-20251210/nvidia-modules-x86_64.tar.gz",
		// Note: NVIDIA open-gpu-kernel-modules does not support arm64 yet
	},
	// Kernel_202511182 does not have NVIDIA modules (pre-module-support kernel)
}

// NvidiaDriverVersion tracks the NVIDIA driver version bundled with each kernel
var NvidiaDriverVersion = map[KernelVersion]string{
	Kernel_20251210: "570.86.16",
	// Kernel_202511182 does not have NVIDIA modules
}

// GetArch returns the architecture string for the current platform
func GetArch() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		return "x86_64"
	}
	return arch
}
