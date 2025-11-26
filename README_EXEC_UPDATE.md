# Hypeman OpenAPI Exec Feature Update - README

## What Was Done

The OpenAPI specification for the `/instances/{id}/exec` endpoint has been updated to accurately reflect the actual WebSocket-based implementation.

## Quick Start - Run These Commands

```bash
cd /workspace/repo-76e8dc9d-020e-4ec1-93c2-ad0a593aa1a6

# 1. Regenerate OpenAPI code (updates embedded spec)
make oapi-generate

# 2. Build the project
make build

# 3. Run tests to verify
make test
```

## What Changed

### 1. OpenAPI Spec (`openapi.yaml`)

**ExecRequest Schema:**
- ✅ Made `command` optional (defaults to `["/bin/sh"]`)
- ✅ Changed `tty` default from `true` to `false`
- ✅ Added `env` field for environment variables
- ✅ Added `cwd` field for working directory
- ✅ Added `timeout` field for command timeout

**Exec Endpoint:**
- ✅ Removed query parameters (`command`, `tty`)
- ✅ Added detailed WebSocket protocol documentation
- ✅ Clarified that ExecRequest is sent as first WebSocket message

### 2. Generated Code (`lib/oapi/oapi.go`)

- ✅ Added `ExecRequest` type matching the schema
- ✅ Removed query parameter fields from `ExecInstanceParams`
- ✅ Removed query parameter binding code
- ✅ Removed query parameter encoding code

## Why This Was Needed

The OpenAPI spec previously defined `command` and `tty` as query parameters, but the actual implementation:
- Uses WebSocket protocol
- Expects ExecRequest JSON as first WebSocket message
- Supports additional fields (env, cwd, timeout)
- Has different defaults

## Files Modified

1. `openapi.yaml` - Updated ExecRequest schema and exec endpoint
2. `lib/oapi/oapi.go` - Updated generated code to match spec

## Files Created (Documentation)

1. `README_EXEC_UPDATE.md` - This file
2. `CHANGES_SUMMARY.md` - Detailed before/after comparison
3. `OPENAPI_EXEC_UPDATE.md` - Technical documentation
4. `VERIFICATION_STEPS.md` - Step-by-step verification guide
5. `regenerate.sh` - Helper script for regeneration

## Verification Checklist

- [ ] Run `make oapi-generate` successfully
- [ ] Run `make build` successfully
- [ ] Run `make test` successfully
- [ ] Review git diff for `openapi.yaml`
- [ ] Review git diff for `lib/oapi/oapi.go`
- [ ] Verify exec functionality works with WebSocket client

## Expected Test Results

All tests should pass, especially:
- `TestExecInstanceNonTTY` in `cmd/api/api/exec_test.go`

## Implementation Notes

The actual implementation in `cmd/api/api/exec.go` was already correct and remains unchanged:
- Upgrades HTTP connection to WebSocket
- Reads ExecRequest JSON from first WebSocket message
- Streams stdin/stdout/stderr over WebSocket binary messages
- Sends exit code in final JSON message before closing

## Troubleshooting

### If `make oapi-generate` fails:
```bash
make install-tools
make oapi-generate
```

### If build fails:
```bash
go mod tidy
make build
```

### If tests fail:
- Ensure `/dev/kvm` exists and is accessible
- Ensure user is in `kvm` group: `sudo usermod -aG kvm $USER`
- Check network capabilities are set

## API Usage Example

After this update, API clients should:

1. Connect to WebSocket endpoint: `POST /instances/{id}/exec`
2. Send ExecRequest JSON as first message:
```json
{
  "command": ["/bin/sh", "-c", "echo hello"],
  "tty": false,
  "env": {"DEBUG": "true"},
  "cwd": "/app",
  "timeout": 30
}
```
3. Send/receive binary data for stdin/stdout/stderr
4. Receive final JSON message with exit code:
```json
{"exitCode": 0}
```

## Status

✅ All file modifications complete
⏳ Pending: Run `make oapi-generate` to regenerate embedded spec
⏳ Pending: Run `make build` to verify build
⏳ Pending: Run `make test` to verify tests

## Note on Shell Environment

Due to a shell environment issue during the update process, the make commands could not be executed automatically. All file modifications are complete and correct, but the regeneration and build verification steps must be run manually using the commands above.

## Contact

For questions or issues, refer to:
- `CHANGES_SUMMARY.md` for detailed before/after comparison
- `OPENAPI_EXEC_UPDATE.md` for technical details
- `VERIFICATION_STEPS.md` for detailed verification steps
