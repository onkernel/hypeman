#!/usr/bin/env bash
set -euo pipefail

# Ensure Go is in PATH
export PATH="/usr/local/go/bin:${PATH}"

# Build the hypeman CLI from the stainless-sdks/hypeman-cli preview branch
# corresponding to the current branch in this repo.
#
# Usage:
#   ./scripts/build-preview-cli.sh                    # Use preview/<current-branch>
#   ./scripts/build-preview-cli.sh preview/my-branch  # Use specific branch

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${REPO_ROOT}/bin"

# Determine which branch to check out
if [ $# -ge 1 ]; then
  CLI_BRANCH="$1"
  echo "[INFO] Using override branch: ${CLI_BRANCH}"
else
  # Get current branch name
  CURRENT_BRANCH="$(git -C "${REPO_ROOT}" rev-parse --abbrev-ref HEAD)"
  CLI_BRANCH="preview/${CURRENT_BRANCH}"
  echo "[INFO] Current branch: ${CURRENT_BRANCH}"
  echo "[INFO] Will fetch CLI branch: ${CLI_BRANCH}"
fi

# Create temp directory for clone
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

echo "[INFO] Cloning stainless-sdks/hypeman-cli to temp directory..."
git clone --depth 1 --branch "${CLI_BRANCH}" \
  "git@github.com:stainless-sdks/hypeman-cli.git" \
  "${TEMP_DIR}/hypeman-cli"

echo "[INFO] Building CLI..."
mkdir -p "${BIN_DIR}"
cd "${TEMP_DIR}/hypeman-cli"
go build -o "${BIN_DIR}/hypeman" ./cmd/hypeman

echo ""
echo "========================================"
echo "CLI built successfully!"
echo "========================================"
echo "Binary: ${BIN_DIR}/hypeman"
echo "Branch: ${CLI_BRANCH}"
echo ""
echo "Test it with:"
echo "  ${BIN_DIR}/hypeman --help"
echo "========================================"

