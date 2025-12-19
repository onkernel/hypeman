# Build System

The build system provides source-to-image builds inside ephemeral Cloud Hypervisor microVMs, enabling secure multi-tenant isolation with rootless BuildKit.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Hypeman API                              │
│  POST /builds  →  BuildManager  →  BuildQueue                   │
│                        │                                         │
│              Start() → VsockHandler (port 5001)                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Builder MicroVM                              │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  Volumes Mounted:                                            ││
│  │  - /src (source code, read-write)                           ││
│  │  - /config/build.json (build configuration, read-only)      ││
│  ├─────────────────────────────────────────────────────────────┤│
│  │  Builder Agent                                               ││
│  │  ┌─────────────┐  ┌──────────────┐  ┌────────────────────┐  ││
│  │  │ Load Config │→ │ Generate     │→ │ Run BuildKit       │  ││
│  │  │ /config/    │  │ Dockerfile   │  │ (buildctl)         │  ││
│  │  └─────────────┘  └──────────────┘  └────────────────────┘  ││
│  │                                              │               ││
│  │                                              ▼               ││
│  │                                     Push to Registry         ││
│  │                                     (HTTP, insecure)         ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       OCI Registry                               │
│              {REGISTRY_URL}/builds/{build-id}                    │
│              (default: 10.102.0.1:8083 from VM)                 │
└─────────────────────────────────────────────────────────────────┘
```

## Components

### Core Types (`types.go`)

| Type | Description |
|------|-------------|
| `Build` | Build job status and metadata |
| `CreateBuildRequest` | API request to create a build |
| `BuildConfig` | Configuration passed to builder VM |
| `BuildResult` | Result returned by builder agent |
| `BuildProvenance` | Audit trail for reproducibility |
| `BuildPolicy` | Resource limits and network policy |

### Build Queue (`queue.go`)

In-memory queue with configurable concurrency:

```go
queue := NewBuildQueue(maxConcurrent)
position := queue.Enqueue(buildID, request, startFunc)
queue.Cancel(buildID)
queue.GetPosition(buildID)
```

**Recovery**: On startup, `listPendingBuilds()` scans disk metadata for incomplete builds and re-enqueues them in FIFO order.

### Storage (`storage.go`)

Builds are persisted to `$DATA_DIR/builds/{id}/`:

```
builds/
└── {build-id}/
    ├── metadata.json    # Build status, provenance
    ├── config.json      # Config for builder VM
    ├── source/
    │   └── source.tar.gz
    └── logs/
        └── build.log
```

### Build Manager (`manager.go`)

Orchestrates the build lifecycle:

1. Validate request and store source
2. Write build config to disk
3. Enqueue build job
4. Create source volume from archive
5. Create config volume with `build.json`
6. Create builder VM with both volumes attached
7. Wait for build completion
8. Update metadata and cleanup

**Important**: The `Start()` method must be called to start the vsock handler for builder communication.

### Dockerfile Templates (`templates/`)

Auto-generates Dockerfiles based on runtime and detected lockfiles:

| Runtime | Package Managers |
|---------|-----------------|
| `nodejs20` | npm, yarn, pnpm |
| `python312` | pip, poetry, pipenv |

```go
gen, _ := templates.GetGenerator("nodejs20")
dockerfile, _ := gen.Generate(sourceDir, baseImageDigest)
```

### Cache System (`cache.go`)

Registry-based caching with tenant isolation:

```
{registry}/cache/{tenant_scope}/{runtime}/{lockfile_hash}
```

```go
gen := NewCacheKeyGenerator("localhost:8080")
key, _ := gen.GenerateCacheKey("my-tenant", "nodejs20", lockfileHashes)
// key.ImportCacheArg() → "type=registry,ref=localhost:8080/cache/my-tenant/nodejs20/abc123"
// key.ExportCacheArg() → "type=registry,ref=localhost:8080/cache/my-tenant/nodejs20/abc123,mode=max"
```

### Builder Agent (`builder_agent/main.go`)

Guest binary that runs inside builder VMs:

1. Reads config from `/config/build.json`
2. Fetches secrets from host via vsock (if any)
3. Generates Dockerfile (if not provided)
4. Runs `buildctl-daemonless.sh` with cache and insecure registry flags
5. Computes provenance (lockfile hashes, source hash)
6. Reports result back via vsock

**Key Details**:
- Config path: `/config/build.json`
- Source path: `/src`
- Uses `registry.insecure=true` for HTTP registries
- Inherits `BUILDKITD_FLAGS` from environment

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/builds` | Submit build (multipart form) |
| `GET` | `/builds` | List all builds |
| `GET` | `/builds/{id}` | Get build details |
| `DELETE` | `/builds/{id}` | Cancel build |
| `GET` | `/builds/{id}/logs` | Stream logs (SSE) |

