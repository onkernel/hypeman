# Alpine-based initrd base image for Hypeman
# Includes socat for vsock-based remote exec functionality
FROM alpine:3.22

# Install socat for vsock support
RUN apk add --no-cache socat

# Minimal metadata
LABEL org.opencontainers.image.description="Alpine base image with socat for Hypeman initrd"
LABEL org.opencontainers.image.source="https://github.com/onkernel/hypeman"

