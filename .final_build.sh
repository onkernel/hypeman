#!/bin/sh
set -e
cd "$(dirname "$0")"
echo "Working directory: $(pwd)"
echo "Running make oapi-generate..."
/usr/bin/make oapi-generate 2>&1
echo "Running make build..."
/usr/bin/make build 2>&1
echo "Success!"
