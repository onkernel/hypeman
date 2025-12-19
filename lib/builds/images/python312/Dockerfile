# Python 3.12 Builder Image
# Contains rootless BuildKit + Python toolchain + builder agent

FROM moby/buildkit:rootless AS buildkit

# Build the builder-agent (multi-stage build from hypeman repo)
FROM golang:1.25-alpine AS agent-builder

WORKDIR /app

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy only the builder_agent source
COPY lib/builds/builder_agent/ ./lib/builds/builder_agent/

# Build the agent
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /builder-agent ./lib/builds/builder_agent

# Final builder image
FROM python:3.12-slim

# Copy BuildKit from official image
COPY --from=buildkit /usr/bin/buildctl /usr/bin/buildctl
COPY --from=buildkit /usr/bin/buildctl-daemonless.sh /usr/bin/buildctl-daemonless.sh
COPY --from=buildkit /usr/bin/buildkitd /usr/bin/buildkitd

# Copy builder agent
COPY --from=agent-builder /builder-agent /usr/bin/builder-agent

# Install additional dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    curl \
    jq \
    tar \
    gzip \
    fuse-overlayfs \
    && rm -rf /var/lib/apt/lists/*

# Create unprivileged user for rootless BuildKit
RUN useradd -m -u 1000 builder && \
    mkdir -p /home/builder/.local/share/buildkit && \
    chown -R builder:builder /home/builder

# Create directories for build
RUN mkdir -p /config /run/secrets /src && \
    chown -R builder:builder /config /run/secrets /src

# Install common Python tools
RUN pip install --no-cache-dir \
    pip-tools \
    poetry \
    pipenv

# Switch to unprivileged user
USER builder
WORKDIR /src

# Set environment for rootless buildkit
ENV BUILDKITD_FLAGS="--oci-worker-no-process-sandbox"
ENV HOME=/home/builder
ENV XDG_RUNTIME_DIR=/home/builder/.local/share

# Run builder agent as entrypoint
ENTRYPOINT ["/usr/bin/builder-agent"]

