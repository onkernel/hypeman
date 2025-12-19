# Node.js 20 Builder Image
# Contains rootless BuildKit + Node.js toolchain + builder agent

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
FROM node:20-alpine

# Copy BuildKit and runc from official image
COPY --from=buildkit /usr/bin/buildctl /usr/bin/buildctl
COPY --from=buildkit /usr/bin/buildctl-daemonless.sh /usr/bin/buildctl-daemonless.sh
COPY --from=buildkit /usr/bin/buildkitd /usr/bin/buildkitd
COPY --from=buildkit /usr/bin/buildkit-runc /usr/bin/runc

# Copy builder agent
COPY --from=agent-builder /builder-agent /usr/bin/builder-agent

# Install additional dependencies
RUN apk add --no-cache \
    ca-certificates \
    git \
    curl \
    jq \
    tar \
    gzip \
    shadow \
    fuse-overlayfs

# Use existing node user (uid 1000) for rootless BuildKit
# Rename node -> builder for clarity and setup buildkit directories
RUN deluser --remove-home node 2>/dev/null || true && \
    adduser -D -u 1000 builder && \
    mkdir -p /home/builder/.local/share/buildkit && \
    chown -R builder:builder /home/builder

# Create directories for build
RUN mkdir -p /config /run/secrets /src && \
    chown -R builder:builder /config /run/secrets /src

# Enable corepack for pnpm/yarn support
RUN corepack enable

# Switch to unprivileged user
USER builder
WORKDIR /src

# Set environment for buildkit in microVM
# Empty flags - use default buildkit behavior with cgroups
ENV BUILDKITD_FLAGS=""
ENV HOME=/home/builder
ENV XDG_RUNTIME_DIR=/home/builder/.local/share

# Run builder agent as entrypoint
ENTRYPOINT ["/usr/bin/builder-agent"]

