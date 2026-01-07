# Generic Builder Image

The generic builder image runs inside Hypeman microVMs to execute source-to-image builds using BuildKit. It is runtime-agnostic - users provide their own Dockerfile which specifies the runtime.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Generic Builder Image (~50MB)                               │
│                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ BuildKit    │  │ builder-    │  │ Minimal Alpine      │ │
│  │ (daemonless)│  │ agent       │  │ (git, curl, fuse)   │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
                    User's Dockerfile
                            │
                            ▼
            ┌───────────────────────────────┐
            │ FROM node:20-alpine           │
            │ FROM python:3.12-slim         │
            │ FROM rust:1.75                │
            │ FROM golang:1.22              │
            │ ... any base image            │
            └───────────────────────────────┘
```

## Key Benefits

- **One image to maintain** - No more runtime-specific builder images
- **Any Dockerfile works** - Node.js, Python, Rust, Go, Java, Ruby, etc.
- **Smaller footprint** - ~50MB vs 200MB+ for runtime-specific images
- **User-controlled versions** - Users specify their runtime version in their Dockerfile

## Directory Structure

```
images/
└── generic/
    └── Dockerfile    # The generic builder image
```

## Building the Generic Builder Image

> **Important**: Hypeman uses `umoci` for OCI image manipulation, which requires images
> to have **OCI manifest format** (not Docker v2 format). You must use `docker buildx`
> with the `oci-mediatypes=true` option.

### Prerequisites

1. **Docker Buildx** with a container builder:
   ```bash
   # Create a buildx builder (if you don't have one)
   docker buildx create --name ocibuilder --use
   ```

2. **Docker Hub login** (or your registry):
   ```bash
   docker login
   ```

### 1. Build and Push with OCI Format

```bash
# From repository root
docker buildx build \
  --platform linux/amd64 \
  --output "type=registry,oci-mediatypes=true" \
  --tag hirokernel/builder-generic:latest \
  -f lib/builds/images/generic/Dockerfile \
  .
```

This command:
- Builds for `linux/amd64` platform
- Uses `oci-mediatypes=true` to create OCI manifests (required for Hypeman)
- Pushes directly to the registry

### 2. Verify the Manifest Format

```bash
# Should show "application/vnd.oci.image.index.v1+json"
docker manifest inspect hirokernel/builder-generic:latest | head -5
```

Expected output:
```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  ...
}
```

### 3. Import into Hypeman

```bash
# Generate a token
TOKEN=$(make gen-jwt | tail -1)

# Import the image
curl -X POST http://localhost:8083/images \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "hirokernel/builder-generic:latest"}'

# Wait for import to complete
curl http://localhost:8083/images/docker.io%2Fhirokernel%2Fbuilder-generic:latest \
  -H "Authorization: Bearer $TOKEN"
```

### 4. Configure Hypeman

Set the builder image in your `.env`:

```bash
BUILDER_IMAGE=hirokernel/builder-generic:latest
```

### Why OCI Format is Required

| Build Method | Manifest Type | Works with Hypeman? |
|--------------|---------------|---------------------|
| `docker build` | Docker v2 (`application/vnd.docker.distribution.manifest.v2+json`) | ❌ No |
| `docker buildx --output type=docker` | Docker v2 | ❌ No |
| `docker buildx --output type=registry,oci-mediatypes=true` | OCI (`application/vnd.oci.image.index.v1+json`) | ✅ Yes |

Hypeman uses `umoci` to extract and convert OCI images to ext4 disk images for microVMs.
`umoci` strictly requires OCI-format manifests and cannot parse Docker v2 manifests.

### Building for Local Testing (without pushing)

If you need to test locally before pushing:

```bash
# Build and load to local Docker (for testing only - won't work with Hypeman import)
docker build \
  -t hypeman/builder:local \
  -f lib/builds/images/generic/Dockerfile \
  .

# Run locally to test
docker run --rm hypeman/builder:local --help
```

**Note**: Images built with `docker build` cannot be imported into Hypeman directly.
You must rebuild with `docker buildx --output type=registry,oci-mediatypes=true`
before deploying to Hypeman.

## Usage

### Submitting a Build

Users must provide a Dockerfile either:
1. **In the source tarball** - Include a `Dockerfile` in the root of the source
2. **As a parameter** - Pass `dockerfile` content in the API request

```bash
# Option 1: Dockerfile in source tarball
tar -czf source.tar.gz Dockerfile package.json index.js

