#!/bin/bash
set -e

cd /workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6

# Install oapi-codegen if not present
if [ ! -f bin/oapi-codegen ]; then
    echo "Installing oapi-codegen..."
    mkdir -p bin
    GOBIN=$(pwd)/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
fi

# Generate code
echo "Generating OpenAPI code..."
./bin/oapi-codegen -config ./oapi-codegen.yaml ./openapi.yaml

# Format generated code
echo "Formatting generated code..."
go fmt ./lib/oapi/oapi.go

echo "Done!"
