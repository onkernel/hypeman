# System Manager

Manages versioned kernel and initrd files for Cloud Hypervisor VMs.

## Features

- **Automatic Downloads**: Kernel downloaded from Cloud Hypervisor releases on first use
- **Automatic Build**: Initrd built from Alpine base + Go init binary + guest-agent
- **Versioned**: Side-by-side support for multiple kernel/initrd versions
- **Zero Docker**: Uses OCI directly (reuses image manager infrastructure)
- **Zero Image Modifications**: All init logic in initrd, OCI images used as-is
- **Dual Mode Support**: Exec mode (container-like) and systemd mode (full VM)

## Architecture

### Storage Layout

```
{dataDir}/system/
├── kernel/
│   ├── ch-v6.12.8/
│   │   ├── x86_64/vmlinux   (~70MB)
│   │   └── aarch64/Image    (~70MB)
│   └── ch-v6.12.9/
│       └── ... (future version)
├── initrd/
│   ├── v1.0.0/
│   │   ├── x86_64/initrd    (~1-2MB)
│   │   └── aarch64/initrd   (~1-2MB)
│   └── v1.1.0/
│       └── ... (when init script changes)
└── oci-cache/              (shared with images manager)
    └── blobs/sha256/       (busybox layers cached)
```

### Versioning Rules

**Snapshots require exact matches:**
```
Standby:  kernel v6.12.9, initrd v1.0.0, CH v49.0
Restore:  kernel v6.12.9, initrd v1.0.0, CH v49.0 (MUST match)
```

**Maintenance upgrades (shutdown → boot):**
```
1. Update DefaultKernelVersion in versions.go
2. Shutdown instance
3. Boot instance (uses new kernel/initrd)
```

**Multi-version support:**
```
Instance A (standby): kernel v6.12.8, initrd v1.0.0
Instance B (running): kernel v6.12.9, initrd v1.0.0
Both work independently
```

## Go Init Binary

The init binary (`lib/system/init/`) is a Go program that runs as PID 1 in the guest VM.
It replaces the previous shell-based init script with cleaner logic and structured logging.

**Initrd handles:**
- ✅ Mount overlay filesystem
- ✅ Mount and source config disk
- ✅ Network configuration (if enabled)
- ✅ Load GPU drivers (if GPU attached)
- ✅ Mount volumes
- ✅ Auto-detect systemd images from CMD
- ✅ Execute container entrypoint (exec mode)
- ✅ Hand off to systemd via pivot_root (systemd mode)

**Two boot modes:**
- **Exec mode** (default): Init binary is PID 1, runs entrypoint as child process
- **Systemd mode** (auto-detected): Uses pivot_root to hand off to systemd as PID 1

**Systemd detection:** If image CMD is `/sbin/init`, `/lib/systemd/systemd`, or similar,
hypeman automatically uses systemd mode.

**Result:** OCI images require **zero modifications** - no `/init` script needed!

## Usage

### Application Startup

```go
// cmd/api/main.go
systemMgr := system.NewManager(dataDir)

// Ensure files exist (download/build if needed)
err := systemMgr.EnsureSystemFiles(ctx)

// Files are ready, instances can be created
```

### Instance Creation

```go
// Instances manager uses system manager automatically
inst, err := instanceManager.CreateInstance(ctx, req)
// Uses default kernel/initrd versions
// Versions stored in instance metadata for restore compatibility
```

### Get File Paths

```go
kernelPath, _ := systemMgr.GetKernelPath(system.KernelV6_12_9)
initrdPath, _ := systemMgr.GetInitrdPath(system.InitrdV1_0_0)
```

## Kernel Sources

Kernels downloaded from Cloud Hypervisor releases:
- https://github.com/cloud-hypervisor/linux/releases

Example URLs:
- x86_64: `https://github.com/cloud-hypervisor/linux/releases/download/ch-v6.12.9/vmlinux-x86_64`
- aarch64: `https://github.com/cloud-hypervisor/linux/releases/download/ch-v6.12.9/Image-aarch64`

## Initrd Build Process

1. **Pull Alpine base** (using image manager's OCI client)
2. **Add guest-agent binary** (embedded, runs in guest for exec/shell)
3. **Add init binary** (embedded Go binary, runs as PID 1)
4. **Add NVIDIA modules** (optional, for GPU passthrough)
5. **Package as cpio.gz** (initramfs format)

**Build tools required:** `find`, `cpio`, `gzip` (standard Unix tools)

## Adding New Versions

### New Kernel Version

```go
// lib/system/versions.go

const (
    KernelV6_12_10 KernelVersion = "ch-v6.12.10"  // Add constant
)

var KernelDownloadURLs = map[KernelVersion]map[string]string{
    // ... existing ...
    KernelV6_12_10: {
        "x86_64":  "https://github.com/cloud-hypervisor/linux/releases/download/ch-v6.12.10/vmlinux-x86_64",
        "aarch64": "https://github.com/cloud-hypervisor/linux/releases/download/ch-v6.12.10/Image-aarch64",
    },
}

// Update default if needed
var DefaultKernelVersion = KernelV6_12_10
```

### Updating the Init Binary

The init binary is in `lib/system/init/`. After making changes:

1. Build the init binary (statically linked for Alpine):
   ```bash
   make build-init
   ```

2. The binary is embedded via `lib/system/init_binary.go`

3. The initrd hash includes the binary, so it will auto-rebuild on next startup

## Testing

```bash
# Unit tests (no downloads)
go test -short ./lib/system/...

# Integration tests (downloads kernel, builds initrd)
go test ./lib/system/...
```

## Files Generated

| File | Size | Purpose |
|------|------|---------|
| kernel/*/vmlinux | ~70MB | Cloud Hypervisor optimized kernel |
| initrd/*/initrd | ~5-10MB | Alpine base + Go init binary + guest-agent |

Files downloaded/built once per version, reused for all instances using that version.

## Init Binary Package Structure

```
lib/system/init/
    main.go           # Entry point, orchestrates boot
    mount.go          # Mount operations (proc, sys, dev, overlay)
    config.go         # Parse config disk
    network.go        # Network configuration
    drivers.go        # GPU driver loading
    volumes.go        # Volume mounting
    mode_exec.go      # Exec mode: run entrypoint
    mode_systemd.go   # Systemd mode: pivot_root + exec init
    logger.go         # Human-readable logging to hypeman operations log
```

