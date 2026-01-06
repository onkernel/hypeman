#!/bin/bash
# E2E Build System Test
# Usage: ./scripts/e2e-build-test.sh
#
# Prerequisites:
#   - API server running (make dev)
#   - Generic builder image imported into Hypeman registry
#   - .env file configured
#
# Environment variables:
#   API_URL       - API endpoint (default: http://localhost:8083)
#   BUILDER_IMAGE - Builder image to check (default: hypeman/builder:latest)

set -e

# Configuration
API_URL="${API_URL:-http://localhost:8083}"
TIMEOUT_POLLS=60
POLL_INTERVAL=5

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${GREEN}[INFO]${NC} $1" >&2; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }
error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check if API is reachable
    if ! curl -s "$API_URL/health" | grep -q "ok"; then
        error "API server not reachable at $API_URL"
        error "Start the server with: make dev"
        exit 1
    fi
    log "✓ API server is running"

    # Check if generic builder image exists
    BUILDER_IMAGE="${BUILDER_IMAGE:-hypeman/builder:latest}"
    if ! docker images "$BUILDER_IMAGE" --format "{{.Repository}}" | grep -q .; then
        warn "Builder image not found locally"
        warn "Build it with: docker build -t hypeman/builder:latest -f lib/builds/images/generic/Dockerfile ."
    else
        log "✓ Builder image available locally"
    fi
}

# Generate JWT token
generate_token() {
    cd "$(dirname "$0")/.."
    
    # Try using make gen-jwt
    if command -v make &> /dev/null && [ -f Makefile ]; then
        TOKEN=$(make gen-jwt 2>/dev/null | tail -1)
        if [ -n "$TOKEN" ] && [ "$TOKEN" != "make:" ]; then
            echo "$TOKEN"
            return
        fi
    fi
    
    # Fallback: run directly
    if [ -f ./bin/godotenv ]; then
        TOKEN=$(./bin/godotenv -f .env go run ./cmd/gen-jwt -user-id e2e-test 2>/dev/null | tail -1)
        echo "$TOKEN"
        return
    fi
    
    echo ""
}

# Create test source with Dockerfile
# The generic builder requires a Dockerfile to be provided
create_test_source() {
    TEST_DIR=$(mktemp -d)
    
    # Application code
    cat > "$TEST_DIR/package.json" << 'EOF'
{
  "name": "e2e-test-app",
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "start": "node index.js"
  },
  "dependencies": {}
}
EOF
    
    cat > "$TEST_DIR/index.js" << 'EOF'
console.log("E2E Build Test - Success!");
console.log("Built at:", new Date().toISOString());
EOF

    # Dockerfile is REQUIRED for the generic builder
    # Users control their runtime version here
    cat > "$TEST_DIR/Dockerfile" << 'EOF'
FROM node:20-alpine
WORKDIR /app
COPY package.json index.js ./
CMD ["node", "index.js"]
EOF

    # Create tarball
    TARBALL=$(mktemp --suffix=.tar.gz)
    tar -czvf "$TARBALL" -C "$TEST_DIR" . > /dev/null 2>&1
    
    rm -rf "$TEST_DIR"
    echo "$TARBALL"
}

# Submit build
submit_build() {
    local token="$1"
    local source="$2"
    
    log "Submitting build..."
    
    # Extract Dockerfile from source tarball
    DOCKERFILE_CONTENT=$(tar -xzf "$source" -O ./Dockerfile 2>/dev/null || echo "")
    
    if [ -n "$DOCKERFILE_CONTENT" ]; then
        # Dockerfile found in source - pass it explicitly for reliability
        RESPONSE=$(curl -s -X POST "$API_URL/builds" \
            -H "Authorization: Bearer $token" \
            -F "source=@$source" \
            -F "dockerfile=$DOCKERFILE_CONTENT" \
            -F "cache_scope=e2e-test" \
            -F "timeout_seconds=300")
    else
        # No Dockerfile in source - will fail if not provided
        error "No Dockerfile found in source tarball"
        exit 1
    fi
    
    BUILD_ID=$(echo "$RESPONSE" | jq -r '.id // empty')
    
    if [ -z "$BUILD_ID" ]; then
        error "Failed to submit build"
        echo "$RESPONSE" | jq .
        exit 1
    fi
    
    log "Build submitted: $BUILD_ID"
    echo "$BUILD_ID"
}

# Poll for build completion
wait_for_build() {
    local token="$1"
    local build_id="$2"
    
    log "Waiting for build to complete..."
    
    for i in $(seq 1 $TIMEOUT_POLLS); do
        RESPONSE=$(curl -s "$API_URL/builds/$build_id" \
            -H "Authorization: Bearer $token")
        
        STATUS=$(echo "$RESPONSE" | jq -r '.status')
        
        case "$STATUS" in
            "ready")
                log "✅ Build succeeded!"
                echo "$RESPONSE" | jq .
                return 0
                ;;
            "failed")
                error "❌ Build failed!"
                echo "$RESPONSE" | jq .
                return 1
                ;;
            "cancelled")
                warn "Build was cancelled"
                return 1
                ;;
            "queued"|"building"|"pushing")
                echo -ne "\r  Status: $STATUS (poll $i/$TIMEOUT_POLLS)..."
                ;;
            *)
                warn "Unknown status: $STATUS"
                ;;
        esac
        
        sleep $POLL_INTERVAL
    done
    
    error "Build timed out after $((TIMEOUT_POLLS * POLL_INTERVAL)) seconds"
    return 1
}

# Get build logs
get_logs() {
    local token="$1"
    local build_id="$2"
    
    log "Fetching build logs..."
    curl -s "$API_URL/builds/$build_id/logs" \
        -H "Authorization: Bearer $token"
}

# Main
main() {
    log "=== E2E Build System Test ==="
    echo ""
    
    # Check prerequisites
    check_prerequisites
    echo ""
    
    # Generate token
    log "Generating JWT token..."
    TOKEN=$(generate_token)
    if [ -z "$TOKEN" ]; then
        error "Failed to generate token"
        error "Run: make gen-jwt"
        exit 1
    fi
    log "✓ Token generated"
    echo ""
    
    # Create test source
    log "Creating test Node.js source..."
    SOURCE=$(create_test_source)
    log "✓ Test source created: $SOURCE"
    echo ""
    
    # Submit build
    BUILD_ID=$(submit_build "$TOKEN" "$SOURCE")
    echo ""
    
    # Wait for completion
    if wait_for_build "$TOKEN" "$BUILD_ID"; then
        echo ""
        log "=== Build Logs ==="
        get_logs "$TOKEN" "$BUILD_ID"
        echo ""
        log "=== E2E Test PASSED ==="
        
        # Cleanup
        rm -f "$SOURCE"
        exit 0
    else
        echo ""
        log "=== Build Logs ==="
        get_logs "$TOKEN" "$BUILD_ID"
        echo ""
        error "=== E2E Test FAILED ==="
        
        # Cleanup
        rm -f "$SOURCE"
        exit 1
    fi
}

main "$@"