curl -X POST http://localhost:8083/builds \
  -H "Authorization: Bearer $TOKEN" \
  -F "source=@source.tar.gz"

# Option 2: Dockerfile as parameter
curl -X POST http://localhost:8083/builds \
  -H "Authorization: Bearer $TOKEN" \
  -F "source=@source.tar.gz" \
  -F "dockerfile=FROM node:20-alpine
WORKDIR /app
COPY . .
RUN npm ci
CMD [\"node\", \"index.js\"]"
```

### Example Dockerfiles

**Node.js:**
```dockerfile
FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["node", "index.js"]
```

**Python:**
```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
CMD ["python", "main.py"]
```

**Go:**
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o main .

FROM alpine:3.21
COPY --from=builder /app/main /main
CMD ["/main"]
```

**Rust:**
```dockerfile
FROM rust:1.75 AS builder
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM debian:bookworm-slim
COPY --from=builder /app/target/release/myapp /myapp
CMD ["/myapp"]
```

## Required Components

The generic builder image contains:

| Component | Path | Purpose |
|-----------|------|---------|
| `buildctl` | `/usr/bin/buildctl` | BuildKit CLI |
| `buildctl-daemonless.sh` | `/usr/bin/buildctl-daemonless.sh` | Runs buildkitd + buildctl |
| `buildkitd` | `/usr/bin/buildkitd` | BuildKit daemon |
| `runc` | `/usr/bin/runc` | Container runtime |
| `builder-agent` | `/usr/bin/builder-agent` | Hypeman orchestration |
| `fuse-overlayfs` | System package | Rootless overlay filesystem |
| `git` | System package | Git operations (for go mod, etc.) |
| `curl` | System package | Network utilities |

## Environment Variables

| Variable | Value | Purpose |
|----------|-------|---------|
| `HOME` | `/home/builder` | User home directory |
| `XDG_RUNTIME_DIR` | `/home/builder/.local/share` | Runtime directory for BuildKit |
| `BUILDKITD_FLAGS` | `""` (empty) | BuildKit daemon flags |

## MicroVM Runtime Environment

When the builder runs inside a Hypeman microVM:

1. **Volumes mounted**:
   - `/src` - Source code (read-write)
   - `/config/build.json` - Build configuration (read-only)

2. **Cgroups**: Mounted at `/sys/fs/cgroup`

3. **Network**: Access to host registry via gateway IP `10.102.0.1`

4. **Registry**: Uses HTTP (insecure) with `registry.insecure=true`

## Troubleshooting

| Issue | Cause | Solution |
|-------|-------|----------|
| `manifest data is not v1.Manifest` | Image built with Docker v2 format | Rebuild with `docker buildx --output type=registry,oci-mediatypes=true` |
| Image import stuck on `pending`/`failed` | Manifest format incompatible | Check manifest format with `docker manifest inspect` |
| `Dockerfile required` | No Dockerfile in source or parameter | Include Dockerfile in tarball or pass as parameter |
| `401 Unauthorized` during push | Registry token issue | Check builder agent logs, verify token generation |
| `runc: not found` | BuildKit binaries missing | Rebuild the builder image |
| `no cgroup mount found` | Cgroups not available | Check VM init script |
| `fuse-overlayfs: not found` | Missing package | Rebuild image with fuse-overlayfs |
| `permission denied` | Wrong user/permissions | Ensure running as `builder` user |

### Debugging Image Import Issues

```bash
# Check image status
cat ~/hypeman_data_dir/images/docker.io/hirokernel/builder-generic/*/metadata.json | jq .

# Check OCI cache for manifest format
cat ~/hypeman_data_dir/system/oci-cache/index.json | jq '.manifests[-1]'

# Verify image on Docker Hub has OCI format
skopeo inspect --raw docker://hirokernel/builder-generic:latest | head -5
```

If you see `application/vnd.docker.distribution.manifest.v2+json`, the image needs to be rebuilt with OCI format.

## Migration from Runtime-Specific Images

If you were using `nodejs20` or `python312` builder images:

1. **Update your build requests** to include a Dockerfile
2. **The `runtime` parameter is deprecated** - you can still send it but it's ignored
3. **Configure `BUILDER_IMAGE`** to use the generic builder