### Submit Build Example

```bash
curl -X POST http://localhost:8083/builds \
  -H "Authorization: Bearer $TOKEN" \
  -F "runtime=nodejs20" \
  -F "source=@source.tar.gz" \
  -F "cache_scope=tenant-123" \
  -F "timeout_seconds=300"
```

### Response

```json
{
  "id": "abc123",
  "status": "queued",
  "runtime": "nodejs20",
  "queue_position": 1,
  "created_at": "2025-01-15T10:00:00Z"
}
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `MAX_CONCURRENT_SOURCE_BUILDS` | `2` | Max parallel builds |
| `BUILDER_IMAGE` | `hypeman/builder:latest` | Builder VM image |
| `REGISTRY_URL` | `localhost:8080` | Registry for built images |
| `BUILD_TIMEOUT` | `600` | Default timeout (seconds) |

### Registry URL Configuration

The `REGISTRY_URL` must be accessible from inside builder VMs. Since `localhost` in the VM refers to the VM itself, you need to use the host's gateway IP:

```bash
# In .env
REGISTRY_URL=10.102.0.1:8083  # Gateway IP accessible from VM network
```

The middleware allows unauthenticated registry pushes from the VM network (10.102.x.x).

## Build Status Flow

```
queued → building → pushing → ready
                 ↘         ↗
                   failed
                      ↑
                  cancelled
```

## Security Model

1. **Isolation**: Each build runs in a fresh microVM (Cloud Hypervisor)
2. **Rootless**: BuildKit runs without root privileges
3. **Network Control**: `network_mode: isolated` or `egress` with optional domain allowlist
4. **Secret Handling**: Secrets fetched via vsock, never written to disk in guest
5. **Cache Isolation**: Per-tenant cache scopes prevent cross-tenant cache poisoning

## Builder Images

Builder images are in `images/`:

- `base/Dockerfile` - BuildKit base
- `nodejs20/Dockerfile` - Node.js 20 + BuildKit + agent
- `python312/Dockerfile` - Python 3.12 + BuildKit + agent

### Required Components

Builder images must include:

| Component | Source | Purpose |
|-----------|--------|---------|
| `buildctl` | `moby/buildkit:rootless` | BuildKit CLI |
| `buildctl-daemonless.sh` | `moby/buildkit:rootless` | Daemonless wrapper |
| `buildkitd` | `moby/buildkit:rootless` | BuildKit daemon |
| `buildkit-runc` | `moby/buildkit:rootless` | Container runtime (as `/usr/bin/runc`) |
| `builder-agent` | Built from `builder_agent/main.go` | Hypeman agent |
| `fuse-overlayfs` | apk/apt | Overlay filesystem support |

### Build and Push (OCI Format)

Builder images must be pushed in OCI format (not Docker v2 manifest):

```bash
# Build with OCI output
docker buildx build --platform linux/amd64 \
  -t myregistry/builder-nodejs20:latest \
  -f lib/builds/images/nodejs20/Dockerfile \
  --output type=oci,dest=/tmp/builder.tar \
  .

# Extract and push with crane
mkdir -p /tmp/oci-builder
tar -xf /tmp/builder.tar -C /tmp/oci-builder
crane push /tmp/oci-builder myregistry/builder-nodejs20:latest
```

### Environment Variables

The builder image should set:

```dockerfile
# Empty or minimal flags - cgroups are mounted in microVM
ENV BUILDKITD_FLAGS=""
ENV HOME=/home/builder
ENV XDG_RUNTIME_DIR=/home/builder/.local/share
```

## MicroVM Requirements

Builder VMs require specific kernel and init script features:

### Cgroups

The init script mounts cgroups for BuildKit/runc:

```bash
# Cgroup v2 (preferred)
mount -t cgroup2 none /sys/fs/cgroup

# Or cgroup v1 fallback
mount -t tmpfs cgroup /sys/fs/cgroup
for ctrl in cpu cpuacct memory devices freezer blkio pids; do
  mkdir -p /sys/fs/cgroup/$ctrl
  mount -t cgroup -o $ctrl cgroup /sys/fs/cgroup/$ctrl
done
```

### Volume Mounts

Two volumes are attached to builder VMs:

1. **Source volume** (`/src`, read-write): Contains extracted source tarball
2. **Config volume** (`/config`, read-only): Contains `build.json`

The source is mounted read-write so the generated Dockerfile can be written.

## Provenance

Each build records provenance for reproducibility:

```json
{
  "base_image_digest": "sha256:abc123...",
  "source_hash": "sha256:def456...",
  "lockfile_hashes": {
    "package-lock.json": "sha256:..."
  },
  "toolchain_version": "v20.10.0",
  "buildkit_version": "v0.12.0",
  "timestamp": "2025-01-15T10:05:00Z"
}
```

## Testing

### Unit Tests

```bash
# Run unit tests
go test ./lib/builds/... -v

# Test specific components
go test ./lib/builds/queue_test.go ./lib/builds/queue.go ./lib/builds/types.go -v
go test ./lib/builds/cache_test.go ./lib/builds/cache.go ./lib/builds/types.go ./lib/builds/errors.go -v
go test ./lib/builds/templates/... -v
```

### E2E Testing

1. **Start the server**:
   ```bash
   make dev
   ```

2. **Ensure builder image is available**:
   ```bash
   TOKEN=$(make gen-jwt | tail -1)
   curl -X POST http://localhost:8083/images \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"name": "hirokernel/builder-nodejs20:latest"}'
   ```

3. **Create test source**:
   ```bash
   mkdir -p /tmp/test-app
   echo '{"name": "test", "version": "1.0.0", "dependencies": {}}' > /tmp/test-app/package.json
   echo '{"lockfileVersion": 3, "packages": {}}' > /tmp/test-app/package-lock.json
   echo 'console.log("Hello!");' > /tmp/test-app/index.js
   tar -czf /tmp/source.tar.gz -C /tmp/test-app .
   ```

4. **Submit build**:
   ```bash
   curl -X POST http://localhost:8083/builds \
     -H "Authorization: Bearer $TOKEN" \
     -F "runtime=nodejs20" \
     -F "source=@/tmp/source.tar.gz"
   ```

5. **Poll for completion**:
   ```bash
   BUILD_ID="<id-from-response>"
   curl http://localhost:8083/builds/$BUILD_ID \
     -H "Authorization: Bearer $TOKEN"
   ```

6. **Run the built image**:
   ```bash
   curl -X POST http://localhost:8083/instances \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{
       "name": "test-app",
       "image": "builds/'$BUILD_ID':latest",
       "size": "1GB",
       "vcpus": 1
     }'
   ```

## Troubleshooting

### Common Issues

| Error | Cause | Solution |
|-------|-------|----------|
| `image not found` | Builder image not in OCI format | Push with `crane` after `docker buildx --output type=oci` |
| `no cgroup mount found` | Cgroups not mounted in VM | Update init script to mount cgroups |
| `http: server gave HTTP response to HTTPS client` | BuildKit using HTTPS for HTTP registry | Add `registry.insecure=true` to output flags |
| `connection refused` to localhost:8080 | Registry URL not accessible from VM | Use gateway IP (10.102.0.1) instead of localhost |
| `authorization header required` | Registry auth blocking VM push | Ensure auth bypass for 10.102.x.x IPs |
| `No space left on device` | Instance memory too small for image | Use at least 1GB RAM for Node.js images |
| `can't enable NoProcessSandbox without Rootless` | Wrong BUILDKITD_FLAGS | Use empty flags or remove the flag |

### Debug Builder VM

Check logs of the builder instance:

```bash
# List instances
curl http://localhost:8083/instances -H "Authorization: Bearer $TOKEN" | jq

# Get builder instance logs
INSTANCE_ID="<builder-instance-id>"
curl http://localhost:8083/instances/$INSTANCE_ID/logs \
  -H "Authorization: Bearer $TOKEN"
```

### Verify Build Config

Check the config volume contents:

```bash
cat $DATA_DIR/builds/$BUILD_ID/config.json
```

Expected format:
```json
{
  "job_id": "abc123",
  "runtime": "nodejs20",
  "registry_url": "10.102.0.1:8083",
  "cache_scope": "my-tenant",
  "source_path": "/src",
  "timeout_seconds": 300,
  "network_mode": "egress"
}
```
